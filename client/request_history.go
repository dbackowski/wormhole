package client

import (
	"slices"
	"sync"
)

// MaxStoredBodySize caps each body kept in the history, see bodyCapture.
// Bodies have no size limit, so only the head is kept, enough for typical JSON
// and webhook payloads: a full history then holds about 64 MB of bodies.
const MaxStoredBodySize = 64 << 10

// Per WebSocket, the most recent messages are kept, each cut to its head.
const (
	MaxStoredMessages    = 100
	MaxStoredMessageSize = 4 << 10
)

type RequestHistory struct {
	logs    []RequestLog
	mutex   sync.RWMutex
	maxSize int
	total   int // every request ever added; neither eviction nor Clear lowers it
}

func NewRequestHistory(maxSize int) *RequestHistory {
	return &RequestHistory{
		logs:    make([]RequestLog, 0, maxSize),
		maxSize: maxSize,
	}
}

func (rh *RequestHistory) Add(log RequestLog) {
	rh.mutex.Lock()
	defer rh.mutex.Unlock()

	rh.logs = append(rh.logs, log)
	rh.total++
	if n := len(rh.logs) - rh.maxSize; n > 0 {
		// slices.Delete zeroes the vacated tail, so dropped entries' bodies are
		// released instead of lingering in the backing array.
		rh.logs = slices.Delete(rh.logs, 0, n)
	}
}

// Update applies fn to the entry for uuid, if it is still in the history.
func (rh *RequestHistory) Update(uuid string, fn func(*RequestLog)) {
	rh.mutex.Lock()
	defer rh.mutex.Unlock()

	for i := range rh.logs {
		if rh.logs[i].UUID == uuid {
			fn(&rh.logs[i])
			return
		}
	}
}

func (rh *RequestHistory) Get(uuid string) (RequestLog, bool) {
	rh.mutex.RLock()
	defer rh.mutex.RUnlock()

	for _, log := range rh.logs {
		if log.UUID == uuid {
			log.Messages = nil // see GetRecent
			return log, true
		}
	}
	return RequestLog{}, false
}

func (rh *RequestHistory) GetMessages(uuid string) ([]WSMessage, bool) {
	rh.mutex.RLock()
	defer rh.mutex.RUnlock()

	for _, log := range rh.logs {
		if log.UUID == uuid {
			return slices.Clone(log.Messages), true
		}
	}
	return nil, false
}

func (rh *RequestHistory) Clear() {
	rh.mutex.Lock()
	defer rh.mutex.Unlock()

	clear(rh.logs)
	rh.logs = rh.logs[:0]
}

func (rh *RequestHistory) Total() int {
	rh.mutex.RLock()
	defer rh.mutex.RUnlock()
	return rh.total
}

func (rh *RequestHistory) GetRecent(n int) []RequestLog {
	rh.mutex.RLock()
	defer rh.mutex.RUnlock()

	result := slices.Clone(rh.logs[max(len(rh.logs)-n, 0):])
	// Update trims Messages in place, so copies must not share them.
	for i := range result {
		result[i].Messages = nil
	}
	return result
}
