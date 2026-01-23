package client

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type WebUI struct {
	client *Client
	server *http.Server
}

func NewWebUI(client *Client, port int) *WebUI {
	ui := &WebUI{client: client}

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

func (ui *WebUI) Start() error {
	return ui.server.ListenAndServe()
}

func (ui *WebUI) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"tunnelURL": ui.client.tunnelURL,
		"domain":    ui.client.domain,
	})
}

func (ui *WebUI) handleRequests(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	logs := ui.client.history.GetRecent(50)
	json.NewEncoder(w).Encode(logs)
}

func (ui *WebUI) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, dashboardHTML)
}

const dashboardHTML = `<!DOCTYPE html>
  <html>
  <head>
      <title>Wormhole Dashboard</title>
      <style>
          * { box-sizing: border-box; }
          body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; padding: 40px 20px; background: #fff; color: #333; }
          .container { max-width: 800px; margin: 0 auto; }
          h1 { font-weight: 500; font-size: 24px; margin: 0 0 30px 0; }
          h2 { font-weight: 500; font-size: 16px; color: #666; margin: 30px 0 15px 0; }
          .tunnel-url { background: #f8f9fa; padding: 16px 20px; border-radius: 6px; font-family: 'SF Mono', Monaco, monospace; font-size: 14px; border: 1px solid #e9ecef; }
          .tunnel-url a { color: #0066cc; text-decoration: none; }
          .tunnel-url a:hover { text-decoration: underline; }
          table { width: 100%; border-collapse: collapse; font-size: 14px; }
          th { padding: 10px 12px; text-align: left; font-weight: 500; color: #666; border-bottom: 2px solid #e9ecef; }
          td { padding: 10px 12px; border-bottom: 1px solid #f1f3f4; }
          tr:hover { background: #f8f9fa; }
          .method { font-family: 'SF Mono', Monaco, monospace; font-weight: 500; }
          .url { font-family: 'SF Mono', Monaco, monospace; color: #555; }
          .time { color: #999; }
          .status { font-family: 'SF Mono', Monaco, monospace; font-weight: 500; }
          .status-2xx { color: #22863a; }
          .status-3xx { color: #0066cc; }
          .status-4xx { color: #b08800; }
          .status-5xx { color: #cb2431; }
          .empty { color: #999; text-align: center; padding: 40px; }
      </style>
  </head>
  <body>
      <div class="container">
          <h1>Wormhole</h1>
          <div class="tunnel-url">
              <a id="tunnelUrl" href="#" target="_blank"></a>
          </div>
          <h2>Requests</h2>
          <table>
              <thead><tr><th>Time</th><th>Method</th><th>Path</th><th>Status</th></tr></thead>
              <tbody id="requests"></tbody>
          </table>
      </div>
      <script>
          async function refresh() {
              const status = await fetch('/api/status').then(r => r.json());
              document.getElementById('tunnelUrl').href = status.tunnelURL;
              document.getElementById('tunnelUrl').textContent = status.tunnelURL;

              const requests = await fetch('/api/requests').then(r => r.json());
              const tbody = document.getElementById('requests');
              if (!requests || requests.length === 0) {
                  tbody.innerHTML = '<tr><td colspan="4" class="empty">No requests yet</td></tr>';
                  return;
              }
              tbody.innerHTML = requests.reverse().map(r => {
                  const sc = r.StatusCode;
                  const statusClass = sc < 300 ? 'status-2xx' : sc < 400 ? 'status-3xx' : sc < 500 ? 'status-4xx' : 'status-5xx';
                  const time = new Date(r.Timestamp).toLocaleTimeString();
                  return '<tr><td class="time">' + time + '</td><td class="method">' + r.Method + '</td><td class="url">' + r.URL + '</td><td class="status ' + statusClass + '">' + sc + '</td></tr>';
              }).join('');
          }
          refresh();
          setInterval(refresh, 1000);
			</script>
  </body>
  </html>`
