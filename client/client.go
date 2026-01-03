package client

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/dbackowski/wormhole/common"

	"github.com/gorilla/websocket"
)

type RequestLog struct {
	Timestamp  time.Time
	Method     string
	URL        string
	StatusCode int
}

type Client struct {
	Domain      string
	Local       string
	Tunnel      string
	Conn        *websocket.Conn
	HTTPClient  *http.Client
	dispatcher  *common.MessageDispatcher
	requestLogs []RequestLog
	logsMutex   sync.RWMutex
	Logger      *common.Logger
}

func createHTTPClient() *http.Client {
	return &http.Client{
		Timeout: common.RequestTimeout,
		Transport: &http.Transport{
			// Disable connection pooling to prevent stale connections
			// when tunnel connections are frequently created/destroyed
			DisableKeepAlives: true,
		},
		// Don't follow redirects automatically - we need to pass them
		// back to the original client so they see the redirect
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func buildTunnelURL(serverConfig *ServerConfig, domain string) string {
	return fmt.Sprintf("%s://%s.%s", serverConfig.HTTPScheme, domain, serverConfig.Host)
}

func establishConnection(serverConfig *ServerConfig, domain string) (*websocket.Conn, error) {
	wsURL := buildWebSocketURL(serverConfig, domain)
	conn, err := ConnectToServer(wsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", wsURL, err)
	}
	return conn, nil
}

func NewClient(cfg *Config) (*Client, error) {
	logCfg := common.LoggerConfig{
		Level:  common.LevelError,
		Format: "text",
		Output: os.Stderr,
	}

	if err := validateClientConfig(cfg.Domain, cfg.Local); err != nil {
		return nil, fmt.Errorf("invalid client config: %w", err)
	}

	serverConfig, err := validateAndParseServerURL(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	conn, err := establishConnection(serverConfig, cfg.Domain)
	if err != nil {
		return nil, err
	}

	client := &Client{
		Domain:      cfg.Domain,
		Local:       cfg.Local,
		Conn:        conn,
		Tunnel:      buildTunnelURL(serverConfig, cfg.Domain),
		HTTPClient:  createHTTPClient(),
		requestLogs: make([]RequestLog, 0),
		Logger:      common.NewLogger(logCfg),
	}

	client.setupMessageHandlers()

	return client, nil
}

func (c *Client) HandleConnection() {
	var message common.Message

	for {
		err := c.Conn.ReadJSON(&message)
		if err != nil {
			c.Logger.Error("Read error", "error", err)
			return
		}

		err = c.dispatcher.Dispatch(&message)
		if err != nil {
			c.Logger.Error("Failed to dispatch message", "error", err)
		}
	}
}

func closeWebsocket(c *websocket.Conn) error {
	return c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

func (c *Client) printSummary() {
	c.logsMutex.RLock()
	defer c.logsMutex.RUnlock()

	common.ClearTerminal()
	fmt.Println("Connected to server.")
	fmt.Printf("Your tunnel is available at: %s\n", c.Tunnel)
	fmt.Printf("Waiting for incoming HTTP requests...\n\n")
	fmt.Printf("------------------- last %d requests -------------------\n\n", common.ClientRequestHistorySize)

	start := 0

	if len(c.requestLogs) > common.ClientRequestHistorySize {
		start = len(c.requestLogs) - common.ClientRequestHistorySize
	}

	for _, rl := range c.requestLogs[start:] {
		fmt.Printf("%s %s %s -> %d\n", common.FormatTime(rl.Timestamp), rl.Method, rl.URL, rl.StatusCode)
	}
}

func (c *Client) logResponse(message common.Message, localURL string, statusCode int) {
	c.logsMutex.Lock()
	defer c.logsMutex.Unlock()

	c.requestLogs = append(c.requestLogs, RequestLog{
		Timestamp:  time.Now(),
		Method:     message.Method,
		URL:        localURL,
		StatusCode: statusCode,
	})
}

func (c *Client) buildLocalURL(requestPath string) (string, error) {
	baseURL, err := url.Parse(c.Local)
	if err != nil {
		return "", fmt.Errorf("failed to parse local URL: %w", err)
	}

	baseURL.Path = path.Join(baseURL.Path, requestPath)
	return baseURL.String(), nil
}

func buildWebSocketURL(s *ServerConfig, domain string) string {
	return fmt.Sprintf("%s://%s.%s/ws", s.WSScheme, domain, s.Host)
}

func ConnectToServer(websocketURL string) (*websocket.Conn, error) {
	conn, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)

	return conn, err
}
