package client

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/dbackowski/wormhole/common"
)

//go:embed templates/dashboard.html templates/dashboard.css templates/dashboard.js
var templateFS embed.FS

type WebUI struct {
	client        *Client
	server        *http.Server
	dashboardHTML string
}

func NewWebUI(client *Client, port int) (*WebUI, error) {
	ui := &WebUI{client: client}

	dashboardHTML, err := ui.buildDashboardHTML()
	if err != nil {
		return nil, fmt.Errorf("failed to build dashboard: %w", err)
	}
	ui.dashboardHTML = dashboardHTML

	mux := http.NewServeMux()
	mux.HandleFunc("/", ui.handleDashboard)
	mux.HandleFunc("/api/status", ui.handleStatus)
	mux.HandleFunc("/api/requests", ui.handleRequests)
	mux.HandleFunc("GET /api/requests/{id}/messages", ui.handleMessages)

	ui.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: localHostOnly(mux),
	}

	return ui, nil
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

func readTemplate(name string) (string, error) {
	data, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", name, err)
	}
	return string(data), nil
}

func (ui *WebUI) buildDashboardHTML() (string, error) {
	html, err := readTemplate("dashboard.html")
	if err != nil {
		return "", err
	}

	css, err := readTemplate("dashboard.css")
	if err != nil {
		return "", err
	}

	js, err := readTemplate("dashboard.js")
	if err != nil {
		return "", err
	}

	result := strings.Replace(html, "{{CSS}}", css, 1)
	result = strings.Replace(result, "{{JS}}", js, 1)

	return result, nil
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
	writeJSON(w, map[string]string{
		"tunnelURL": ui.client.tunnelURL,
		"domain":    ui.client.domain,
	})
}

func (ui *WebUI) handleRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		ui.client.ClearHistory()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, ui.client.history.GetRecent(common.ClientRequestHistorySize))
}

func (ui *WebUI) handleMessages(w http.ResponseWriter, r *http.Request) {
	messages, ok := ui.client.history.GetMessages(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, messages)
}

func (ui *WebUI) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, ui.dashboardHTML)
}
