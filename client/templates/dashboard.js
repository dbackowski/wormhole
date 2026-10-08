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
let lastMessagesPayload = '';

function getStatusClass(statusCode) {
    if (statusCode < STATUS_BOUNDARIES.SUCCESS) return 'status-2xx';
    if (statusCode < STATUS_BOUNDARIES.REDIRECT) return 'status-3xx';
    if (statusCode < STATUS_BOUNDARIES.CLIENT_ERROR) return 'status-4xx';
    return 'status-5xx';
}

// Escapes quotes too: the result is also placed inside attribute values, where
// textContent/innerHTML serialization would leave a `"` from a request URL intact.
function escapeHtml(text) {
    return String(text).replace(/[&<>"']/g, c => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
    })[c]);
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

function formatBody(body, size) {
    if (!body || body.length === 0) {
        return '<span class="no-content">No body</span>';
    }
    const decoded = atob(body);
    let text;
    try {
        text = JSON.stringify(JSON.parse(decoded), null, 2);
    } catch {
        text = decoded;
    }
    let html = escapeHtml(text);
    if (size > decoded.length) {
        html += '\n\n<span class="no-content">Truncated: showing first ' +
            decoded.length.toLocaleString() + ' of ' + size.toLocaleString() + ' bytes</span>';
    }
    return html;
}

function formatBytes(n) {
    if (n < 1024) return n + ' B';
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
    return (n / (1024 * 1024)).toFixed(1) + ' MB';
}

function decodeUTF8(binary) {
    return new TextDecoder().decode(Uint8Array.from(binary, c => c.charCodeAt(0)));
}

function formatRows(rows) {
    return rows.map(([name, value]) =>
        '<div class="header-row">' +
        '<span class="header-name">' + escapeHtml(name) + ':</span>' +
        '<span class="header-value">' + escapeHtml(value) + '</span>' +
        '</div>'
    ).join('');
}

function formatStream(req) {
    const stream = req.Stream;
    const opened = new Date(req.Timestamp);
    const rows = [['Opened', opened.toLocaleTimeString()]];
    if (stream.Open) {
        rows.push(['State', 'open']);
    } else {
        const seconds = (new Date(stream.ClosedAt) - opened) / 1000;
        rows.push(['State', 'closed after ' + seconds.toFixed(1) + ' s']);
    }
    rows.push(
        ['Sent to app', formatBytes(stream.BytesToApp)],
        ['Received from app', formatBytes(stream.BytesFromApp)],
        ['Messages', String(stream.Messages)]
    );
    return formatRows(rows);
}

function formatPayload(msg) {
    const raw = msg.Payload ? atob(msg.Payload) : '';
    let text;
    if (msg.Type === 'close') {
        if (raw.length < 2) return '';
        text = String((raw.charCodeAt(0) << 8) | raw.charCodeAt(1));
        if (raw.length > 2) text += ' ' + decodeUTF8(raw.slice(2));
    } else if (msg.Type === 'binary') {
        text = Array.from(raw, c => c.charCodeAt(0).toString(16).padStart(2, '0')).join(' ');
    } else {
        text = decodeUTF8(raw);
        try {
            text = JSON.stringify(JSON.parse(text), null, 2);
        } catch {}
    }
    if (!text) return '';
    let html = escapeHtml(text);
    if (msg.Size > raw.length) {
        html += '\n\n<span class="no-content">Truncated: showing first ' +
            raw.length.toLocaleString() + ' of ' + msg.Size.toLocaleString() + ' bytes</span>';
    }
    return html;
}

// Arrows follow the browser's point of view, as in its developer tools: up
// is sent to the app, down is received from it.
function renderMessage(msg) {
    const payload = formatPayload(msg);
    return '<li class="message ' + (msg.FromApp ? 'from-app' : 'to-app') + '">' +
        '<div class="message-meta">' +
        '<span class="message-dir" title="' + (msg.FromApp ? 'Received from app' : 'Sent to app') + '">' +
        (msg.FromApp ? '&darr;' : '&uarr;') + '</span>' +
        '<span class="message-type">' + escapeHtml(msg.Type) + '</span>' +
        '<span class="message-size">' + formatBytes(msg.Size) + '</span>' +
        '<span class="time">' + new Date(msg.Timestamp).toLocaleTimeString() + '</span>' +
        '</div>' +
        (payload ? '<pre class="message-payload">' + payload + '</pre>' : '') +
        '</li>';
}

async function refreshMessages() {
    const req = requestsData.find(r => r.UUID === selectedId);
    if (!req || !req.Stream) return;

    try {
        const response = await fetch('/api/requests/' + encodeURIComponent(req.UUID) + '/messages');
        if (!response.ok) return;
        const payload = await response.text();
        if (req.UUID !== selectedId || payload === lastMessagesPayload) return;
        lastMessagesPayload = payload;

        const messages = JSON.parse(payload) || [];
        const list = document.getElementById('messages');
        if (messages.length === 0) {
            list.innerHTML = '<li class="no-content">No messages yet</li>';
            return;
        }
        let note = '';
        if (req.Stream.Messages > messages.length) {
            note = '<li class="no-content messages-note">Showing the last ' + messages.length +
                ' of ' + req.Stream.Messages + ' messages</li>';
        }
        list.innerHTML = note + messages.slice().reverse().map(renderMessage).join('');
    } catch (err) {
        console.error('Failed to load messages:', err);
    }
}

function matchesFilter(req, query) {
    const text = req.Method + ' ' + req.URL + ' ' + req.StatusCode + (req.Stream ? ' websocket' : '');
    return text.toLowerCase().includes(query);
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
    let wsBadge = '';
    if (req.Stream) {
        wsBadge = req.Stream.Open
            ? '<span class="ws-badge ws-open" title="WebSocket open">ws</span>'
            : '<span class="ws-badge ws-closed" title="WebSocket closed">ws</span>';
    }

    return '<li class="request-item' + selected + '" data-id="' + req.UUID + '"' +
        ' title="' + escapeHtml(req.URL) + '" onclick="selectRequest(\'' + req.UUID + '\')">' +
        '<div class="request-path">' + escapeHtml(req.URL) + '</div>' +
        '<div class="request-meta">' +
        '<span class="status ' + getStatusClass(req.StatusCode) + '">' + req.StatusCode + '</span>' +
        '<span class="method">' + escapeHtml(req.Method) + '</span>' +
        wsBadge +
        errorDot +
        '<span class="time">' + time + '</span>' +
        '</div>' +
        '</li>';
}

function selectRequest(id) {
    if (id !== selectedId) {
        lastMessagesPayload = '';
        document.getElementById('messages').innerHTML = '';
    }
    selectedId = id;
    document.querySelectorAll('.request-item').forEach(item => {
        item.classList.toggle('selected', item.dataset.id === id);
    });
    renderDetails();
    refreshMessages();
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

    const isStream = Boolean(req.Stream);
    document.getElementById('streamSection').classList.toggle('hidden', !isStream);
    document.getElementById('messagesSection').classList.toggle('hidden', !isStream);
    document.getElementById('requestBodySection').classList.toggle('hidden', isStream);
    document.getElementById('responseBodySection').classList.toggle('hidden', isStream);
    if (isStream) {
        document.getElementById('streamInfo').innerHTML = formatStream(req);
    }

    document.getElementById('requestHeaders').innerHTML = formatHeaders(req.RequestHeaders);
    document.getElementById('responseHeaders').innerHTML = formatHeaders(req.ResponseHeaders);
    document.getElementById('requestBody').innerHTML = formatBody(req.RequestBody, req.RequestBodySize);
    document.getElementById('responseBody').innerHTML = formatBody(req.ResponseBody, req.ResponseBodySize);

    document.getElementById('detailsPanel').classList.add('visible');
    document.getElementById('detailsEmpty').classList.add('hidden');
}

function closeDetails() {
    selectedId = null;
    lastDetailsPayload = '';
    lastMessagesPayload = '';
    document.getElementById('messages').innerHTML = '';
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
        await refreshMessages();
    } catch (err) {
        console.error('Failed to refresh:', err);
    }
}

refresh();
setInterval(refresh, REFRESH_INTERVAL_MS);
