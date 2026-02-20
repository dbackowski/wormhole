package client

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
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

	ui.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return ui, nil
}

func (ui *WebUI) buildDashboardHTML() (string, error) {
	html, err := templateFS.ReadFile("templates/dashboard.html")
	if err != nil {
		return "", fmt.Errorf("failed to read dashboard.html: %w", err)
	}

	css, err := templateFS.ReadFile("templates/dashboard.css")
	if err != nil {
		return "", fmt.Errorf("failed to read dashboard.css: %w", err)
	}

	js, err := templateFS.ReadFile("templates/dashboard.js")
	if err != nil {
		return "", fmt.Errorf("failed to read dashboard.js: %w", err)
	}

	result := string(html)
	result = strings.Replace(result, "{{CSS}}", string(css), 1)
	result = strings.Replace(result, "{{JS}}", string(js), 1)

	return result, nil
}

func (ui *WebUI) Start() error {
	return ui.server.ListenAndServe()
}

func (ui *WebUI) Shutdown(ctx context.Context) error {
	return ui.server.Shutdown(ctx)
}

func (ui *WebUI) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"tunnelURL": ui.client.tunnelURL,
		"domain":    ui.client.domain,
	}); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func (ui *WebUI) handleRequests(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	logs := ui.client.history.GetRecent(common.ClientRequestHistorySize)
	if err := json.NewEncoder(w).Encode(logs); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func (ui *WebUI) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, ui.dashboardHTML)
}
