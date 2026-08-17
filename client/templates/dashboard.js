const REFRESH_INTERVAL_MS = 1000;
const STATUS_BOUNDARIES = {
    SUCCESS: 300,
    REDIRECT: 400,
    CLIENT_ERROR: 500
};

let requestsData = [];
let selectedId = null;
let lastRequestsPayload = '';
let lastDetailsPayload = '';

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

function matchesFilter(req, query) {
    return (req.Method + ' ' + req.URL + ' ' + req.StatusCode).toLowerCase().includes(query);
}

function renderList() {
    const query = document.getElementById('filter').value.trim().toLowerCase();
    const matches = requestsData.filter(req => !query || matchesFilter(req, query)).reverse();
    const list = document.getElementById('requests');

    if (matches.length === 0) {
        const message = requestsData.length === 0 ? 'No requests yet' : 'No matching requests';
        list.innerHTML = '<li class="empty">' + message + '</li>';
        return;
    }

    list.innerHTML = matches.map(renderRequestItem).join('');
}

function renderRequestItem(req) {
    const time = new Date(req.Timestamp).toLocaleTimeString();
    const selected = req.UUID === selectedId ? ' selected' : '';
    const errorDot = req.Error ? '<span class="error-dot" title="Forwarding failed">&bull;</span>' : '';

    return '<li class="request-item' + selected + '" data-id="' + req.UUID + '"' +
        ' title="' + escapeHtml(req.URL) + '" onclick="selectRequest(\'' + req.UUID + '\')">' +
        '<div class="request-path">' + escapeHtml(req.URL) + '</div>' +
        '<div class="request-meta">' +
        '<span class="status ' + getStatusClass(req.StatusCode) + '">' + req.StatusCode + '</span>' +
        '<span class="method">' + escapeHtml(req.Method) + '</span>' +
        errorDot +
        '<span class="time">' + time + '</span>' +
        '</div>' +
        '</li>';
}

function selectRequest(id) {
    selectedId = id;
    document.querySelectorAll('.request-item').forEach(item => {
        item.classList.toggle('selected', item.dataset.id === id);
    });
    renderDetails();
}

// Re-renders only when the selected request actually changed, so polling does not
// wipe text selection or reset scroll inside the details pane.
function renderDetails() {
    const req = requestsData.find(r => r.UUID === selectedId);
    if (!req) {
        closeDetails();
        return;
    }

    const payload = JSON.stringify(req);
    if (payload === lastDetailsPayload) return;
    lastDetailsPayload = payload;

    document.getElementById('detailsTitle').textContent = req.Method + ' ' + req.URL;

    const errorSection = document.getElementById('errorSection');
    if (req.Error) {
        document.getElementById('errorReason').textContent = req.Error;
        errorSection.classList.remove('hidden');
    } else {
        errorSection.classList.add('hidden');
    }

    document.getElementById('requestHeaders').innerHTML = formatHeaders(req.RequestHeaders);
    document.getElementById('responseHeaders').innerHTML = formatHeaders(req.ResponseHeaders);
    document.getElementById('requestBody').innerHTML = formatBody(req.RequestBody);
    document.getElementById('responseBody').innerHTML = formatBody(req.ResponseBody);

    document.getElementById('detailsPanel').classList.add('visible');
    document.getElementById('detailsEmpty').classList.add('hidden');
}

function closeDetails() {
    selectedId = null;
    lastDetailsPayload = '';
    document.querySelectorAll('.request-item').forEach(item => item.classList.remove('selected'));
    document.getElementById('detailsPanel').classList.remove('visible');
    document.getElementById('detailsEmpty').classList.remove('hidden');
}

async function clearRequests() {
    try {
        await fetch('/api/requests', { method: 'DELETE' });
        closeDetails();
        refresh();
    } catch (err) {
        console.error('Failed to clear:', err);
    }
}

async function refresh() {
    try {
        const status = await fetch('/api/status').then(r => r.json());
        document.getElementById('tunnelUrl').href = status.tunnelURL;
        document.getElementById('tunnelUrl').textContent = status.tunnelURL;

        const payload = await fetch('/api/requests').then(r => r.text());
        if (payload === lastRequestsPayload) return;
        lastRequestsPayload = payload;

        requestsData = JSON.parse(payload) || [];
        renderList();
        renderDetails();
    } catch (err) {
        console.error('Failed to refresh:', err);
    }
}

refresh();
setInterval(refresh, REFRESH_INTERVAL_MS);
