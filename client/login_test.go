package client

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// fakeLogin plays the server and the browser: the browser hits the CLI's
// listener with redirect (built from the /cli/login query), and /cli/token
// hands out a token for the code "good" if the verifier matches.
func fakeLogin(t *testing.T, redirect func(login url.Values) url.Values) string {
	t.Helper()
	var challenge string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sum := sha256.Sum256([]byte(r.PostFormValue("verifier")))
		if r.URL.Path != "/cli/token" || r.PostFormValue("code") != "good" ||
			base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			http.Error(w, "invalid or expired code", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"token": "tok", "email": "dev@example.com"})
	}))
	t.Cleanup(srv.Close)

	old := openBrowser
	t.Cleanup(func() { openBrowser = old })
	openBrowser = func(loginURL string) error {
		u, _ := url.Parse(loginURL)
		q := u.Query()
		challenge = q.Get("challenge")
		go func() {
			// A stray request with the wrong state must be ignored.
			http.Get("http://127.0.0.1:" + q.Get("port") + "/?state=wrong&code=good")
			resp, err := http.Get("http://127.0.0.1:" + q.Get("port") + "/?" + redirect(q).Encode())
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	return srv.URL
}

func TestLogin(t *testing.T) {
	server := fakeLogin(t, func(q url.Values) url.Values {
		return url.Values{"state": {q.Get("state")}, "code": {"good"}}
	})

	token, email, err := Login(server, io.Discard)
	if err != nil || token != "tok" || email != "dev@example.com" {
		t.Errorf("Login() = %q, %q, %v; want tok, dev@example.com", token, email, err)
	}
}

func TestLogin_NotAllowed(t *testing.T) {
	server := fakeLogin(t, func(q url.Values) url.Values {
		return url.Values{"state": {q.Get("state")}, "error": {"not_allowed"}, "email": {"stranger@example.com"}}
	})

	_, _, err := Login(server, io.Discard)
	if !errors.Is(err, ErrNotAllowed) {
		t.Errorf("Login() error = %v, want ErrNotAllowed", err)
	}
}

func TestLogin_BadCode(t *testing.T) {
	server := fakeLogin(t, func(q url.Values) url.Values {
		return url.Values{"state": {q.Get("state")}, "code": {"stolen"}}
	})

	if _, _, err := Login(server, io.Discard); err == nil {
		t.Error("Login() succeeded with a code the server rejects")
	}
}
