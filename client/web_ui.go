package client

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"

	"github.com/dbackowski/wormhole/common"
)

//go:embed templates
var templateFS embed.FS

type WebUI struct {
	client *Client
	server *http.Server
}

func NewWebUI(client *Client, port int) *WebUI {
	ui := &WebUI{client: client}

	static, _ := fs.Sub(templateFS, "templates") // fails only on an invalid path

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServerFS(static))
	mux.HandleFunc("/api/status", ui.handleStatus)
	mux.HandleFunc("/api/requests", ui.handleRequests)
	mux.HandleFunc("GET /api/requests/{id}", ui.handleRequest)
	mux.HandleFunc("GET /api/requests/{id}/messages", ui.handleMessages)

	ui.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: localHostOnly(mux),
	}

	return ui
}

// localHostOnly rejects requests whose Host is not the loopback name the UI is
// served on. Binding to 127.0.0.1 alone does not stop DNS rebinding: a page on
// an attacker's domain re-resolved to 127.0.0.1 reaches the UI same-origin, but
// its requests still carry the attacker's hostname.
func localHostOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "localhost" && host != "127.0.0.1" {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Start binds the port before returning, so a port already in use is reported
// to the caller instead of lost in a background goroutine. Serving continues in
// the background until Shutdown.
func (ui *WebUI) Start() error {
	ln, err := net.Listen("tcp", ui.server.Addr)
	if err != nil {
		return err
	}
	go ui.server.Serve(ln)
	return nil
}

func (ui *WebUI) Shutdown(ctx context.Context) error {
	return ui.server.Shutdown(ctx)
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func (ui *WebUI) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"tunnelURL":     ui.client.tunnelURL,
		"domain":        ui.client.domain,
		"totalRequests": ui.client.history.Total(),
	})
}

func (ui *WebUI) handleRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		ui.client.ClearHistory()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Lets a paused dashboard count what arrived after this list. Read apart
	// from the list, so a request landing in between can be counted off by one.
	w.Header().Set("X-Total-Requests", strconv.Itoa(ui.client.history.Total()))
	logs := ui.client.history.GetRecent(common.ClientRequestHistorySize)
	// Polled every second, so the list leaves out headers and bodies; the
	// dashboard fetches them from handleRequest for the request it shows.
	for i := range logs {
		logs[i].RequestHeaders, logs[i].ResponseHeaders = nil, nil
		logs[i].RequestBody, logs[i].ResponseBody = nil, nil
	}
	writeJSON(w, logs)
}

func (ui *WebUI) handleRequest(w http.ResponseWriter, r *http.Request) {
	log, ok := ui.client.history.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, log)
}

func (ui *WebUI) handleMessages(w http.ResponseWriter, r *http.Request) {
	messages, ok := ui.client.history.GetMessages(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, messages)
}
