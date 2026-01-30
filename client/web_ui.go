package client

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

//go:embed templates/dashboard.html templates/dashboard.css templates/dashboard.js
var templateFS embed.FS

type WebUI struct {
	client        *Client
	server        *http.Server
	dashboardHTML string
}

func NewWebUI(client *Client, port int) *WebUI {
	ui := &WebUI{client: client}
	ui.dashboardHTML = ui.buildDashboardHTML()

	mux := http.NewServeMux()
	mux.HandleFunc("/", ui.handleDashboard)
	mux.HandleFunc("/api/status", ui.handleStatus)
	mux.HandleFunc("/api/requests", ui.handleRequests)

	ui.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return ui
}

func (ui *WebUI) buildDashboardHTML() string {
	html, _ := templateFS.ReadFile("templates/dashboard.html")
	css, _ := templateFS.ReadFile("templates/dashboard.css")
	js, _ := templateFS.ReadFile("templates/dashboard.js")

	result := string(html)
	result = strings.Replace(result, "{{CSS}}", string(css), 1)
	result = strings.Replace(result, "{{JS}}", string(js), 1)

	return result
}

func (ui *WebUI) Start() error {
	return ui.server.ListenAndServe()
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
	logs := ui.client.history.GetRecent(50)
	if err := json.NewEncoder(w).Encode(logs); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func (ui *WebUI) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, ui.dashboardHTML)
}
