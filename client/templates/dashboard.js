const REFRESH_INTERVAL_MS = 1000;
const STATUS_BOUNDARIES = {
    SUCCESS: 300,
    REDIRECT: 400,
    CLIENT_ERROR: 500
};

let requestsData = [];
let selectedId = null;

function getStatusClass(statusCode) {
    if (statusCode < STATUS_BOUNDARIES.SUCCESS) return 'status-2xx';
    if (statusCode < STATUS_BOUNDARIES.REDIRECT) return 'status-3xx';
    if (statusCode < STATUS_BOUNDARIES.CLIENT_ERROR) return 'status-4xx';
    return 'status-5xx';
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function formatHeaders(headers) {
    if (!headers || Object.keys(headers).length === 0) {
        return '<span class="no-content">No headers</span>';
    }
    return Object.entries(headers).map(([name, values]) =>
        '<div class="header-row">' +
        '<span class="header-name">' + escapeHtml(name) + ':</span>' +
        '<span class="header-value">' + escapeHtml(values.join(', ')) + '</span>' +
        '</div>'
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

function showDetails(id) {
    const req = requestsData.find(r => r.UUID === id);
    if (!req) return;

    selectedId = id;
    document.querySelectorAll('tbody tr').forEach(tr => {
        tr.classList.toggle('selected', tr.dataset.id === id);
    });

    document.getElementById('detailsTitle').textContent = req.Method + ' ' + req.URL;

    const errorSection = document.getElementById('errorSection');
    if (req.Error) {
        document.getElementById('errorReason').textContent = req.Error;
        errorSection.style.display = '';
    } else {
        errorSection.style.display = 'none';
    }

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

function renderRequestRow(req) {
    const statusClass = getStatusClass(req.StatusCode);
    const time = new Date(req.Timestamp).toLocaleTimeString();
    const selected = req.UUID === selectedId ? ' selected' : '';

    return '<tr data-id="' + req.UUID + '" class="' + selected + '" onclick="showDetails(\'' + req.UUID + '\')">' +
        '<td class="time">' + time + '</td>' +
        '<td class="method">' + req.Method + '</td>' +
        '<td class="url">' + escapeHtml(req.URL) + '</td>' +
        '<td class="status ' + statusClass + '">' + req.StatusCode + '</td>' +
        '</tr>';
}

async function refresh() {
    try {
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

        tbody.innerHTML = requests.slice().reverse().map(renderRequestRow).join('');

        if (selectedId) {
            const stillExists = requestsData.find(r => r.UUID === selectedId);
            if (stillExists) {
                showDetails(selectedId);
            } else {
                closeDetails();
            }
        }
    } catch (err) {
        console.error('Failed to refresh:', err);
    }
}

refresh();
setInterval(refresh, REFRESH_INTERVAL_MS);
