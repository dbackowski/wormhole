package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/dbackowski/wormhole/common"
)

type PendingRequests struct {
	pending map[string]chan *common.Message
	mu      sync.Mutex
}

func NewPendingRequests() *PendingRequests {
	return &PendingRequests{
		pending: make(map[string]chan *common.Message),
	}
}

func (pr *PendingRequests) Register(ctx context.Context, uuid string) (chan *common.Message, context.CancelFunc) {
	timeoutCtx, cancel := context.WithTimeout(ctx, common.RequestTimeoutBuffer)

	ch := make(chan *common.Message, 1)
	pr.mu.Lock()
	pr.pending[uuid] = ch
	pr.mu.Unlock()

	context.AfterFunc(timeoutCtx, func() {
		pr.Cleanup(uuid)
	})

	return ch, func() {
		cancel()
		pr.Cleanup(uuid)
	}
}

func (pr *PendingRequests) Deliver(message *common.Message) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	ch, exists := pr.pending[message.UUID]
	if !exists {
		return fmt.Errorf("no pending request for UUID %s", message.UUID)
	}

	select {
	case ch <- message:
		return nil
	default:
		return fmt.Errorf("failed to deliver message %s, channel full", message.UUID)
	}
}

// Cleanup runs twice per request (deferred cleanup and timeout). The lookup,
// delete and close share one lock, so only the first call closes the channel.
func (pr *PendingRequests) Cleanup(uuid string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if ch, exists := pr.pending[uuid]; exists {
		delete(pr.pending, uuid)
		close(ch)
	}
}
