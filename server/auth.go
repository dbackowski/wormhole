package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Google login, for two audiences: the CLI, which gets a long-lived token
// through a loopback redirect (RFC 8252, with PKCE), and admins, who get a
// session cookie for /admin. The server is the only party talking to Google.

const (
	// __Host- keeps the cookies host-only: tunnel subdomains can neither read
	// nor overwrite them.
	sessionCookie = "__Host-wormhole_session"
	stateCookie   = "__Host-wormhole_login"

	loginTTL   = 10 * time.Minute
	cliCodeTTL = time.Minute
	sessionTTL = 12 * time.Hour

	// Caps each in-memory map. Only allowlisted users add to them: a login
	// in progress lives in its cookie, see pendingLogin.
	maxPending = 1000
)

//go:embed templates/pages.html
var pagesHTML string

var pages = template.Must(template.New("").Funcs(templateFuncs).Parse(pagesHTML))

type googleOAuth struct {
	clientID, clientSecret         string
	authURL, tokenURL, userinfoURL string
	http                           *http.Client
}

func newGoogleOAuth(clientID, clientSecret string) *googleOAuth {
	return &googleOAuth{
		clientID:     clientID,
		clientSecret: clientSecret,
		authURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		tokenURL:     "https://oauth2.googleapis.com/token",
		userinfoURL:  "https://openidconnect.googleapis.com/v1/userinfo",
		http:         &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *googleOAuth) authCodeURL(state, redirectURI string) string {
	return g.authURL + "?" + url.Values{
		"client_id":     {g.clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {"openid email"},
		"state":         {state},
		"prompt":        {"select_account"},
	}.Encode()
}

// verifiedEmail exchanges an authorization code for the user's email, which
// Google must have verified: an unverified address proves nothing.
func (g *googleOAuth) verifiedEmail(ctx context.Context, code, redirectURI string) (string, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {g.clientID},
		"client_secret": {g.clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := g.doJSON(req, &tok); err != nil {
		return "", fmt.Errorf("exchanging code: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, g.userinfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	var info struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := g.doJSON(req, &info); err != nil {
		return "", fmt.Errorf("fetching userinfo: %w", err)
	}
	if info.Email == "" || !info.EmailVerified {
		return "", fmt.Errorf("email %q is not verified", info.Email)
	}
	return normalizeEmail(info.Email), nil
}

func (g *googleOAuth) doJSON(req *http.Request, v any) error {
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: %s", resp.Status, body)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ttlMap holds short-lived entries under random keys. Being in memory, it is
// cleared by a restart: a login in progress or an admin session starts over.
type ttlMap[V any] struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]ttlEntry[V]
}

type ttlEntry[V any] struct {
	v       V
	expires time.Time
}

func newTTLMap[V any](ttl time.Duration) *ttlMap[V] {
	return &ttlMap[V]{ttl: ttl, m: make(map[string]ttlEntry[V])}
}

var errTooManyPending = errors.New("too many logins in progress, try again later")

func (t *ttlMap[V]) put(v V) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	if len(t.m) >= maxPending {
		for k, e := range t.m {
			if now.After(e.expires) {
				delete(t.m, k)
			}
		}
		if len(t.m) >= maxPending {
			return "", errTooManyPending
		}
	}

	key := rand.Text()
	t.m[key] = ttlEntry[V]{v: v, expires: now.Add(t.ttl)}
	return key, nil
}

func (t *ttlMap[V]) get(key string) (V, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lookup(key, false)
}

// take is get for single-use entries.
func (t *ttlMap[V]) take(key string) (V, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lookup(key, true)
}

func (t *ttlMap[V]) lookup(key string, remove bool) (V, bool) {
	e, ok := t.m[key]
	expired := ok && time.Now().After(e.expires)
	if ok && (remove || expired) {
		delete(t.m, key)
	}
	if !ok || expired {
		var zero V
		return zero, false
	}
	return e.v, true
}

func (t *ttlMap[V]) delete(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, key)
}

// pendingLogin is a Google login in progress. It lives in the browser's
// state cookie, signed, not on the server: anyone can start a login, so
// keeping them would let anyone fill the server's memory, or any cap on it.
type pendingLogin struct {
	State   string `json:"s"` // the OAuth state sent to Google
	Expires int64  `json:"e"` // unix seconds

	SetupCode string `json:"setup,omitempty"` // claims the first admin

	// Set for a CLI login: where to send the browser back, and what to check
	// the CLI's code exchange against.
	CLIPort   string `json:"port,omitempty"`
	CLIState  string `json:"cli,omitempty"`
	Challenge string `json:"ch,omitempty"`
}

// sealLogin returns p as a cookie value that only this server process can
// have made.
func (a *auth) sealLogin(p pendingLogin) string {
	data, _ := json.Marshal(p) // strings and an int: cannot fail
	payload := base64.RawURLEncoding.EncodeToString(data)
	return payload + "." + base64.RawURLEncoding.EncodeToString(a.loginMAC(payload))
}

// openLogin returns the login sealed in value, if this process sealed it and
// it has not expired.
func (a *auth) openLogin(value string) (pendingLogin, bool) {
	payload, sig, _ := strings.Cut(value, ".")
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, a.loginMAC(payload)) {
		return pendingLogin{}, false
	}
	var p pendingLogin
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || json.Unmarshal(data, &p) != nil || time.Now().Unix() > p.Expires {
		return pendingLogin{}, false
	}
	return p, true
}

func (a *auth) loginMAC(payload string) []byte {
	m := hmac.New(sha256.New, a.loginKey)
	m.Write([]byte(payload))
	return m.Sum(nil)
}

// cliCode is a one-time code the CLI exchanges for a token.
type cliCode struct {
	email, challenge string
}

type auth struct {
	store    *Store
	google   *googleOAuth
	loginKey []byte // signs pendingLogin; new on each start, which ends logins in progress
	codes    *ttlMap[cliCode]
	sessions *ttlMap[string] // session id -> admin email
	csrf     http.CrossOriginProtection

	setupMu   sync.Mutex
	setupCode string // non-empty while there is no admin
}

// serveAuth handles the login and admin routes on the server's own host. It
// reports false for any other path.
func (s *Server) serveAuth(w http.ResponseWriter, r *http.Request) bool {
	handlers := map[string]http.HandlerFunc{
		"/cli/login":     s.handleCLILogin,
		"/cli/token":     s.handleCLIToken,
		"/auth/callback": s.handleCallback,
		"/admin":         s.handleAdmin,
		"/admin/login":   s.handleAdminLogin,
		"/admin/setup":   s.handleAdminSetup,
		"/admin/users":   s.handleAdminUsers,
		"/admin/domains": s.handleAdminDomains,
		"/admin/logout":  s.handleAdminLogout,
	}
	h, ok := handlers[r.URL.Path]
	if !ok {
		return false
	}
	// Tunnels share this site, so one could frame /admin and trick a click.
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	h(w, r)
	return true
}

func (s *Server) redirectURI(r *http.Request) string {
	return s.forwardedProto(r) + "://" + r.Host + "/auth/callback"
}

func (s *Server) startGoogleLogin(w http.ResponseWriter, r *http.Request, p pendingLogin) {
	p.State = rand.Text()
	p.Expires = time.Now().Add(loginTTL).Unix()
	// Binds the login to this browser, so nobody can hand someone else a
	// callback link that logs them in as the attacker.
	setCookie(w, stateCookie, s.auth.sealLogin(p), loginTTL)
	http.Redirect(w, r, s.auth.google.authCodeURL(p.State, s.redirectURI(r)), http.StatusFound)
}

// handleCLILogin starts a login for the CLI listening on 127.0.0.1:port. Only
// the port is taken from the request, so this cannot redirect anywhere else.
func (s *Server) handleCLILogin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	port, err := strconv.Atoi(q.Get("port"))
	state, challenge := q.Get("state"), q.Get("challenge")

	if err != nil || port < 1 || port > 65535 || state == "" || len(state) > 128 || len(challenge) != 43 {
		renderMessage(w, http.StatusBadRequest, "Invalid login request. Run the wormhole client again.")
		return
	}
	s.startGoogleLogin(w, r, pendingLogin{CLIPort: strconv.Itoa(port), CLIState: state, Challenge: challenge})
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	s.startGoogleLogin(w, r, pendingLogin{})
}

func (s *Server) handleAdminSetup(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if !s.setupCodeValid(code) {
		renderMessage(w, http.StatusNotFound, "This setup link is no longer valid. Use the latest link from the server logs.")
		return
	}
	s.startGoogleLogin(w, r, pendingLogin{SetupCode: code})
}

func (s *Server) setupCodeValid(code string) bool {
	s.auth.setupMu.Lock()
	defer s.auth.setupMu.Unlock()
	return s.auth.setupCode != "" && subtle.ConstantTimeCompare([]byte(code), []byte(s.auth.setupCode)) == 1
}

func (s *Server) setupPending() bool {
	s.auth.setupMu.Lock()
	defer s.auth.setupMu.Unlock()
	return s.auth.setupCode != ""
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var p pendingLogin
	c, err := r.Cookie(stateCookie)
	ok := err == nil
	if ok {
		p, ok = s.auth.openLogin(c.Value)
	}
	if !ok || subtle.ConstantTimeCompare([]byte(p.State), []byte(q.Get("state"))) != 1 {
		renderMessage(w, http.StatusBadRequest, "This login has expired or was started in another browser. Start again.")
		return
	}
	setCookie(w, stateCookie, "", -1)

	email, err := s.googleEmail(r)
	if err != nil {
		s.Logger.Warn("Google login failed", "error", err)
		if p.CLIPort != "" {
			redirectToCLI(w, r, p, url.Values{"error": {"login_failed"}})
			return
		}
		renderMessage(w, http.StatusBadGateway, "Google login failed. Try again.")
		return
	}

	switch {
	case p.SetupCode != "":
		s.finishSetup(w, r, p.SetupCode, email)
	case p.CLIPort != "":
		s.finishCLILogin(w, r, p, email)
	default:
		s.finishAdminLogin(w, r, email)
	}
}

func (s *Server) googleEmail(r *http.Request) (string, error) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		return "", fmt.Errorf("google returned %q", e)
	}
	return s.auth.google.verifiedEmail(r.Context(), q.Get("code"), s.redirectURI(r))
}

func redirectToCLI(w http.ResponseWriter, r *http.Request, p pendingLogin, v url.Values) {
	v.Set("state", p.CLIState)
	http.Redirect(w, r, "http://127.0.0.1:"+p.CLIPort+"/?"+v.Encode(), http.StatusFound)
}

func (s *Server) finishCLILogin(w http.ResponseWriter, r *http.Request, p pendingLogin, email string) {
	_, allowed, err := s.auth.store.User(email)
	if err != nil {
		s.Logger.Error("User lookup failed", "error", err)
		// Back to the CLI, which otherwise waits out its login timeout.
		redirectToCLI(w, r, p, url.Values{"error": {"login_failed"}})
		return
	}
	if !allowed {
		s.Logger.Info("CLI login refused: email not on the allowlist", "email", email)
		redirectToCLI(w, r, p, url.Values{"error": {"not_allowed"}, "email": {email}})
		return
	}

	code, err := s.auth.codes.put(cliCode{email: email, challenge: p.Challenge})
	if err != nil {
		s.Logger.Warn("CLI login failed", "email", email, "error", err)
		redirectToCLI(w, r, p, url.Values{"error": {"login_failed"}})
		return
	}
	redirectToCLI(w, r, p, url.Values{"code": {code}})
}

// handleCLIToken trades a one-time code for a token. The PKCE verifier proves
// the caller is the CLI that started the login, not something that read the
// code from the browser's history.
func (s *Server) handleCLIToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)

	c, ok := s.auth.codes.take(r.PostFormValue("code"))
	sum := sha256.Sum256([]byte(r.PostFormValue("verifier")))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if !ok || subtle.ConstantTimeCompare([]byte(challenge), []byte(c.challenge)) != 1 {
		http.Error(w, "invalid or expired code", http.StatusBadRequest)
		return
	}

	// Fails on the foreign key if the user was removed since the redirect.
	token, err := s.auth.store.CreateToken(c.email)
	if err != nil {
		s.Logger.Error("Creating token failed", "email", c.email, "error", err)
		http.Error(w, "could not create token", http.StatusInternalServerError)
		return
	}

	s.Logger.Info("CLI logged in", "email", c.email)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token, "email": c.email}) //nolint:errcheck
}

func (s *Server) finishSetup(w http.ResponseWriter, r *http.Request, code, email string) {
	s.auth.setupMu.Lock()
	defer s.auth.setupMu.Unlock()

	if s.auth.setupCode == "" || subtle.ConstantTimeCompare([]byte(code), []byte(s.auth.setupCode)) != 1 {
		renderMessage(w, http.StatusNotFound, "This setup link is no longer valid.")
		return
	}
	if err := s.auth.store.AddUser(email, true); err != nil {
		s.Logger.Error("Adding first admin failed", "error", err)
		renderMessage(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	s.auth.setupCode = ""
	s.Logger.Info("First admin set up", "email", email)
	s.startSession(w, r, email)
}

func (s *Server) finishAdminLogin(w http.ResponseWriter, r *http.Request, email string) {
	user, ok, err := s.auth.store.User(email)
	if err != nil {
		s.Logger.Error("User lookup failed", "error", err)
		renderMessage(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	if !ok || !user.IsAdmin {
		renderMessage(w, http.StatusForbidden, email+" is not an admin.")
		return
	}
	s.startSession(w, r, email)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, email string) {
	id, err := s.auth.sessions.put(email)
	if err != nil {
		renderMessage(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	setCookie(w, sessionCookie, id, sessionTTL)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// sessionAdmin returns the admin logged in on r. The user is looked up on
// every request, so removal or demotion takes effect at once.
func (s *Server) sessionAdmin(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	email, ok := s.auth.sessions.get(c.Value)
	if !ok {
		return "", false
	}
	user, ok, err := s.auth.store.User(email)
	return email, err == nil && ok && user.IsAdmin
}

// checkAdminPost guards the admin forms. A tunnel page is same-site with this
// host, so SameSite cookies alone would let it submit them.
func (s *Server) checkAdminPost(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return "", false
	}
	if err := s.auth.csrf.Check(r); err != nil {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return "", false
	}
	email, ok := s.sessionAdmin(r)
	if !ok {
		http.Error(w, "Forbidden", http.StatusForbidden)
	}
	return email, ok
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.checkAdminPost(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)

	email := normalizeEmail(r.PostFormValue("email"))
	action := r.PostFormValue("action")
	if !strings.Contains(email, "@") {
		redirectAdminError(w, r, "invalid_email")
		return
	}

	var err error
	switch action {
	case "add":
		err = s.auth.store.AddUser(email, false)
	case "remove":
		err = s.auth.store.RemoveUser(email)
	case "promote":
		err = s.auth.store.SetAdmin(email, true)
	case "demote":
		err = s.auth.store.SetAdmin(email, false)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	if errors.Is(err, errLastAdmin) {
		redirectAdminError(w, r, "last_admin")
		return
	}
	if err != nil {
		s.Logger.Error("Updating allowlist failed", "action", action, "email", email, "error", err)
		redirectAdminError(w, r, "internal")
		return
	}

	// Removal revokes the tokens; closing the tunnels makes it take effect now.
	// The client's reconnect then gets a 401 and stops.
	if action == "remove" {
		if n := s.connManager.CloseByEmail(email); n > 0 {
			s.Logger.Info("Closed tunnels of removed user", "email", email, "tunnels", n)
		}
	}

	s.Logger.Info("Allowlist changed", "by", admin, "action", action, "email", email)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.checkAdminPost(w, r); !ok {
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.auth.sessions.delete(c.Value)
	}
	setCookie(w, sessionCookie, "", -1)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// setCookie sets a __Host- cookie; ttl < 0 deletes it.
func setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	maxAge := int(ttl.Seconds())
	if ttl < 0 {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

func renderMessage(w http.ResponseWriter, status int, msg string) {
	render(w, status, "message", msg)
}

func render(w http.ResponseWriter, status int, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	pages.ExecuteTemplate(w, page, data) //nolint:errcheck // headers are already out
}
