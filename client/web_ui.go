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

	fmt.Printf(":%d", port)
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
        .container { max-width: 900px; margin: 0 auto; }
        h1 { font-weight: 500; font-size: 24px; margin: 0 0 30px 0; }
        h2 { font-weight: 500; font-size: 16px; color: #666; margin: 30px 0 15px 0; }
        .tunnel-url { background: #f8f9fa; padding: 16px 20px; border-radius: 6px; font-family: 'SF Mono', Monaco, monospace; font-size: 14px; border: 1px solid #e9ecef; }
        .tunnel-url a { color: #0066cc; text-decoration: none; }
        .tunnel-url a:hover { text-decoration: underline; }
        table { width: 100%; border-collapse: collapse; font-size: 14px; }
        th { padding: 10px 12px; text-align: left; font-weight: 500; color: #666; border-bottom: 2px solid #e9ecef; }
        td { padding: 10px 12px; border-bottom: 1px solid #f1f3f4; }
        tbody tr { cursor: pointer; }
        tbody tr:hover { background: #f8f9fa; }
        tbody tr.selected { background: #e7f3ff; }
        .method { font-family: 'SF Mono', Monaco, monospace; font-weight: 500; }
        .url { font-family: 'SF Mono', Monaco, monospace; color: #555; max-width: 400px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
        .time { color: #999; }
        .status { font-family: 'SF Mono', Monaco, monospace; font-weight: 500; }
        .status-2xx { color: #22863a; }
        .status-3xx { color: #0066cc; }
        .status-4xx { color: #b08800; }
        .status-5xx { color: #cb2431; }
        .empty { color: #999; text-align: center; padding: 40px; }
        .details-panel { display: none; margin-top: 20px; border: 1px solid #e9ecef; border-radius: 6px; background: #f8f9fa; }
        .details-panel.visible { display: block; }
        .details-header { display: flex; justify-content: space-between; align-items: center; padding: 12px 16px; border-bottom: 1px solid #e9ecef; background: #fff; border-radius: 6px 6px 0 0; }
        .details-header h3 { margin: 0; font-size: 14px; font-weight: 500; }
        .details-close { background: none; border: none; font-size: 20px; cursor: pointer; color: #666; padding: 0 4px; }
        .details-close:hover { color: #333; }
        .details-content { display: grid; grid-template-columns: 1fr 1fr; gap: 1px; background: #e9ecef; }
        .details-section { background: #fff; padding: 16px; }
        .details-section h4 { margin: 0 0 12px 0; font-size: 13px; font-weight: 600; color: #333; text-transform: uppercase; letter-spacing: 0.5px; }
        .details-section pre { margin: 0; font-family: 'SF Mono', Monaco, monospace; font-size: 12px; white-space: pre-wrap; word-break: break-all; background: #f8f9fa; padding: 12px; border-radius: 4px; max-height: 300px; overflow: auto; }
        .details-section .headers { font-size: 12px; }
        .details-section .header-row { display: flex; padding: 4px 0; border-bottom: 1px solid #f1f3f4; }
        .details-section .header-name { font-weight: 500; color: #0066cc; min-width: 150px; flex-shrink: 0; }
        .details-section .header-value { color: #555; word-break: break-all; }
        .details-section .no-content { color: #999; font-style: italic; }
        .body-section { grid-column: 1 / -1; }
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
        <div id="detailsPanel" class="details-panel">
            <div class="details-header">
                <h3 id="detailsTitle">Request Details</h3>
                <button class="details-close" onclick="closeDetails()">&times;</button>
            </div>
            <div class="details-content">
                <div class="details-section">
                    <h4>Request Headers</h4>
                    <div id="requestHeaders" class="headers"></div>
                </div>
                <div class="details-section">
                    <h4>Response Headers</h4>
                    <div id="responseHeaders" class="headers"></div>
                </div>
                <div class="details-section body-section">
                    <h4>Request Body</h4>
                    <pre id="requestBody"></pre>
                </div>
                <div class="details-section body-section">
                    <h4>Response Body</h4>
                    <pre id="responseBody"></pre>
                </div>
            </div>
        </div>
    </div>
    <script>
        let requestsData = [];
        let selectedId = null;

        function formatHeaders(headers) {
            if (!headers || Object.keys(headers).length === 0) {
                return '<span class="no-content">No headers</span>';
            }
            return Object.entries(headers).map(([name, values]) =>
                '<div class="header-row"><span class="header-name">' + escapeHtml(name) + ':</span><span class="header-value">' + escapeHtml(values.join(', ')) + '</span></div>'
            ).join('');
        }

        function formatBody(body) {
            if (!body || body.length === 0) {
                return '<span class="no-content">No body</span>';
            }
            const decoded = atob(body);
            try {
                const json = JSON.parse(decoded);
                return escapeHtml(JSON.stringify(json, null, 2));
            } catch {
                return escapeHtml(decoded);
            }
        }

        function escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }

        function showDetails(id) {
            const req = requestsData.find(r => r.UUID === id);
            if (!req) return;

            selectedId = id;
            document.querySelectorAll('tbody tr').forEach(tr => {
                tr.classList.toggle('selected', tr.dataset.id === id);
            });

            document.getElementById('detailsTitle').textContent = req.Method + ' ' + req.URL;
            document.getElementById('requestHeaders').innerHTML = formatHeaders(req.RequestHeaders);
            document.getElementById('responseHeaders').innerHTML = formatHeaders(req.ResponseHeaders);
            document.getElementById('requestBody').innerHTML = formatBody(req.RequestBody);
            document.getElementById('responseBody').innerHTML = formatBody(req.ResponseBody);
            document.getElementById('detailsPanel').classList.add('visible');
        }

        function closeDetails() {
            selectedId = null;
            document.querySelectorAll('tbody tr').forEach(tr => tr.classList.remove('selected'));
            document.getElementById('detailsPanel').classList.remove('visible');
        }

        async function refresh() {
            const status = await fetch('/api/status').then(r => r.json());
            document.getElementById('tunnelUrl').href = status.tunnelURL;
            document.getElementById('tunnelUrl').textContent = status.tunnelURL;

            const requests = await fetch('/api/requests').then(r => r.json());
            requestsData = requests || [];
            const tbody = document.getElementById('requests');
            if (!requests || requests.length === 0) {
                tbody.innerHTML = '<tr><td colspan="4" class="empty">No requests yet</td></tr>';
                return;
            }
            tbody.innerHTML = requests.slice().reverse().map(r => {
                const sc = r.StatusCode;
                const statusClass = sc < 300 ? 'status-2xx' : sc < 400 ? 'status-3xx' : sc < 500 ? 'status-4xx' : 'status-5xx';
                const time = new Date(r.Timestamp).toLocaleTimeString();
                const selected = r.UUID === selectedId ? ' selected' : '';
                return '<tr data-id="' + r.UUID + '" class="' + selected + '" onclick="showDetails(\'' + r.UUID + '\')"><td class="time">' + time + '</td><td class="method">' + r.Method + '</td><td class="url">' + escapeHtml(r.URL) + '</td><td class="status ' + statusClass + '">' + sc + '</td></tr>';
            }).join('');

            if (selectedId) {
                const stillExists = requestsData.find(r => r.UUID === selectedId);
                if (stillExists) {
                    showDetails(selectedId);
                } else {
                    closeDetails();
                }
            }
        }
        refresh();
        setInterval(refresh, 1000);
    </script>
</body>
</html>`
