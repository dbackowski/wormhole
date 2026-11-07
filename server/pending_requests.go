package server

import (
	"fmt"
	"sync"

	"github.com/dbackowski/wormhole/common"
)

// PendingRequests holds the requests waiting for their response headers. How
// long one waits is up to its handler: the clock starts once its body is sent.
type PendingRequests struct {
	pending map[string]chan *common.Message
	mu      sync.Mutex
}

func NewPendingRequests() *PendingRequests {
	return &PendingRequests{
		pending: make(map[string]chan *common.Message),
	}
}

// Register returns the channel the response for uuid arrives on, and the
// function that stops waiting for it.
func (pr *PendingRequests) Register(uuid string) (chan *common.Message, func()) {
	ch := make(chan *common.Message, 1)
	pr.mu.Lock()
	pr.pending[uuid] = ch
	pr.mu.Unlock()

	return ch, func() { pr.Cleanup(uuid) }
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

func (pr *PendingRequests) Cleanup(uuid string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	delete(pr.pending, uuid)
}

func (pr *PendingRequests) Len() int {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	return len(pr.pending)
}
