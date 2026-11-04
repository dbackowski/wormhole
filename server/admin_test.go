package server

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

// adminSession logs in boss@example.com as admin without going through Google.
func adminSession(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	if err := s.auth.store.AddUser("boss@example.com", true); err != nil {
		t.Fatal(err)
	}
	s.auth.setupCode = ""
	id, err := s.auth.sessions.put("boss@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookie, Value: id}
}

// connectTunnel opens app.localhost as dev@example.com, whose token is "secret".
func connectTunnel(t *testing.T, s *Server) *websocket.Conn {
	t.Helper()
	wsURL, cleanup := startWSServer(t, s)
	t.Cleanup(cleanup)

	header := http.Header{}
	header.Set(common.ClientHeader, "1")
	header.Set("Host", "app.localhost")
	header.Set("Authorization", "Bearer secret")
	header.Set("Fly-Client-IP", "203.0.113.7")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws", header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	var msg common.Message
	if err := conn.ReadJSON(&msg); err != nil || msg.Type != common.MessageTypeDomainRegistered {
		t.Fatalf("registration: %v %+v", err, msg)
	}
	return conn
}

func TestAdminDashboard(t *testing.T) {
	s := newTestServerWithAuth(t, "secret")
	session := adminSession(t, s)
	conn := connectTunnel(t, s)

	// One request through the tunnel, answered by the "client".
	go func() {
		var req common.Message
		if conn.ReadJSON(&req) == nil {
			conn.WriteJSON(&common.Message{Type: common.MessageTypeHTTPResponse, UUID: req.UUID, Status: 200}) //nolint:errcheck
		}
	}()
	r := httptest.NewRequest(http.MethodGet, "http://app.localhost/", nil)
	w := httptest.NewRecorder()
	s.routeRequest(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("tunneled request: status %d", w.Code)
	}

	tunnels := s.connManager.Snapshot()
	if len(tunnels) != 1 {
		t.Fatalf("Snapshot() = %+v, want one tunnel", tunnels)
	}
	if got := tunnels[0]; got.Domain != "app" || got.Email != "dev@example.com" || got.ClientIP != "203.0.113.7" || got.Requests != 1 {
		t.Errorf("tunnel = %+v, want app owned by dev@example.com from 203.0.113.7 with 1 request", got)
	}
	if s.requests.Load() != 1 {
		t.Errorf("server requests = %d, want 1", s.requests.Load())
	}

	page := serve(s, http.MethodGet, "/admin", nil, session).Body.String()
	for _, want := range []string{"http://app.localhost", "dev@example.com", "203.0.113.7", "status on"} {
		if !strings.Contains(page, want) {
			t.Errorf("/admin is missing %q", want)
		}
	}

	if w := serve(s, http.MethodGet, "/admin", nil); w.Code != http.StatusFound {
		t.Errorf("/admin without a session: status %d, want redirect to login", w.Code)
	}
}

func TestAdminRemoveUserClosesTunnels(t *testing.T) {
	s := newTestServerWithAuth(t, "secret")
	session := adminSession(t, s)
	conn := connectTunnel(t, s)

	if w := serve(s, http.MethodPost, "/admin/users", url.Values{"action": {"remove"}, "email": {"dev@example.com"}}, session); w.Code != http.StatusSeeOther {
		t.Fatalf("remove: status %d %s", w.Code, w.Body)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	var msg common.Message
	err := conn.ReadJSON(&msg)
	var netErr net.Error
	if err == nil || errors.As(err, &netErr) && netErr.Timeout() {
		t.Errorf("tunnel still open after removing its owner: %v", err)
	}
}

func TestThousandsAndShortDuration(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 12480: "12,480", 1234567: "1,234,567"} {
		if got := thousands(n); got != want {
			t.Errorf("thousands(%d) = %q, want %q", n, got, want)
		}
	}
	for d, want := range map[time.Duration]string{
		45 * time.Second: "45s", 14 * time.Minute: "14m",
		2*time.Hour + 14*time.Minute: "2h 14m", 76 * time.Hour: "3d 4h",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestStore_Domains(t *testing.T) {
	s := newTestServerWithAuth(t, "secret")
	st := s.auth.store

	if err := st.ReserveDomain("app", "dev@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := st.ReserveDomain("app", "dev@example.com"); err != errDomainReserved {
		t.Errorf("reserving twice: err = %v, want errDomainReserved", err)
	}
	if owner, ok, _ := st.DomainOwner("app"); !ok || owner != "dev@example.com" {
		t.Errorf("DomainOwner(app) = %q, %v", owner, ok)
	}

	st.ReleaseDomain("app")
	if _, ok, _ := st.DomainOwner("app"); ok {
		t.Error("app still reserved after release")
	}

	st.ReserveDomain("app", "dev@example.com")
	st.RemoveUser("dev@example.com")
	if _, ok, _ := st.DomainOwner("app"); ok {
		t.Error("app still reserved after its owner was removed")
	}
}

func dialTunnel(t *testing.T, wsURL, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	header.Set(common.ClientHeader, "1")
	header.Set("Host", "app.localhost")
	header.Set("Authorization", "Bearer "+token)
	return websocket.DefaultDialer.Dial(wsURL+"/ws", header)
}

func TestReservedDomain_OnlyOwnerConnects(t *testing.T) {
	s := newTestServerWithAuth(t, "secret") // dev@example.com
	session := adminSession(t, s)           // boss@example.com
	wsURL, cleanup := startWSServer(t, s)
	t.Cleanup(cleanup)

	if w := serve(s, http.MethodPost, "/admin/domains", url.Values{"action": {"reserve"}, "domain": {" App "}, "email": {"boss@example.com"}}, session); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin" {
		t.Fatalf("reserve: %d %s", w.Code, w.Header().Get("Location"))
	}

	_, resp, err := dialTunnel(t, wsURL, "secret")
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("other user claiming a reserved domain: err=%v resp=%v, want 403", err, resp)
	}

	// The owner gets in.
	bossToken, _ := s.auth.store.CreateToken("boss@example.com")
	conn, _, err := dialTunnel(t, wsURL, bossToken)
	if err != nil {
		t.Fatalf("owner dial: %v", err)
	}
	conn.Close()

	if page := serve(s, http.MethodGet, "/admin", nil, session).Body.String(); !strings.Contains(page, ">app</td>") {
		t.Error("/admin does not list the reserved domain")
	}
}

func TestReserveDomain_ClosesSomeoneElsesTunnel(t *testing.T) {
	s := newTestServerWithAuth(t, "secret")
	session := adminSession(t, s)
	conn := connectTunnel(t, s) // dev@example.com on app

	serve(s, http.MethodPost, "/admin/domains", url.Values{"action": {"reserve"}, "domain": {"app"}, "email": {"boss@example.com"}}, session)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	var msg common.Message
	err := conn.ReadJSON(&msg)
	var netErr net.Error
	if err == nil || errors.As(err, &netErr) && netErr.Timeout() {
		t.Errorf("tunnel on the newly reserved domain still open: %v", err)
	}
}

func TestReserveDomain_Rejects(t *testing.T) {
	s := newTestServerWithAuth(t, "secret")
	session := adminSession(t, s)

	for _, tc := range []struct{ domain, email, want string }{
		{"my.app", "dev@example.com", "invalid_domain"},
		{"-app", "dev@example.com", "invalid_domain"},
		{"app", "stranger@example.com", "unknown_user"},
	} {
		w := serve(s, http.MethodPost, "/admin/domains", url.Values{"action": {"reserve"}, "domain": {tc.domain}, "email": {tc.email}}, session)
		if loc := w.Header().Get("Location"); loc != "/admin?error="+tc.want {
			t.Errorf("reserve %q for %s: redirected to %q, want error=%s", tc.domain, tc.email, loc, tc.want)
		}
	}

	serve(s, http.MethodPost, "/admin/domains", url.Values{"action": {"reserve"}, "domain": {"app"}, "email": {"dev@example.com"}}, session)
	w := serve(s, http.MethodPost, "/admin/domains", url.Values{"action": {"reserve"}, "domain": {"app"}, "email": {"boss@example.com"}}, session)
	if loc := w.Header().Get("Location"); loc != "/admin?error=domain_taken" {
		t.Errorf("reserving a reserved domain: redirected to %q, want error=domain_taken", loc)
	}
}
