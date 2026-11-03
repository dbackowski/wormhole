package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeGoogle stands in for Google's token and userinfo endpoints. The
// authorization code is the email to log in as.
func fakeGoogle(t *testing.T, s *Server) {
	t.Helper()
	g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.PostFormValue("client_secret") != "client-secret" {
				http.Error(w, "bad client", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"access_token": r.PostFormValue("code")})
		case "/userinfo":
			email := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			json.NewEncoder(w).Encode(map[string]any{"email": email, "email_verified": email != "unverified@example.com"})
		}
	}))
	t.Cleanup(g.Close)
	s.auth.google.tokenURL = g.URL + "/token"
	s.auth.google.userinfoURL = g.URL + "/userinfo"
}

func serve(s *Server, method, target string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r.Host = "localhost"
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.routeRequest(w, r)
	return w
}

func cookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s cookie set (status %d)", name, w.Code)
	return nil
}

// googleLogin starts a login at start and completes it at Google as email,
// returning the callback's response.
func googleLogin(t *testing.T, s *Server, start, email string) *httptest.ResponseRecorder {
	t.Helper()
	w := serve(s, http.MethodGet, start, nil)
	if w.Code != http.StatusFound {
		t.Fatalf("GET %s = %d, want redirect to Google: %s", start, w.Code, w.Body)
	}
	loc, _ := url.Parse(w.Header().Get("Location"))
	state := loc.Query().Get("state")
	if loc.Query().Get("redirect_uri") != "http://localhost/auth/callback" {
		t.Errorf("redirect_uri = %q", loc.Query().Get("redirect_uri"))
	}
	return serve(s, http.MethodGet, "/auth/callback?"+url.Values{"state": {state}, "code": {email}}.Encode(),
		nil, cookie(t, w, stateCookie))
}

const verifier = "a-very-long-random-verifier-made-up-by-the-cli-process"

func challengeFor(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func cliLogin(t *testing.T, s *Server, email string) url.Values {
	t.Helper()
	start := "/cli/login?" + url.Values{"port": {"4321"}, "state": {"cli-state"}, "challenge": {challengeFor(verifier)}}.Encode()
	w := googleLogin(t, s, start, email)
	loc, _ := url.Parse(w.Header().Get("Location"))
	if loc.Host != "127.0.0.1:4321" {
		t.Fatalf("callback redirected to %q, want the CLI on 127.0.0.1:4321", w.Header().Get("Location"))
	}
	if loc.Query().Get("state") != "cli-state" {
		t.Errorf("state = %q, want cli-state", loc.Query().Get("state"))
	}
	return loc.Query()
}

func TestCLILogin(t *testing.T) {
	s := newTestServerWithAuth(t, "unused")
	fakeGoogle(t, s)

	code := cliLogin(t, s, "dev@example.com").Get("code")

	if w := serve(s, http.MethodPost, "/cli/token", url.Values{"code": {code}, "verifier": {"wrong"}}); w.Code != http.StatusBadRequest {
		t.Errorf("wrong verifier: status %d, want 400", w.Code)
	}

	// The wrong verifier above used the code up.
	code = cliLogin(t, s, "dev@example.com").Get("code")
	w := serve(s, http.MethodPost, "/cli/token", url.Values{"code": {code}, "verifier": {verifier}})
	var got struct{ Token, Email string }
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil || got.Email != "dev@example.com" {
		t.Fatalf("token response = %d %+v %v", w.Code, got, err)
	}

	if w := serve(s, http.MethodPost, "/cli/token", url.Values{"code": {code}, "verifier": {verifier}}); w.Code != http.StatusBadRequest {
		t.Errorf("reused code: status %d, want 400", w.Code)
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+got.Token)
	if email, ok := s.authenticateRequest(r); !ok || email != "dev@example.com" {
		t.Errorf("issued token: authenticateRequest = %q, %v", email, ok)
	}
}

func TestCLILogin_NotAllowed(t *testing.T) {
	s := newTestServerWithAuth(t, "unused")
	fakeGoogle(t, s)

	q := cliLogin(t, s, "stranger@example.com")
	if q.Get("error") != "not_allowed" || q.Get("code") != "" {
		t.Errorf("redirect query = %v, want error=not_allowed and no code", q)
	}

	q = cliLogin(t, s, "unverified@example.com")
	if q.Get("error") != "login_failed" {
		t.Errorf("unverified email: redirect query = %v, want error=login_failed", q)
	}
}

func TestCLILogin_RejectsBadRequests(t *testing.T) {
	s := newTestServerWithAuth(t, "unused")
	for _, q := range []string{
		"port=0&state=x&challenge=" + challengeFor(verifier),
		"port=evil.com&state=x&challenge=" + challengeFor(verifier),
		"port=4321&state=&challenge=" + challengeFor(verifier),
		"port=4321&state=x&challenge=short",
	} {
		if w := serve(s, http.MethodGet, "/cli/login?"+q, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, w.Code)
		}
	}
}

func TestCallback_RequiresStateCookie(t *testing.T) {
	s := newTestServerWithAuth(t, "unused")
	fakeGoogle(t, s)

	w := serve(s, http.MethodGet, "/admin/login", nil)
	loc, _ := url.Parse(w.Header().Get("Location"))
	w = serve(s, http.MethodGet, "/auth/callback?state="+loc.Query().Get("state")+"&code=dev@example.com", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("callback without the state cookie: status %d, want 400", w.Code)
	}
}

func TestAdminSetupAndPanel(t *testing.T) {
	s := newTestServerWithAuth(t, "unused")
	fakeGoogle(t, s)

	if w := serve(s, http.MethodGet, "/admin", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "No admin yet") {
		t.Fatalf("/admin before setup = %d %s", w.Code, w.Body)
	}
	if w := serve(s, http.MethodGet, "/admin/setup?code=guess", nil); w.Code != http.StatusNotFound {
		t.Errorf("wrong setup code: status %d, want 404", w.Code)
	}

	code := s.auth.setupCode
	w := googleLogin(t, s, "/admin/setup?code="+code, "boss@example.com")
	session := cookie(t, w, sessionCookie)

	if w := serve(s, http.MethodGet, "/admin/setup?code="+code, nil); w.Code != http.StatusNotFound {
		t.Errorf("setup link reused: status %d, want 404", w.Code)
	}
	if w := serve(s, http.MethodGet, "/admin", nil, session); !strings.Contains(w.Body.String(), "boss@example.com") {
		t.Errorf("/admin as admin = %d %s", w.Code, w.Body)
	}

	// Same-site form posts from a tunnel page are refused.
	r := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader("action=add&email=x@example.com"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-site")
	r.Host = "localhost"
	r.AddCookie(session)
	rec := httptest.NewRecorder()
	s.routeRequest(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Errorf("same-site POST: status %d, want 403", rec.Code)
	}

	if w := serve(s, http.MethodPost, "/admin/users", url.Values{"action": {"add"}, "email": {" New@Example.com "}}, session); w.Code != http.StatusSeeOther {
		t.Fatalf("add user: status %d %s", w.Code, w.Body)
	}
	if _, ok, _ := s.auth.store.User("new@example.com"); !ok {
		t.Error("added user is not on the allowlist")
	}

	w = serve(s, http.MethodPost, "/admin/users", url.Values{"action": {"demote"}, "email": {"boss@example.com"}}, session)
	if loc := w.Header().Get("Location"); loc != "/admin?error=last_admin" {
		t.Errorf("demoting the last admin: redirected to %q, want the last_admin banner", loc)
	}
	if w := serve(s, http.MethodGet, "/admin?error=last_admin", nil, session); !strings.Contains(w.Body.String(), "at least one admin") {
		t.Error("/admin?error=last_admin shows no banner")
	}
	if w := serve(s, http.MethodGet, "/admin?error=Call+us+now", nil, session); strings.Contains(w.Body.String(), "Call us now") {
		t.Error("/admin shows text taken from the URL")
	}

	if w := serve(s, http.MethodPost, "/admin/users", url.Values{"action": {"add"}, "email": {"x@example.com"}}); w.Code != http.StatusForbidden {
		t.Errorf("POST without a session: status %d, want 403", w.Code)
	}

	// A non-admin's Google login gets no session.
	w = googleLogin(t, s, "/admin/login", "dev@example.com")
	if w.Code != http.StatusForbidden {
		t.Errorf("admin login as non-admin: status %d, want 403", w.Code)
	}
}

func TestStore_LastAdmin(t *testing.T) {
	s := newTestServerWithAuth(t, "unused")
	st := s.auth.store
	st.AddUser("a@example.com", true)

	if err := st.RemoveUser("a@example.com"); err != errLastAdmin {
		t.Errorf("removing the last admin: err = %v, want errLastAdmin", err)
	}
	st.AddUser("b@example.com", true)
	if err := st.SetAdmin("a@example.com", false); err != nil {
		t.Errorf("demoting one of two admins: %v", err)
	}
	if err := st.RemoveUser("b@example.com"); err != errLastAdmin {
		t.Errorf("removing the remaining admin: err = %v, want errLastAdmin", err)
	}
	if err := st.RemoveUser("dev@example.com"); err != nil {
		t.Errorf("removing a user: %v", err)
	}
}
