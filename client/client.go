package client

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dbackowski/wormhole/common"

	"github.com/gorilla/websocket"
)

const MaxConcurrentRequests = 64

var (
	ErrDomainTaken  = errors.New("domain is already taken")
	ErrUnauthorized = errors.New("authentication failed: invalid or missing auth token")

	errReconnectCancelled = errors.New("reconnect cancelled")
)

type RequestLog struct {
	UUID            string
	Timestamp       time.Time
	Method          string
	URL             string
	StatusCode      int
	RequestHeaders  map[string][]string
	RequestBody     []byte // at most MaxStoredBodySize bytes, see RequestBodySize
	ResponseHeaders map[string][]string
	ResponseBody    []byte        // at most MaxStoredBodySize bytes, see ResponseBodySize
	Error           string        // reason the local forward failed, empty on success
	Duration        time.Duration // time the local app took to respond; for a WebSocket, its handshake

	RequestBodySize  int // full size before truncation
	ResponseBodySize int

	Stream   StreamInfo  `json:",omitzero"`
	Messages []WSMessage `json:"-"` // served on their own, see RequestHistory.GetMessages
}

// StreamInfo describes a WebSocket; it is zero for plain HTTP requests.
type StreamInfo struct {
	Open         bool
	ClosedAt     time.Time `json:",omitzero"`
	BytesToApp   int64
	BytesFromApp int64
	Messages     int // total seen, including ones no longer stored
}

type WSMessage struct {
	Timestamp time.Time
	FromApp   bool   // sent by the local app; otherwise sent to it
	Type      string // text, binary, close, ping or pong
	Size      int64  // full payload size before truncation
	Payload   []byte // at most MaxStoredMessageSize bytes
}

type Client struct {
	domain     string
	tunnelURL  string
	localURL   string
	Conn       *websocket.Conn
	writeMu    sync.Mutex
	displayMu  sync.Mutex
	statusMu   sync.Mutex
	requestSem chan struct{}
	streams    *common.Streams
	proxy      *LocalProxy
	history    *RequestHistory
	display    Display
	handlers   common.Handlers
	WebUIPort  int

	wsURL       string
	dialHeaders http.Header
	// Guarded by its own lock rather than displayMu, so that reconnecting can
	// report progress while a frame is stuck on a terminal that stopped reading.
	status      string        // guarded by statusMu
	redraw      chan struct{} // see requestRedraw
	rateSamples []rateSample  // guarded by displayMu
}

type rateSample struct {
	at    time.Time
	total int
}

const (
	reconnectInitialDelay = 500 * time.Millisecond
	reconnectMaxDelay     = 30 * time.Second
	reconnectMaxElapsed   = 5 * time.Minute
)

func NewClient(cfg *Config) (*Client, error) {
	localURL, err := validateClientConfig(cfg.Domain, cfg.Local, cfg.WebUIPort)
	if err != nil {
		return nil, fmt.Errorf("invalid client config: %w", err)
	}

	server, err := parseURL(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	// http becomes ws, https becomes wss.
	wsScheme := strings.Replace(server.Scheme, "http", "ws", 1)
	wsURL := common.BuildSubdomainURL(wsScheme, cfg.Domain, server.Host)

	dialHeaders := http.Header{}
	dialHeaders.Set(common.ClientHeader, "1")
	dialHeaders.Set(common.SessionHeader, rand.Text())
	if cfg.AuthToken != "" {
		dialHeaders.Set("Authorization", "Bearer "+cfg.AuthToken)
	}

	conn, err := dialAndRegister(wsURL, dialHeaders)
	if err != nil {
		return nil, err
	}

	tunnelURL := common.BuildSubdomainURL(server.Scheme, cfg.Domain, server.Host)

	client := &Client{
		domain:    cfg.Domain,
		tunnelURL: tunnelURL,
		localURL:  localURL.String(),
		Conn:      conn,
		proxy:     NewLocalProxy(localURL, tunnelURL, common.RequestTimeout),
		history:   NewRequestHistory(common.ClientRequestHistorySize),
		display:   TerminalDisplay{},
		WebUIPort: cfg.WebUIPort,
		redraw:    make(chan struct{}, 1),

		wsURL:       wsURL,
		dialHeaders: dialHeaders,
	}

	client.setupMessageHandlers()

	return client, nil
}

func dialAndRegister(wsURL string, headers http.Header) (*websocket.Conn, error) {
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return nil, ErrUnauthorized
		}
		// The server names the problem, e.g. a host outside its -host domain.
		if resp != nil && resp.StatusCode == http.StatusBadRequest {
			reason, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("server rejected %s: %s", wsURL, strings.TrimSpace(string(reason)))
		}
		return nil, fmt.Errorf("failed to connect to %s: %w", wsURL, err)
	}

	if err := awaitRegistration(conn); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

func (c *Client) Reconnect() error {
	conn, err := dialAndRegister(c.wsURL, c.dialHeaders)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	old := c.Conn
	c.Conn = conn
	c.writeMu.Unlock()

	old.Close()

	return nil
}

// ReconnectWithBackoff returns nil once reconnected. Otherwise it returns the
// last dial error, which is shown in the status line between attempts. An auth
// failure is returned at once since retrying cannot fix it; a taken domain is
// retried, as the server holds a dropped connection until its heartbeat expires.
func (c *Client) ReconnectWithBackoff(cancel <-chan struct{}) error {
	deadline := time.Now().Add(reconnectMaxElapsed)
	delay := reconnectInitialDelay
	var lastErr error

	for attempt := 1; ; attempt++ {
		status := fmt.Sprintf("reconnecting... (attempt %d)", attempt)
		if lastErr != nil {
			status += ": " + lastErr.Error()
		}
		c.setStatus(status)

		select {
		case <-cancel:
			return errReconnectCancelled
		case <-time.After(delay):
		}

		lastErr = c.Reconnect()
		if lastErr == nil {
			c.setStatus("")
			return nil
		}

		if errors.Is(lastErr, ErrUnauthorized) || time.Now().After(deadline) {
			return lastErr
		}

		delay = min(delay*2, reconnectMaxDelay)
	}
}

func (c *Client) HandleConnection() {
	// Streams belong to the connection that carried them: the server has
	// already dropped its side of each one when that connection ends.
	defer c.streams.AbortAll()

	// Errors are dropped: anything written to the terminal is wiped by the next
	// refresh of the alt screen. Errors worth seeing go to the status line.
	common.RunMessageLoop(c.Conn, c.handlers, common.DefaultHeartbeat(),
		func(error) {}, func(*common.Message, error) {})
}

// Shutdown holds writeMu throughout, since Reconnect may swap Conn from the
// reconnect goroutine at the same time.
func (c *Client) Shutdown() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// Best effort: the connection may already be gone.
	c.Conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")) //nolint:errcheck
	return c.Conn.Close()
}

func (c *Client) safeWriteJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return common.WriteJSON(c.Conn, v)
}

func resolveProxyResponse(proxyResp *ProxyResponse, proxyErr error) ProxyResponse {
	if proxyErr != nil {
		if errors.Is(proxyErr, ErrResponseTooLarge) {
			return ProxyResponse{
				StatusCode: http.StatusBadGateway,
				Body:       []byte("response body too large"),
			}
		}
		return ProxyResponse{
			StatusCode: http.StatusBadGateway,
			Body:       []byte(http.StatusText(http.StatusBadGateway)),
		}
	}
	return *proxyResp
}

func (c *Client) sendResponse(msg *common.Message, resolved ProxyResponse) error {
	responseMsg := common.Message{
		Type:    common.MessageTypeHTTPResponse,
		UUID:    msg.UUID,
		Method:  msg.Method,
		URL:     msg.URL,
		Status:  resolved.StatusCode,
		Body:    resolved.Body,
		Headers: resolved.Headers,
	}

	if err := c.safeWriteJSON(responseMsg); err != nil {
		return fmt.Errorf("failed to send response: %w", err)
	}

	return nil
}

func (c *Client) ClearHistory() {
	c.history.Clear()
	c.requestRedraw()
}

func (c *Client) setStatus(status string) {
	c.statusMu.Lock()
	c.status = status
	c.statusMu.Unlock()

	c.requestRedraw()
}

// Redraws are at most this frequent, so that heavy traffic neither floods the
// terminal nor makes request handling wait on it.
const redrawInterval = 100 * time.Millisecond

// requestRedraw asks RunDisplay for a redraw without waiting for it. Requests
// made while one is pending fold into it.
func (c *Client) requestRedraw() {
	select {
	case c.redraw <- struct{}{}:
	default:
	}
}

// RunDisplay redraws the terminal when asked, when it is resized, and every
// second so that the request rate falls once traffic stops.
func (c *Client) RunDisplay() {
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	tick := time.Tick(time.Second)
	for {
		select {
		case <-c.redraw:
		case <-resize:
		case <-tick:
		}
		c.RefreshTerminalOutput()
		time.Sleep(redrawInterval)
	}
}

func (c *Client) RefreshTerminalOutput() {
	c.displayMu.Lock()
	defer c.displayMu.Unlock()
	c.statusMu.Lock()
	status := c.status
	c.statusMu.Unlock()
	total := c.history.Total()
	// The display picks how many of these fit the terminal.
	c.display.Render(Frame{
		TunnelURL: c.tunnelURL,
		LocalURL:  c.localURL,
		WebUIPort: c.WebUIPort,
		Status:    status,
		Logs:      c.history.GetRecent(common.ClientRequestHistorySize),
		Total:     total,
		Rate:      c.requestRate(time.Now(), total),
	})
}

const rateWindow = 10 * time.Second

// requestRate is requests per second over about the last rateWindow, from the
// total sampled at each redraw; until the client has run that long, it is the
// rate since start. Exact however busy the tunnel, unlike counting the
// history, which keeps only the latest requests. Callers hold displayMu.
func (c *Client) requestRate(now time.Time, total int) float64 {
	c.rateSamples = append(c.rateSamples, rateSample{now, total})
	// The baseline is the newest sample at least rateWindow old.
	for len(c.rateSamples) > 1 && now.Sub(c.rateSamples[1].at) >= rateWindow {
		c.rateSamples = c.rateSamples[1:]
	}
	base := c.rateSamples[0]
	if span := now.Sub(base.at); span > 0 {
		return float64(total-base.total) / span.Seconds()
	}
	return 0
}

func awaitRegistration(conn *websocket.Conn) error {
	if err := conn.SetReadDeadline(time.Now().Add(common.WriteWait)); err != nil {
		return err
	}

	var msg common.Message
	if err := conn.ReadJSON(&msg); err != nil {
		return fmt.Errorf("waiting for domain registration: %w", err)
	}

	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}

	switch msg.Type {
	case common.MessageTypeDomainRegistered:
		return nil
	case common.MessageTypeDomainTaken:
		return ErrDomainTaken
	default:
		return fmt.Errorf("unexpected message during registration: %s", msg.Type)
	}
}
