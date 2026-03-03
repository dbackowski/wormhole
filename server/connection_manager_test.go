package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

func newTestWSPair(t *testing.T) (client *websocket.Conn, cleanup func()) {
	t.Helper()

	u := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.Upgrade(w, r, nil) //nolint:errcheck
	}))

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial websocket: %v", err)
	}

	return conn, func() {
		conn.Close()
		srv.Close()
	}
}

func TestNewConnectionManager(t *testing.T) {
	cm := NewConnectionManager()
	if cm == nil {
		t.Fatal("expected non-nil ConnectionManager")
	}
	if cm.Count() != 0 {
		t.Errorf("Count() = %d, want 0", cm.Count())
	}
}

func TestAddConnection_Success(t *testing.T) {
	cm := NewConnectionManager()
	err := cm.AddConnection("example.com", nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cm.Count() != 1 {
		t.Errorf("Count() = %d, want 1", cm.Count())
	}
}

func TestAddConnection_DuplicateDomain(t *testing.T) {
	cm := NewConnectionManager()
	cm.AddConnection("example.com", nil) //nolint:errcheck
	err := cm.AddConnection("example.com", nil)
	if err == nil {
		t.Error("expected error for duplicate domain, got nil")
	}
}

func TestAddConnection_DifferentDomains(t *testing.T) {
	cm := NewConnectionManager()
	cm.AddConnection("a.com", nil) //nolint:errcheck
	cm.AddConnection("b.com", nil) //nolint:errcheck
	if cm.Count() != 2 {
		t.Errorf("Count() = %d, want 2", cm.Count())
	}
}

func TestGetConnection_Exists(t *testing.T) {
	cm := NewConnectionManager()
	cm.AddConnection("example.com", nil) //nolint:errcheck
	conn, err := cm.GetConnection("example.com")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if conn == nil {
		t.Error("expected non-nil Connection")
	}
}

func TestGetConnection_NotFound(t *testing.T) {
	cm := NewConnectionManager()
	_, err := cm.GetConnection("missing.com")
	if err == nil {
		t.Error("expected error for missing domain, got nil")
	}
}

func TestRemoveConnection(t *testing.T) {
	cm := NewConnectionManager()
	cm.AddConnection("example.com", nil) //nolint:errcheck
	cm.RemoveConnection("example.com")
	if cm.Count() != 0 {
		t.Errorf("Count() = %d, want 0 after remove", cm.Count())
	}
	_, err := cm.GetConnection("example.com")
	if err == nil {
		t.Error("expected error after removal, got nil")
	}
}

func TestRemoveConnection_NonExistent(t *testing.T) {
	cm := NewConnectionManager()
	// should not panic
	cm.RemoveConnection("missing.com")
}

func TestCount(t *testing.T) {
	cm := NewConnectionManager()
	if cm.Count() != 0 {
		t.Errorf("Count() = %d, want 0", cm.Count())
	}
	cm.AddConnection("a.com", nil) //nolint:errcheck
	if cm.Count() != 1 {
		t.Errorf("Count() = %d, want 1", cm.Count())
	}
	cm.AddConnection("b.com", nil) //nolint:errcheck
	if cm.Count() != 2 {
		t.Errorf("Count() = %d, want 2", cm.Count())
	}
	cm.RemoveConnection("a.com")
	if cm.Count() != 1 {
		t.Errorf("Count() = %d, want 1 after remove", cm.Count())
	}
}

func TestCloseAll(t *testing.T) {
	ws1, cleanup1 := newTestWSPair(t)
	defer cleanup1()
	ws2, cleanup2 := newTestWSPair(t)
	defer cleanup2()

	cm := NewConnectionManager()
	cm.AddConnection("a.com", ws1) //nolint:errcheck
	cm.AddConnection("b.com", ws2) //nolint:errcheck

	cm.CloseAll()

	if cm.Count() != 0 {
		t.Errorf("Count() = %d, want 0 after CloseAll", cm.Count())
	}
}

func TestConnectionManager_ConcurrentAccess(t *testing.T) {
	cm := NewConnectionManager()
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			domain := strings.Repeat("x", i+1) + ".com"
			cm.AddConnection(domain, nil) //nolint:errcheck
			cm.GetConnection(domain)      //nolint:errcheck
			cm.Count()
		}(i)
	}
	wg.Wait()
}
