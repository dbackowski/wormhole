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

func TestAddConnection(t *testing.T) {
	tests := []struct {
		name      string
		domains   []string
		wantErr   bool
		wantCount int
	}{
		{"success", []string{"example.com"}, false, 1},
		{"duplicate domain", []string{"example.com", "example.com"}, true, 1},
		{"different domains", []string{"a.com", "b.com"}, false, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm := NewConnectionManager()
			var err error
			for _, d := range tc.domains {
				_, err = cm.AddConnection(d, nil)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("AddConnection() error = %v, wantErr %v", err, tc.wantErr)
			}
			if cm.Count() != tc.wantCount {
				t.Errorf("Count() = %d, want %d", cm.Count(), tc.wantCount)
			}
		})
	}
}

func TestGetConnection(t *testing.T) {
	tests := []struct {
		name    string
		setup   []string
		domain  string
		wantErr bool
	}{
		{"exists", []string{"example.com"}, "example.com", false},
		{"not found", nil, "missing.com", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm := NewConnectionManager()
			for _, d := range tc.setup {
				cm.AddConnection(d, nil) //nolint:errcheck
				cm.ActivateConnection(d)
			}
			conn, err := cm.GetConnection(tc.domain)
			if (err != nil) != tc.wantErr {
				t.Errorf("GetConnection() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && conn == nil {
				t.Error("expected non-nil Connection")
			}
		})
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
