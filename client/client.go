package client

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dbackowski/wormhole/common"

	"github.com/gorilla/websocket"
)

type RequestLog struct {
	UUID            string
	Timestamp       time.Time
	Method          string
	URL             string
	StatusCode      int
	RequestHeaders  map[string][]string
	RequestBody     []byte
	ResponseHeaders map[string][]string
	ResponseBody    []byte
}

type Client struct {
	domain     string
	tunnelURL  string
	Conn       *websocket.Conn
	proxy      *LocalProxy
	history    *RequestHistory
	display    Display
	Logger     *common.Logger
	dispatcher *common.MessageDispatcher
	WebUIPort  int
}

func NewClient(cfg *Config) (*Client, error) {
	localURL, err := validateClientConfig(cfg.Domain, cfg.Local, cfg.WebUIPort)
	if err != nil {
		return nil, fmt.Errorf("invalid client config: %w", err)
	}

	serverConfig, err := validateAndParseServerURL(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	wsURL := common.BuildSubdomainURL(serverConfig.WSScheme, cfg.Domain, serverConfig.Host, "/ws")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", wsURL, err)
	}

	logger := common.NewLogger(common.LevelError, "text")

	tunnelURL := common.BuildSubdomainURL(serverConfig.HTTPScheme, cfg.Domain, serverConfig.Host, "")

	client := &Client{
		domain:    cfg.Domain,
		tunnelURL: tunnelURL,
		Conn:      conn,
		proxy:     NewLocalProxy(localURL, tunnelURL, common.RequestTimeout),
		history:   NewRequestHistory(common.ClientRequestHistorySize),
		display:   NewTerminalDisplay(common.ClientTerminalMaxLogs),
		Logger:    logger,
		WebUIPort: cfg.WebUIPort,
	}

	client.setupMessageHandlers()

	return client, nil
}

func (c *Client) HandleConnection() {
	common.RunMessageLoop(c.Conn, c.dispatcher,
		func(err error) {
			c.Logger.Error("Failed to read message", "error", err)
		},
		func(msg *common.Message, err error) {
			c.Logger.Error("Message handling failed",
				"type", msg.Type,
				"uuid", msg.UUID,
				"error", err)
		},
	)
}

func (c *Client) Shutdown() error {
	c.Logger.Info("Shutting down client")

	if err := closeWebsocket(c.Conn); err != nil {
		c.Logger.Error("Failed to send close frame", "error", err)
	}

	return c.Conn.Close()
}

func resolveProxyResponse(proxyResp *ProxyResponse, proxyErr error) ProxyResponse {
	if proxyErr != nil {
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
		Domain:  c.domain,
		UUID:    msg.UUID,
		Method:  msg.Method,
		URL:     msg.URL,
		Status:  resolved.StatusCode,
		Body:    resolved.Body,
		Headers: resolved.Headers,
	}

	if err := c.Conn.WriteJSON(responseMsg); err != nil {
		return fmt.Errorf("failed to send response: %w", err)
	}

	return nil
}

func (c *Client) RefreshTerminalOutput() {
	ClearTerminal()
	recentLogs := c.history.GetRecent(common.ClientTerminalMaxLogs)
	c.display.ShowConnectionInfo(c.tunnelURL, c.WebUIPort)
	c.display.ShowRequestHistory(recentLogs)
}

func closeWebsocket(c *websocket.Conn) error {
	return c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}
