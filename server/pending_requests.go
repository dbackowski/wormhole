package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/dbackowski/wormhole/common"
)

type pendingRequest struct {
	ch   chan *common.Message
	once sync.Once
}

type PendingRequests struct {
	pending map[string]*pendingRequest
	mu      sync.RWMutex
}

func NewPendingRequests() *PendingRequests {
	return &PendingRequests{
		pending: make(map[string]*pendingRequest),
	}
}

func (pr *PendingRequests) Register(ctx context.Context, uuid string) (chan *common.Message, context.CancelFunc) {
	timeoutCtx, cancel := context.WithTimeout(ctx, common.RequestTimeoutBuffer)

	req := &pendingRequest{ch: make(chan *common.Message, 1)}
	pr.mu.Lock()
	pr.pending[uuid] = req
	pr.mu.Unlock()

	context.AfterFunc(timeoutCtx, func() {
		pr.Cleanup(uuid)
	})

	return req.ch, func() {
		cancel()
		pr.Cleanup(uuid)
	}
}

func (pr *PendingRequests) Deliver(message *common.Message) error {
	pr.mu.RLock()
	req, exists := pr.pending[message.UUID]
	pr.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no pending request for UUID %s", message.UUID)
	}

	select {
	case req.ch <- message:
	default:
		return fmt.Errorf("failed to deliver message %s, channel closed or not ready", message.UUID)
	}

	return nil
}

func (pr *PendingRequests) Cleanup(uuid string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if req, exists := pr.pending[uuid]; exists {
		delete(pr.pending, uuid)
		req.once.Do(func() { close(req.ch) })
	}
}
