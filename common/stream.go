package common

import (
	"errors"
	"io"
	"sync"
)

const (
	StreamChunkSize = 32 << 10
	// StreamWindow is how many bytes a side may send on one stream before the
	// peer acknowledges consuming them, which bounds the memory a stream holds
	// on the receiving side. Throughput per stream is at most one window per
	// round trip: 256 KB at 30 ms is about 8 MB/s.
	StreamWindow       = 256 << 10
	streamAckThreshold = StreamWindow / 4

	MaxStreamsPerTunnel = 100 // WebSockets
)

var (
	ErrTooManyStreams = errors.New("too many open streams")
	ErrStreamClosed   = errors.New("stream closed")
	ErrStreamReset    = errors.New("stream reset by peer")
)

// Stream is one body or upgraded connection carried over the tunnel, in both
// directions. Read returns what the peer sent; Send passes a reader to the
// peer. Each direction is flow controlled: a sender waits once the peer holds
// StreamWindow bytes it has not read, so a slow reader slows its own sender
// instead of filling memory or stalling the message loop that delivers chunks.
type Stream struct {
	uuid string
	send func(*Message) error

	mu       sync.Mutex
	queue    [][]byte // chunks from the peer, not yet read
	queued   int
	finished bool  // the peer has sent everything
	err      error // set once aborted
	unacked  int   // bytes read but not yet acknowledged to the peer
	credit   int   // bytes this side may still send

	readable  chan struct{} // see notify
	writable  chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// notify wakes a waiter without blocking; wakeups made while one is pending
// fold into it. Each channel has a single waiter: Read, or Send.
func notify(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (st *Stream) Done() <-chan struct{} {
	return st.done
}

func (st *Stream) abort(err error) {
	st.mu.Lock()
	if st.err == nil {
		st.err = err
	}
	st.queue, st.queued = nil, 0
	st.mu.Unlock()
	st.closeOnce.Do(func() { close(st.done) })
}

// deliver queues a chunk from the peer. It reports false if the peer
// overran the window, which only a broken peer does.
func (st *Stream) deliver(data []byte) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil || st.finished {
		return true
	}
	if st.queued+len(data) > StreamWindow {
		return false
	}
	st.queue = append(st.queue, data)
	st.queued += len(data)
	notify(st.readable)
	return true
}

func (st *Stream) finish() {
	st.mu.Lock()
	st.finished = true
	st.mu.Unlock()
	notify(st.readable)
}

func (st *Stream) addCredit(n int) {
	st.mu.Lock()
	st.credit += n
	st.mu.Unlock()
	notify(st.writable)
}

// Read returns what the peer sent, io.EOF once it has finished, or the
// reason the stream was aborted. It acknowledges what it reads, which lets the
// peer send more.
func (st *Stream) Read(p []byte) (int, error) {
	for {
		st.mu.Lock()
		if st.err != nil {
			err := st.err
			st.mu.Unlock()
			return 0, err
		}
		if len(st.queue) > 0 {
			n := copy(p, st.queue[0])
			if st.queue[0] = st.queue[0][n:]; len(st.queue[0]) == 0 {
				st.queue = st.queue[1:]
			}
			st.queued -= n
			st.unacked += n
			ack := 0
			// A peer that has finished sends nothing more, so needs no credit.
			if st.unacked >= streamAckThreshold && !st.finished {
				ack, st.unacked = st.unacked, 0
			}
			st.mu.Unlock()
			if ack > 0 {
				st.send(&Message{Type: MessageTypeStreamAck, UUID: st.uuid, Ack: ack}) //nolint:errcheck // a dead tunnel aborts the stream
			}
			return n, nil
		}
		if st.finished {
			st.mu.Unlock()
			return 0, io.EOF
		}
		st.mu.Unlock()

		select {
		case <-st.readable:
		case <-st.done:
		}
	}
}

// Send passes r to the peer as stream_data, then ends this direction with
// stream_close at EOF or stream_reset on a read error, so the peer can tell a
// complete body from a cut one. It returns early, with the reason, if the
// stream is aborted or the tunnel fails.
func (st *Stream) Send(r io.Reader) error {
	buf := make([]byte, StreamChunkSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if werr := st.write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			return st.send(&Message{Type: MessageTypeStreamClose, UUID: st.uuid})
		}
		if err != nil {
			st.send(&Message{Type: MessageTypeStreamReset, UUID: st.uuid}) //nolint:errcheck // the read error matters more
			return err
		}
	}
}

// write sends p once the peer has room for it. send must not keep p.
func (st *Stream) write(p []byte) error {
	for {
		st.mu.Lock()
		if st.err != nil {
			err := st.err
			st.mu.Unlock()
			return err
		}
		if st.credit >= len(p) {
			st.credit -= len(p)
			st.mu.Unlock()
			return st.send(&Message{Type: MessageTypeStreamData, UUID: st.uuid, Body: p})
		}
		st.mu.Unlock()

		select {
		case <-st.writable:
		case <-st.done:
		}
	}
}

// Streams holds the open streams of one tunnel connection, keyed by the UUID
// of the request that opened them.
type Streams struct {
	mu      sync.Mutex
	streams map[string]*Stream
	max     int
}

func NewStreams(max int) *Streams {
	return &Streams{streams: make(map[string]*Stream), max: max}
}

// Open starts a stream whose messages to the peer go through send.
func (s *Streams) Open(uuid string, send func(*Message) error) (*Stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.streams) >= s.max {
		return nil, ErrTooManyStreams
	}

	st := &Stream{
		uuid:     uuid,
		send:     send,
		credit:   StreamWindow,
		readable: make(chan struct{}, 1),
		writable: make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
	s.streams[uuid] = st
	return st, nil
}

func (s *Streams) get(uuid string) *Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams[uuid]
}

func (s *Streams) remove(uuid string) *Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.streams[uuid]
	delete(s.streams, uuid)
	return st
}

// Deliver queues a chunk from the peer. Chunks for an unknown stream are
// dropped: it has already ended on this side.
func (s *Streams) Deliver(uuid string, data []byte) {
	if st := s.get(uuid); st != nil && len(data) > 0 && !st.deliver(data) {
		s.Abort(uuid)
	}
}

// Finish marks that the peer has sent everything; Read returns io.EOF once
// the chunks queued before it are read.
func (s *Streams) Finish(uuid string) {
	if st := s.get(uuid); st != nil {
		st.finish()
	}
}

// Reset ends a stream the peer gave up on, dropping anything queued.
func (s *Streams) Reset(uuid string) {
	if st := s.remove(uuid); st != nil {
		st.abort(ErrStreamReset)
	}
}

// Ack lets the stream send n more bytes, which the peer has consumed.
func (s *Streams) Ack(uuid string, n int) {
	if st := s.get(uuid); st != nil && n > 0 {
		st.addCredit(n)
	}
}

// Abort ends a stream at once, dropping anything still queued. It reports
// whether the stream was still open, that is, neither ended here before nor
// reset by the peer.
func (s *Streams) Abort(uuid string) bool {
	st := s.remove(uuid)
	if st != nil {
		st.abort(ErrStreamClosed)
	}
	return st != nil
}

func (s *Streams) AbortAll() {
	s.mu.Lock()
	all := s.streams
	s.streams = make(map[string]*Stream)
	s.mu.Unlock()

	for _, st := range all {
		st.abort(ErrStreamClosed)
	}
}

func (s *Streams) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams)
}

// StreamHandlers routes the stream messages of a tunnel to whichever of all
// holds the stream.
func StreamHandlers(all ...*Streams) Handlers {
	each := func(f func(*Streams, *Message)) func(*Message) error {
		return func(msg *Message) error {
			for _, s := range all {
				f(s, msg)
			}
			return nil
		}
	}
	return Handlers{
		MessageTypeStreamData:  each(func(s *Streams, m *Message) { s.Deliver(m.UUID, m.Body) }),
		MessageTypeStreamClose: each(func(s *Streams, m *Message) { s.Finish(m.UUID) }),
		MessageTypeStreamReset: each(func(s *Streams, m *Message) { s.Reset(m.UUID) }),
		MessageTypeStreamAck:   each(func(s *Streams, m *Message) { s.Ack(m.UUID, m.Ack) }),
	}
}

// Pipe carries an upgraded connection over the tunnel until either side ends
// it, then closes conn, tells the peer, and returns once both directions have
// stopped.
func (s *Streams) Pipe(uuid string, st *Stream, conn io.ReadWriteCloser) {
	// Closing conn is what unblocks a Read or Write in progress.
	go func() {
		<-st.done
		conn.Close()
	}()

	written := make(chan struct{})
	go func() {
		defer close(written)
		io.Copy(conn, st) //nolint:errcheck // either way the socket is done
		st.abort(ErrStreamClosed)
	}()

	// Send reports a read error with stream_reset; for a socket, which ends the
	// same way whatever stopped it, stream_close fits either way.
	if st.Send(eofReader{conn}) != nil {
		st.send(&Message{Type: MessageTypeStreamClose, UUID: uuid}) //nolint:errcheck // the tunnel may already be gone
	}
	s.Abort(uuid)
	<-written
}

type eofReader struct{ io.Reader }

func (r eofReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err != nil {
		err = io.EOF
	}
	return n, err
}
