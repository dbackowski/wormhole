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

func TestClear(t *testing.T) {
	rh := NewRequestHistory(5)

	rh.Add(makeLog(1))
	rh.Add(makeLog(2))
	rh.Clear()

	if len(rh.logs) != 0 {
		t.Fatalf("expected 0 logs after Clear, got %d", len(rh.logs))
	}

	rh.Add(makeLog(3))

	recent := rh.GetRecent(5)
	if len(recent) != 1 || recent[0].UUID != "uuid-3" {
		t.Errorf("after Clear then Add, got %+v, want only uuid-3", recent)
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

func TestAdd_CapsAndCopiesLargeBodies(t *testing.T) {
	rh := NewRequestHistory(5)
	large := make([]byte, MaxStoredBodySize+1000)
	small := []byte("small")

	rh.Add(RequestLog{RequestBody: small, ResponseBody: large})
	got := rh.GetRecent(1)[0]

	if len(got.ResponseBody) != MaxStoredBodySize {
		t.Errorf("stored response body = %d bytes, want %d", len(got.ResponseBody), MaxStoredBodySize)
	}
	if got.ResponseBodySize != len(large) {
		t.Errorf("ResponseBodySize = %d, want %d", got.ResponseBodySize, len(large))
	}
	if cap(got.ResponseBody) >= len(large) {
		t.Error("truncated body still references the original buffer")
	}
	if string(got.RequestBody) != "small" || got.RequestBodySize != len(small) {
		t.Errorf("small body = %q (size %d), want unchanged", got.RequestBody, got.RequestBodySize)
	}
}

// Entries no longer visible must not stay reachable through the backing array,
// or their bodies cannot be garbage collected.
func TestDroppedAndClearedEntriesAreReleased(t *testing.T) {
	rh := NewRequestHistory(3)
	for i := range 10 {
		log := makeLog(i)
		log.ResponseBody = []byte("body")
		rh.Add(log)
	}

	for i, e := range rh.logs[len(rh.logs):cap(rh.logs)] {
		if e.ResponseBody != nil {
			t.Errorf("dropped slot %d still holds a body", i)
		}
	}

	rh.Clear()

	for i, e := range rh.logs[:cap(rh.logs)] {
		if e.ResponseBody != nil {
			t.Errorf("slot %d still holds a body after Clear", i)
		}
	}
}
