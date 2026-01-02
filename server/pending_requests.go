package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/dbackowski/wormhole/common"
)

type PendingRequests struct {
	pending map[string]chan *common.Message
	mu      sync.RWMutex
}

func NewPendingRequests() *PendingRequests {
	return &PendingRequests{
		pending: make(map[string]chan *common.Message),
	}
}

func (pr *PendingRequests) Register(ctx context.Context, uuid string) (chan *common.Message, context.CancelFunc) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	timeoutCtx, cancel := context.WithTimeout(ctx, common.RequestTimeoutBuffer)

	ch := make(chan *common.Message, 1)
	pr.pending[uuid] = ch

	go func() {
		<-timeoutCtx.Done()
		pr.Cleanup(uuid)
	}()

	return ch, cancel
}

func (pr *PendingRequests) Deliver(message *common.Message) error {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	ch, exists := pr.pending[message.UUID]

	if !exists {
		return fmt.Errorf("no pending request for UUID %s", message.UUID)
	}

	select {
	case ch <- message:
	default:
		return fmt.Errorf("failed to deliver message %s, channel closed or not ready", message.UUID)
	}

	return nil
}

func (pr *PendingRequests) Cleanup(uuid string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if ch, exists := pr.pending[uuid]; exists {
		close(ch)
		delete(pr.pending, uuid)
	}
}
