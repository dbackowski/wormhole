package common

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"testing/iotest"
	"time"
)

type sentMessages struct {
	mu   sync.Mutex
	msgs []Message
}

func (s *sentMessages) send(msg *Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := *msg
	m.Body = append([]byte(nil), msg.Body...)
	s.msgs = append(s.msgs, m)
	return nil
}

func (s *sentMessages) all() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.msgs...)
}

func (s *sentMessages) dataBytes() int {
	n := 0
	for _, m := range s.all() {
		if m.Type == MessageTypeStreamData {
			n += len(m.Body)
		}
	}
	return n
}

func startPipe(t *testing.T, s *Streams, st *Stream) (peer net.Conn, done chan struct{}) {
	t.Helper()
	local, peer := net.Pipe()
	done = make(chan struct{})
	go func() {
		s.Pipe("u1", st, local)
		close(done)
	}()
	return peer, done
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Chunks queued before Finish must still be written: they can carry the
// WebSocket close frame the peer sent just before closing.
func TestStreams_FinishWritesQueuedChunksFirst(t *testing.T) {
	s := NewStreams(1)
	sent := &sentMessages{}
	st, err := s.Open("u1", sent.send)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	s.Deliver("u1", []byte("ab"))
	s.Deliver("u1", []byte("cd"))
	s.Finish("u1")

	peer, done := startPipe(t, s, st)

	got, err := io.ReadAll(peer)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != "abcd" {
		t.Errorf("written = %q, want %q", got, "abcd")
	}

	waitDone(t, done)
	msgs := sent.all()
	if len(msgs) == 0 || msgs[len(msgs)-1].Type != MessageTypeStreamClose {
		t.Errorf("last message = %v, want stream_close", msgs)
	}
	if s.Count() != 0 {
		t.Errorf("Count() = %d, want 0", s.Count())
	}
}

func TestStreams_PipeReadsBecomeStreamData(t *testing.T) {
	s := NewStreams(1)
	sent := &sentMessages{}
	st, _ := s.Open("u1", sent.send)
	peer, done := startPipe(t, s, st)

	if _, err := peer.Write([]byte("hello")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	peer.Close()
	waitDone(t, done)

	msgs := sent.all()
	if len(msgs) != 2 {
		t.Fatalf("sent %d messages, want 2: %v", len(msgs), msgs)
	}
	if msgs[0].Type != MessageTypeStreamData || string(msgs[0].Body) != "hello" || msgs[0].UUID != "u1" {
		t.Errorf("first message = %+v, want stream_data hello", msgs[0])
	}
	if msgs[1].Type != MessageTypeStreamClose {
		t.Errorf("second message type = %q, want stream_close", msgs[1].Type)
	}
}

func TestStreams_PipeAbortClosesConnDuringBlockedWrite(t *testing.T) {
	s := NewStreams(1)
	sent := &sentMessages{}
	st, _ := s.Open("u1", sent.send)
	_, done := startPipe(t, s, st)

	// Nobody reads the peer end, so this write blocks inside Pipe.
	s.Deliver("u1", []byte("stuck"))
	time.Sleep(20 * time.Millisecond)
	s.AbortAll()

	waitDone(t, done)
}

// A peer that ignores the window must not grow the queue without bound or
// block the caller of Deliver, which is the tunnel's message loop.
func TestStreams_OverrunningTheWindowAborts(t *testing.T) {
	s := NewStreams(1)
	st, _ := s.Open("u1", (&sentMessages{}).send)
	s.Deliver("u1", make([]byte, StreamWindow))
	if s.Count() != 1 {
		t.Fatal("a full window aborted the stream")
	}
	s.Deliver("u1", []byte("x"))

	if s.Count() != 0 {
		t.Errorf("Count() = %d, want 0 after overrun", s.Count())
	}
	if _, err := st.Read(make([]byte, 1)); !errors.Is(err, ErrStreamClosed) {
		t.Errorf("Read() error = %v, want ErrStreamClosed", err)
	}
}

func TestStreams_OpenLimit(t *testing.T) {
	s := NewStreams(MaxStreamsPerTunnel)
	for i := range MaxStreamsPerTunnel {
		if _, err := s.Open(string(rune('a'+i)), nil); err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
	}
	if _, err := s.Open("one-more", nil); !errors.Is(err, ErrTooManyStreams) {
		t.Errorf("Open() error = %v, want ErrTooManyStreams", err)
	}
}

func TestStreams_AbortReportsOpen(t *testing.T) {
	s := NewStreams(2)
	s.Open("u1", nil) //nolint:errcheck
	if !s.Abort("u1") {
		t.Error("Abort() = false for open stream")
	}
	if s.Abort("u1") {
		t.Error("Abort() = true for already aborted stream")
	}

	// A stream the peer reset is no longer open either: nothing to tell it.
	st, _ := s.Open("u2", nil)
	s.Reset("u2")
	if s.Abort("u2") {
		t.Error("Abort() = true for a reset stream")
	}
	if _, err := st.Read(make([]byte, 1)); !errors.Is(err, ErrStreamReset) {
		t.Errorf("Read() error = %v, want ErrStreamReset", err)
	}

	// Unknown streams are ignored rather than panicking.
	s.Deliver("u1", []byte("late"))
	s.Finish("u1")
	s.Reset("u1")
	s.Ack("u1", 10)
}

func TestStream_SendWaitsForCredit(t *testing.T) {
	s := NewStreams(1)
	sent := &sentMessages{}
	st, _ := s.Open("u1", sent.send)

	done := make(chan error, 1)
	go func() { done <- st.Send(bytes.NewReader(make([]byte, 2*StreamWindow))) }()

	waitUntil(t, "a full window to be sent", func() bool { return sent.dataBytes() == StreamWindow })
	time.Sleep(20 * time.Millisecond)
	if n := sent.dataBytes(); n != StreamWindow {
		t.Fatalf("sent %d bytes without credit, want %d", n, StreamWindow)
	}

	s.Ack("u1", StreamWindow)
	waitDone(t, chanOf(done))
	if n := sent.dataBytes(); n != 2*StreamWindow {
		t.Errorf("sent %d bytes, want %d", n, 2*StreamWindow)
	}
	if msgs := sent.all(); msgs[len(msgs)-1].Type != MessageTypeStreamClose {
		t.Errorf("last message = %q, want stream_close", msgs[len(msgs)-1].Type)
	}
}

func chanOf(errs chan error) <-chan struct{} {
	done := make(chan struct{})
	go func() { <-errs; close(done) }()
	return done
}

func TestStream_SendStopsWhenAborted(t *testing.T) {
	s := NewStreams(1)
	st, _ := s.Open("u1", (&sentMessages{}).send)

	done := make(chan error, 1)
	go func() { done <- st.Send(bytes.NewReader(make([]byte, 2*StreamWindow))) }()
	time.Sleep(20 * time.Millisecond)
	s.Abort("u1")

	select {
	case err := <-done:
		if !errors.Is(err, ErrStreamClosed) {
			t.Errorf("Send() error = %v, want ErrStreamClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Send() still waiting for credit after abort")
	}
}

// A cut body must not look complete to the peer.
func TestStream_SendResetsOnReadError(t *testing.T) {
	s := NewStreams(1)
	sent := &sentMessages{}
	st, _ := s.Open("u1", sent.send)

	readErr := errors.New("browser went away")
	if err := st.Send(iotest.ErrReader(readErr)); !errors.Is(err, readErr) {
		t.Errorf("Send() error = %v, want %v", err, readErr)
	}
	if msgs := sent.all(); len(msgs) != 1 || msgs[0].Type != MessageTypeStreamReset {
		t.Errorf("sent %v, want one stream_reset", msgs)
	}
}

func TestStream_ReadAcknowledges(t *testing.T) {
	s := NewStreams(1)
	sent := &sentMessages{}
	st, _ := s.Open("u1", sent.send)

	s.Deliver("u1", make([]byte, streamAckThreshold-1))
	io.ReadFull(st, make([]byte, streamAckThreshold-1)) //nolint:errcheck
	if msgs := sent.all(); len(msgs) != 0 {
		t.Fatalf("acknowledged below the threshold: %v", msgs)
	}

	s.Deliver("u1", []byte("x"))
	io.ReadFull(st, make([]byte, 1)) //nolint:errcheck
	msgs := sent.all()
	if len(msgs) != 1 || msgs[0].Type != MessageTypeStreamAck || msgs[0].Ack != streamAckThreshold {
		t.Errorf("sent %+v, want one stream_ack of %d", msgs, streamAckThreshold)
	}
}

// linkedStreams connects two sides the way a tunnel does, each side's
// messages going to the other's handlers.
func linkedStreams(t *testing.T) (a, b *Stream) {
	t.Helper()
	sa, sb := NewStreams(1), NewStreams(1)
	ha, hb := StreamHandlers(sa), StreamHandlers(sb)
	a, _ = sa.Open("u1", func(m *Message) error { return hb.Dispatch(copyMsg(m)) })
	b, _ = sb.Open("u1", func(m *Message) error { return ha.Dispatch(copyMsg(m)) })
	return a, b
}

// copyMsg stands in for the JSON round trip: send must not keep the message.
func copyMsg(m *Message) *Message {
	c := *m
	c.Body = bytes.Clone(m.Body)
	return &c
}

// A body many windows long arrives intact through a reader slower than the
// sender, which is held back by the window rather than overrunning it.
func TestStream_TransferThroughSlowReader(t *testing.T) {
	a, b := linkedStreams(t)
	body := make([]byte, 5*StreamWindow+123)
	rand.Read(body) //nolint:errcheck

	sendErr := make(chan error, 1)
	go func() { sendErr <- a.Send(bytes.NewReader(body)) }()

	var got bytes.Buffer
	buf := make([]byte, 1000)
	for {
		n, err := b.Read(buf)
		got.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if got.Len()%(50*1000) == 0 {
			time.Sleep(time.Millisecond)
		}
	}

	if err := <-sendErr; err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !bytes.Equal(got.Bytes(), body) {
		t.Errorf("received %d bytes that differ from the %d sent", got.Len(), len(body))
	}
}
