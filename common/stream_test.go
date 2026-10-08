package common

import (
	"errors"
	"io"
	"net"
	"sync"
	"testing"
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

func startPipe(t *testing.T, s *Streams, st *Stream, sent *sentMessages) (peer net.Conn, done chan struct{}) {
	t.Helper()
	local, peer := net.Pipe()
	done = make(chan struct{})
	go func() {
		s.Pipe("u1", st, local, sent.send)
		close(done)
	}()
	return peer, done
}

func waitDone(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Pipe did not return")
	}
}

// Chunks queued before Finish must still be written: they can carry the
// WebSocket close frame the peer sent just before closing.
func TestStreams_FinishWritesQueuedChunksFirst(t *testing.T) {
	s := NewStreams()
	st, err := s.Open("u1")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	s.Deliver("u1", []byte("ab"))
	s.Deliver("u1", []byte("cd"))
	s.Finish("u1")

	sent := &sentMessages{}
	peer, done := startPipe(t, s, st, sent)

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

func TestStreams_ReadsBecomeStreamData(t *testing.T) {
	s := NewStreams()
	st, _ := s.Open("u1")
	sent := &sentMessages{}
	peer, done := startPipe(t, s, st, sent)

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

// A peer that outruns the queue must not block the caller of Deliver, which
// is the tunnel's message loop.
func TestStreams_OverflowAborts(t *testing.T) {
	s := NewStreams()
	st, _ := s.Open("u1")
	for range StreamQueueSize + 1 {
		s.Deliver("u1", []byte("x"))
	}

	if s.Count() != 0 {
		t.Errorf("Count() = %d, want 0 after overflow", s.Count())
	}
	select {
	case <-st.done:
	default:
		t.Error("stream not aborted after overflow")
	}
}

func TestStreams_AbortClosesConnDuringBlockedWrite(t *testing.T) {
	s := NewStreams()
	st, _ := s.Open("u1")
	sent := &sentMessages{}
	_, done := startPipe(t, s, st, sent)

	// Nobody reads the peer end, so this write blocks inside Pipe.
	s.Deliver("u1", []byte("stuck"))
	time.Sleep(20 * time.Millisecond)
	s.AbortAll()

	waitDone(t, done)
}

func TestStreams_OpenLimit(t *testing.T) {
	s := NewStreams()
	for i := range MaxStreamsPerTunnel {
		if _, err := s.Open(string(rune('a' + i))); err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
	}
	if _, err := s.Open("one-more"); !errors.Is(err, ErrTooManyStreams) {
		t.Errorf("Open() error = %v, want ErrTooManyStreams", err)
	}
}

func TestStreams_AbortReportsOpen(t *testing.T) {
	s := NewStreams()
	s.Open("u1") //nolint:errcheck
	if !s.Abort("u1") {
		t.Error("Abort() = false for open stream")
	}
	if s.Abort("u1") {
		t.Error("Abort() = true for already aborted stream")
	}
	// Unknown streams are ignored rather than panicking.
	s.Deliver("u1", []byte("late"))
	s.Finish("u1")
}
