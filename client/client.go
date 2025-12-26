package client

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
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
	logsMutex   sync.Mutex
}

func NewClient(cfg *Config) (*Client, error) {
	if err := validateClientConfig(cfg.Domain, cfg.Local); err != nil {
		return nil, err
	}

	serverConfig, err := validateAndParseServerURL(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	conn, err := ConnectToServer(buildWebSocketURL(serverConfig, cfg.Domain))
	if err != nil {
		return nil, err
	}

	client := &Client{
		Domain: cfg.Domain,
		Conn:   conn,
		Local:  cfg.Local,
		Tunnel: fmt.Sprintf("%s://%s.%s", serverConfig.HTTPScheme, cfg.Domain, serverConfig.Host),
		HTTPClient: &http.Client{
			Timeout:       common.RequestTimeout,
			Transport:     &http.Transport{DisableKeepAlives: true},                                              // Disable connection reuse
			CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }, // Don't follow redirects, instead pass them back to the browser
		},
		requestLogs: make([]RequestLog, 0),
	}
	client.setupMessageHandlers()

	return client, nil
}

func (c *Client) HandleConnection() {
	var message common.Message

	for {
		err := c.Conn.ReadJSON(&message)
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}

		err = c.dispatcher.Dispatch(&message)
		if err != nil {
			log.Printf("Failed to dispatch message: %v", err)
		}
	}
}

func closeWebsocket(c *websocket.Conn) {
	err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Println("write close:", err)
		return
	}
}

func (c *Client) printSummary() {
	common.ClearTerminal()
	fmt.Println("Connected to server.")
	fmt.Printf("Your tunnel is available at: %s\n", c.Tunnel)
	fmt.Println("Waiting for incoming HTTP requests...")
	fmt.Println()
	fmt.Printf("------------------- last %d requests -------------------\n", common.ClientRequestHistorySize)
	fmt.Println()

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
