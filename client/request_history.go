package client

import (
	"bytes"
	"slices"
	"sync"
)

// MaxStoredBodySize caps each body kept in the history. Bodies can be up to
// 10 MB and the dashboard re-fetches the whole history every second, so only
// the head is kept, enough for typical JSON and webhook payloads.
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
}

func NewRequestHistory(maxSize int) *RequestHistory {
	return &RequestHistory{
		logs:    make([]RequestLog, 0, maxSize),
		maxSize: maxSize,
	}
}

// capBody copies rather than reslices, so a truncated entry does not keep the
// full original buffer alive.
func capBody(b []byte) []byte {
	if len(b) <= MaxStoredBodySize {
		return b
	}
	return bytes.Clone(b[:MaxStoredBodySize])
}

func (rh *RequestHistory) Add(log RequestLog) {
	log.RequestBodySize = len(log.RequestBody)
	log.ResponseBodySize = len(log.ResponseBody)
	log.RequestBody = capBody(log.RequestBody)
	log.ResponseBody = capBody(log.ResponseBody)

	rh.mutex.Lock()
	defer rh.mutex.Unlock()

	rh.logs = append(rh.logs, log)
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

func (rh *RequestHistory) GetRecent(n int) []RequestLog {
	rh.mutex.RLock()
	defer rh.mutex.RUnlock()

	start := 0
	if len(rh.logs) > n {
		start = len(rh.logs) - n
	}

	slice := rh.logs[start:]
	result := make([]RequestLog, len(slice))
	copy(result, slice)
	// Update trims Messages in place, so copies must not share them.
	for i := range result {
		result[i].Messages = nil
	}
	return result
}
