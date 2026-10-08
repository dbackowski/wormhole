package common

import (
	"errors"
	"io"
	"sync"
)

const (
	StreamChunkSize     = 32 << 10
	StreamQueueSize     = 64 // chunks, so up to 2 MB buffered per stream
	MaxStreamsPerTunnel = 100
)

var ErrTooManyStreams = errors.New("too many open streams")

// Stream is one upgraded connection (a WebSocket) carried over the tunnel.
// Chunks from the peer are queued and written out by Pipe, so a slow socket
// never blocks the message loop that delivers them.
type Stream struct {
	queue     chan []byte // a nil chunk ends the stream, see Streams.Finish
	done      chan struct{}
	closeOnce sync.Once
}

func (st *Stream) abort() {
	st.closeOnce.Do(func() { close(st.done) })
}

// Streams holds the open streams of one tunnel connection, keyed by the UUID
// of the upgrade request that opened them.
type Streams struct {
	mu      sync.Mutex
	streams map[string]*Stream
}

func NewStreams() *Streams {
	return &Streams{streams: make(map[string]*Stream)}
}

func (s *Streams) Open(uuid string) (*Stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.streams) >= MaxStreamsPerTunnel {
		return nil, ErrTooManyStreams
	}

	st := &Stream{
		queue: make(chan []byte, StreamQueueSize),
		done:  make(chan struct{}),
	}
	s.streams[uuid] = st
	return st, nil
}

func (s *Streams) get(uuid string) *Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams[uuid]
}

// Deliver queues a chunk from the peer. Chunks for an unknown stream are
// dropped: it has already ended on this side, and the peer has been or will be
// told so by stream_close.
// ponytail: a full queue aborts the stream, since the tunnel has no flow
// control and blocking would stall every other request. Add per-stream acks if
// real apps outrun the 2 MB buffer.
func (s *Streams) Deliver(uuid string, data []byte) {
	st := s.get(uuid)
	if st == nil || len(data) == 0 {
		return
	}

	select {
	case st.queue <- data:
	default:
		s.Abort(uuid)
	}
}

// Finish ends a stream once the chunks queued before it are written. The peer
// has closed its side, and its last chunks (such as a WebSocket close frame)
// must still go out.
func (s *Streams) Finish(uuid string) {
	st := s.get(uuid)
	if st == nil {
		return
	}

	select {
	case st.queue <- nil:
	default:
		s.Abort(uuid)
	}
}

// Abort ends a stream at once, dropping anything still queued. It reports
// whether the stream was still open.
func (s *Streams) Abort(uuid string) bool {
	s.mu.Lock()
	st, ok := s.streams[uuid]
	delete(s.streams, uuid)
	s.mu.Unlock()

	if ok {
		st.abort()
	}
	return ok
}

func (s *Streams) AbortAll() {
	s.mu.Lock()
	all := s.streams
	s.streams = make(map[string]*Stream)
	s.mu.Unlock()

	for _, st := range all {
		st.abort()
	}
}

func (s *Streams) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams)
}

// Pipe carries conn over the tunnel until either side ends it: what conn reads
// is passed to send as stream_data, and chunks queued by Deliver are written to
// conn. It then closes conn, sends stream_close and returns once both
// directions have stopped. send must not keep the message after it returns.
func (s *Streams) Pipe(uuid string, st *Stream, conn io.ReadWriteCloser, send func(*Message) error) {
	// Closing conn is what unblocks a Read or Write in progress.
	go func() {
		<-st.done
		conn.Close()
	}()

	written := make(chan struct{})
	go func() {
		defer close(written)
		defer st.abort()
		for {
			select {
			case data := <-st.queue:
				if data == nil {
					return
				}
				if _, err := conn.Write(data); err != nil {
					return
				}
			case <-st.done:
				return
			}
		}
	}()

	buf := make([]byte, StreamChunkSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 && send(&Message{Type: MessageTypeStreamData, UUID: uuid, Body: buf[:n]}) != nil {
			break
		}
		if err != nil {
			break
		}
	}

	send(&Message{Type: MessageTypeStreamClose, UUID: uuid}) //nolint:errcheck // the tunnel may already be gone
	s.Abort(uuid)
	st.abort() // in case it was already gone from the map
	<-written
}
