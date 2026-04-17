package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dbackowski/wormhole/common"
)

func TestDeliver_Success(t *testing.T) {
	pr := NewPendingRequests()
	ch, cancel := pr.Register(context.Background(), "uuid-1")
	defer cancel()

	msg := &common.Message{UUID: "uuid-1"}
	if err := pr.Deliver(msg); err != nil {
		t.Fatalf("Deliver() error = %v, want nil", err)
	}

	select {
	case got := <-ch:
		if got != msg {
			t.Errorf("got %v, want %v", got, msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestDeliver_UnknownUUID(t *testing.T) {
	pr := NewPendingRequests()
	err := pr.Deliver(&common.Message{UUID: "missing"})
	if err == nil {
		t.Fatal("expected error for unknown UUID, got nil")
	}
}

func TestDeliver_AfterCleanupDoesNotPanic(t *testing.T) {
	pr := NewPendingRequests()
	_, cancel := pr.Register(context.Background(), "uuid-1")
	cancel()

	err := pr.Deliver(&common.Message{UUID: "uuid-1"})
	if err == nil {
		t.Fatal("expected error after cleanup, got nil")
	}
}

func TestDeliver_ChannelFull(t *testing.T) {
	pr := NewPendingRequests()
	_, cancel := pr.Register(context.Background(), "uuid-1")
	defer cancel()

	if err := pr.Deliver(&common.Message{UUID: "uuid-1"}); err != nil {
		t.Fatalf("first Deliver() error = %v, want nil", err)
	}
	if err := pr.Deliver(&common.Message{UUID: "uuid-1"}); err == nil {
		t.Fatal("expected error on second Deliver, got nil")
	}
}

func TestDeliver_RaceWithCleanup(t *testing.T) {
	const iterations = 500
	for range iterations {
		pr := NewPendingRequests()
		_, cancel := pr.Register(context.Background(), "uuid-1")

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = pr.Deliver(&common.Message{UUID: "uuid-1"})
		}()
		go func() {
			defer wg.Done()
			cancel()
		}()
		wg.Wait()
	}
}

func TestCleanup_Idempotent(t *testing.T) {
	pr := NewPendingRequests()
	_, cancel := pr.Register(context.Background(), "uuid-1")
	cancel()
	cancel()
}
