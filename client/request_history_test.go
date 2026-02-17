package client

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func makeLog(id int) RequestLog {
	return RequestLog{
		UUID:       fmt.Sprintf("uuid-%d", id),
		Timestamp:  time.Now(),
		Method:     "GET",
		URL:        fmt.Sprintf("/path/%d", id),
		StatusCode: 200,
	}
}

func TestNewRequestHistory(t *testing.T) {
	rh := NewRequestHistory(10)

	if rh.maxSize != 10 {
		t.Errorf("expected maxSize 10, got %d", rh.maxSize)
	}
	if len(rh.logs) != 0 {
		t.Errorf("expected empty logs, got %d", len(rh.logs))
	}
}

func TestAdd(t *testing.T) {
	rh := NewRequestHistory(5)

	rh.Add(makeLog(1))
	rh.Add(makeLog(2))

	if len(rh.logs) != 2 {
		t.Errorf("expected 2 logs, got %d", len(rh.logs))
	}
}

func TestAddEvictsOldEntries(t *testing.T) {
	rh := NewRequestHistory(3)

	for i := 1; i <= 5; i++ {
		rh.Add(makeLog(i))
	}

	if len(rh.logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(rh.logs))
	}

	for i, expected := range []int{3, 4, 5} {
		if rh.logs[i].UUID != fmt.Sprintf("uuid-%d", expected) {
			t.Errorf("logs[%d]: expected uuid-%d, got %s", i, expected, rh.logs[i].UUID)
		}
	}
}

func TestGetRecent(t *testing.T) {
	rh := NewRequestHistory(10)

	for i := 1; i <= 5; i++ {
		rh.Add(makeLog(i))
	}

	tests := []struct {
		name      string
		n         int
		wantLen   int
		firstUUID string
	}{
		{"fewer than available", 3, 3, "uuid-3"},
		{"exact count", 5, 5, "uuid-1"},
		{"more than available", 10, 5, "uuid-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rh.GetRecent(tt.n)
			if len(result) != tt.wantLen {
				t.Errorf("expected %d results, got %d", tt.wantLen, len(result))
			}
			if result[0].UUID != tt.firstUUID {
				t.Errorf("expected first UUID %s, got %s", tt.firstUUID, result[0].UUID)
			}
		})
	}
}

func TestGetRecentReturnsACopy(t *testing.T) {
	rh := NewRequestHistory(10)
	rh.Add(makeLog(1))

	result := rh.GetRecent(1)
	result[0].UUID = "modified"

	if rh.logs[0].UUID == "modified" {
		t.Error("GetRecent should return a copy, not a reference to internal data")
	}
}

func TestGetRecentEmpty(t *testing.T) {
	rh := NewRequestHistory(10)

	result := rh.GetRecent(5)
	if len(result) != 0 {
		t.Errorf("expected 0 results for empty history, got %d", len(result))
	}
}

func TestConcurrentAccess(t *testing.T) {
	rh := NewRequestHistory(100)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rh.Add(makeLog(id))
		}(i)
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rh.GetRecent(10)
		}()
	}

	wg.Wait()
}
