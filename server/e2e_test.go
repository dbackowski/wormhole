package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dbackowski/wormhole/client"
	"github.com/gorilla/websocket"
)

// e2e runs the real server and client with app as the local app, and returns
// a browser that reaches app through the tunnel.
type e2e struct {
	t       *testing.T
	server  *httptest.Server
	browser *http.Client
}

func startE2E(t *testing.T, app http.Handler) *e2e {
	t.Helper()
	appServer := httptest.NewServer(app)
	t.Cleanup(appServer.Close)

	s := newTestServer(t)
	ts := httptest.NewServer(http.HandlerFunc(s.routeRequest))
	t.Cleanup(ts.Close)
	t.Cleanup(func() { s.connManager.CloseAll() })

	// app.localhost does not resolve everywhere, so every dial goes to ts.
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", ts.Listener.Addr().String())
	}
	saved := websocket.DefaultDialer
	websocket.DefaultDialer = &websocket.Dialer{NetDialContext: dial, HandshakeTimeout: 5 * time.Second}
	t.Cleanup(func() { websocket.DefaultDialer = saved })

	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	c, err := client.NewClient(&client.Config{
		ServerURL: "http://localhost:" + port,
		Domain:    "app",
		Local:     appServer.URL,
		WebUIPort: 4040,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	go c.HandleConnection()
	t.Cleanup(func() { c.Shutdown() }) //nolint:errcheck

	return &e2e{t: t, server: ts, browser: &http.Client{
		Transport: &http.Transport{DialContext: dial},
		Timeout:   30 * time.Second,
	}}
}

func (e *e2e) do(method, path string, body io.Reader) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(method, e.server.URL+path, body)
	if err != nil {
		e.t.Fatalf("NewRequest() error = %v", err)
	}
	req.Host = "app.localhost"
	resp, err := e.browser.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s error = %v", method, path, err)
	}
	e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return b
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// digestApp answers with the length and hash of the body it received, and how
// it was framed.
var digestApp = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fmt.Fprintf(w, "%d %s %d %v", len(body), sum(body), r.ContentLength, r.TransferEncoding)
})

func TestE2E_LargeUploadIsStreamedIntact(t *testing.T) {
	e := startE2E(t, digestApp)
	body := randomBytes(30 << 20)

	resp := e.do(http.MethodPost, "/upload", bytes.NewReader(body))
	got, _ := io.ReadAll(resp.Body)

	want := fmt.Sprintf("%d %s %d []", len(body), sum(body), len(body))
	if resp.StatusCode != http.StatusOK || string(got) != want {
		t.Errorf("got %d %q, want 200 %q", resp.StatusCode, got, want)
	}
}

func TestE2E_ChunkedUploadStaysChunked(t *testing.T) {
	e := startE2E(t, digestApp)
	body := randomBytes(3 << 20)

	// A reader of unknown length makes the browser send it chunked.
	resp := e.do(http.MethodPost, "/upload", io.MultiReader(bytes.NewReader(body)))
	got, _ := io.ReadAll(resp.Body)

	want := fmt.Sprintf("%d %s -1 [chunked]", len(body), sum(body))
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestE2E_RequestWithoutBodyHasNone(t *testing.T) {
	e := startE2E(t, digestApp)

	resp := e.do(http.MethodGet, "/", nil)
	got, _ := io.ReadAll(resp.Body)

	if want := fmt.Sprintf("0 %s 0 []", sum(nil)); string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestE2E_LargeDownloadThroughSlowReader(t *testing.T) {
	body := randomBytes(20 << 20)
	e := startE2E(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body) //nolint:errcheck
	}))

	resp := e.do(http.MethodGet, "/file", nil)
	var got bytes.Buffer
	buf := make([]byte, 64<<10)
	for {
		n, err := resp.Body.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			break
		}
		if got.Len()%(4<<20) < len(buf) {
			time.Sleep(20 * time.Millisecond)
		}
	}

	if got.Len() != len(body) || sum(got.Bytes()) != sum(body) {
		t.Errorf("received %d bytes, want the %d sent", got.Len(), len(body))
	}
}

func TestE2E_ServerSentEventsArriveAsWritten(t *testing.T) {
	next := make(chan struct{})
	e := startE2E(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		<-next
		fmt.Fprint(w, "data: two\n\n")
	}))

	resp := e.do(http.MethodGet, "/events", nil)
	first := make([]byte, len("data: one\n\n"))
	if _, err := io.ReadFull(resp.Body, first); err != nil || string(first) != "data: one\n\n" {
		t.Fatalf("first event = %q, %v; it must arrive before the response ends", first, err)
	}
	close(next)
	rest, _ := io.ReadAll(resp.Body)
	if string(rest) != "data: two\n\n" {
		t.Errorf("second event = %q", rest)
	}
}

func TestE2E_BrowserLeavingCancelsLocalRequest(t *testing.T) {
	cancelled := make(chan struct{})
	e := startE2E(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))

	resp := e.do(http.MethodGet, "/events", nil)
	resp.Body.Close()
	e.browser.CloseIdleConnections()

	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("local request still running after the browser left")
	}
}

// A response the local app breaks off must not reach the browser looking
// complete, which with chunked encoding it otherwise would.
func TestE2E_CutResponseIsNotComplete(t *testing.T) {
	e := startE2E(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("partial")) //nolint:errcheck
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))

	resp := e.do(http.MethodGet, "/", nil)
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Error("cut response read to a clean end")
	}
}

func TestE2E_EarlyResponseDuringUpload(t *testing.T) {
	e := startE2E(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "too big", http.StatusRequestEntityTooLarge)
	}))

	resp := e.do(http.MethodPost, "/upload", bytes.NewReader(randomBytes(4<<20)))
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusRequestEntityTooLarge || strings.TrimSpace(string(got)) != "too big" {
		t.Errorf("got %d %q, want 413 %q", resp.StatusCode, got, "too big")
	}
}

// A message several windows long crosses in both directions.
func TestE2E_WebSocketEchoesLargeMessage(t *testing.T) {
	upgrader := websocket.Upgrader{}
	e := startE2E(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			kind, msg, err := conn.ReadMessage()
			if err != nil || conn.WriteMessage(kind, msg) != nil {
				return
			}
		}
	}))

	dialer := websocket.Dialer{NetDialContext: e.browser.Transport.(*http.Transport).DialContext}
	conn, _, err := dialer.Dial("ws://app.localhost/socket", nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	msg := randomBytes(2 << 20)
	if err := conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck
	_, got, err := conn.ReadMessage()
	if err != nil || !bytes.Equal(got, msg) {
		t.Errorf("echo = %d bytes, %v; want the %d sent", len(got), err, len(msg))
	}
}
