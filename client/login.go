package client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const loginTimeout = 5 * time.Minute

var ErrNotAllowed = errors.New("not on this server's allowlist")

// openBrowser is a variable so tests can play the browser.
var openBrowser = func(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // only reaps the process
	return nil
}

// Login logs in through the browser and returns a token for serverURL. The
// server sends the browser back to a listener on 127.0.0.1 with a one-time
// code, which is exchanged together with the PKCE verifier (RFC 7636): a code
// read from the browser's history is useless without it.
func Login(serverURL string, out io.Writer) (token, email string, err error) {
	server, err := parseURL(serverURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid server URL: %w", err)
	}
	base := server.Scheme + "://" + server.Host

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", fmt.Errorf("starting login listener: %w", err)
	}

	state := rand.Text()
	verifier := rand.Text() + rand.Text()
	sum := sha256.Sum256([]byte(verifier))

	loginURL := base + "/cli/login?" + url.Values{
		"port":      {fmt.Sprint(ln.Addr().(*net.TCPAddr).Port)},
		"state":     {state},
		"challenge": {base64.RawURLEncoding.EncodeToString(sum[:])},
	}.Encode()

	type result struct {
		token, email string
		err          error
	}
	done := make(chan result, 1)

	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			// Anything else on this port (a favicon request, another local
			// process) is not the server's redirect.
			if r.URL.Path != "/" || q.Get("state") != state {
				http.NotFound(w, r)
				return
			}

			var res result
			switch q.Get("error") {
			case "":
				res.token, res.email, res.err = exchangeCode(base, q.Get("code"), verifier)
			case "not_allowed":
				res.err = fmt.Errorf("%s is %w; ask the server admin to add it", q.Get("email"), ErrNotAllowed)
			default:
				res.err = errors.New("login with Google failed")
			}

			msg := "Logged in as " + res.email + ". You can close this tab and go back to the terminal."
			if res.err != nil {
				msg = "Login failed: " + res.err.Error() + "."
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Wormhole</title>
<body style="font-family:system-ui,sans-serif;max-width:40rem;margin:3rem auto;padding:0 1rem">
<h1>Wormhole</h1><p>%s</p>`, html.EscapeString(msg))

			select {
			case done <- res:
			default: // a second valid redirect; the first one counts
			}
		}),
	}
	go srv.Serve(ln) //nolint:errcheck // ends with Shutdown below
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx) //nolint:errcheck
	}()

	fmt.Fprintf(out, "Log in to %s in your browser. If it does not open, visit:\n\n  %s\n\n", server.Host, loginURL)
	if err := openBrowser(loginURL); err != nil {
		fmt.Fprintf(out, "Could not open a browser: %v\n", err)
	}

	select {
	case res := <-done:
		return res.token, res.email, res.err
	case <-time.After(loginTimeout):
		return "", "", fmt.Errorf("login timed out after %v", loginTimeout)
	}
}

func exchangeCode(base, code, verifier string) (token, email string, err error) {
	resp, err := (&http.Client{Timeout: 15 * time.Second}).PostForm(base+"/cli/token",
		url.Values{"code": {code}, "verifier": {verifier}})
	if err != nil {
		return "", "", fmt.Errorf("getting token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", fmt.Errorf("getting token: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var got struct{ Token, Email string }
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || got.Token == "" {
		return "", "", fmt.Errorf("getting token: unexpected response")
	}
	return got.Token, got.Email, nil
}
