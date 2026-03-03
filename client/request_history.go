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
	if len(rh.logs) > rh.maxSize {
		rh.logs = rh.logs[len(rh.logs)-rh.maxSize:]
	}
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
	return result
}
