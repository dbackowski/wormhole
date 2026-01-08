package client

import (
	"fmt"
	"net/http"
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
	domain     string
	tunnelURL  string
	Conn       *websocket.Conn
	proxy      *LocalProxy
	history    *RequestHistory
	display    Display
	Logger     *common.Logger
	dispatcher *common.MessageDispatcher
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

	logger := common.NewLogger(common.LevelError, "text")

	tunnelURL := buildTunnelURL(serverConfig, cfg.Domain)

	client := &Client{
		domain:    cfg.Domain,
		tunnelURL: tunnelURL,
		Conn:      conn,
		proxy:     NewLocalProxy(cfg.Local, tunnelURL, common.RequestTimeout),
		history:   NewRequestHistory(common.ClientRequestHistorySize),
		display:   NewTerminalDisplay(common.ClientRequestHistorySize),
		Logger:    logger,
	}

	client.setupMessageHandlers()

	return client, nil
}

func (c *Client) HandleConnection() {
	var message common.Message

	for {
		err := c.Conn.ReadJSON(&message)
		if err != nil {
			c.Logger.Error("Failed to read message", "error", err)
			return
		}

		if err := c.dispatcher.Dispatch(&message); err != nil {
			c.Logger.Error("Failed to dispatch message", "error", err)
			return
		}
	}
}

func (c *Client) sendResponse(msg *common.Message, proxyResp *ProxyResponse, proxyErr error) error {
	responseMsg := common.Message{
		Type:   common.MessageTypeHTTPResponse,
		Domain: c.domain,
		UUID:   msg.UUID,
		Method: msg.Method,
		URL:    msg.URL,
	}

	if proxyErr != nil {
		responseMsg.Status = http.StatusBadGateway
		responseMsg.Body = []byte(http.StatusText(http.StatusBadGateway))
	} else {
		responseMsg.Status = proxyResp.StatusCode
		responseMsg.Body = proxyResp.Body
		responseMsg.Headers = proxyResp.Headers
	}

	if err := c.Conn.WriteJSON(responseMsg); err != nil {
		return fmt.Errorf("failed to send response: %w", err)
	}

	return nil
}

func (c *Client) RefreshTerminalOutput() {
	common.ClearTerminal()
	recentLogs := c.history.GetRecent(common.ClientTerminalMaxLogs)
	c.display.ShowConnectionInfo(c.tunnelURL)
	c.display.ShowRequestHistory(recentLogs)
}

func closeWebsocket(c *websocket.Conn) error {
	return c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

func buildProxyRequest(msg *common.Message) ProxyRequest {
	return ProxyRequest{
		Method:  msg.Method,
		URL:     msg.URL,
		Headers: msg.Headers,
		Body:    msg.Body,
	}
}

func buildWebSocketURL(s *ServerConfig, domain string) string {
	return fmt.Sprintf("%s://%s.%s/ws", s.WSScheme, domain, s.Host)
}

func ConnectToServer(websocketURL string) (*websocket.Conn, error) {
	conn, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	return conn, err
}
