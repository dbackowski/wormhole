package client

import "sync"

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

func (rh *RequestHistory) Add(log RequestLog) {
	rh.mutex.Lock()
	defer rh.mutex.Unlock()

	rh.logs = append(rh.logs, log)
}

func (rh *RequestHistory) GetRecent(n int) []RequestLog {
	rh.mutex.RLock()
	defer rh.mutex.RUnlock()

	start := 0
	if len(rh.logs) > n {
		start = len(rh.logs) - n
	}

	result := make([]RequestLog, len(rh.logs[start:]))
	copy(result, rh.logs[start:])
	return result
}
