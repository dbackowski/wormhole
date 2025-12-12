package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"slices"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

type Client struct {
	Domain     string
	Local      string
	Tunnel     string
	Conn       *websocket.Conn
	HTTPClient *http.Client
}

type ServerConfig struct {
	HTTPScheme string
	WSScheme   string
	Host       string
}

func closeWebsocket(c *websocket.Conn) {
	err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Println("write close:", err)
		return
	}
}

func (c *Client) buildResponseMessage(message common.Message, status int, body []byte, headers http.Header) common.Message {
	return common.Message{
		Type:    common.MessageTypeHTTPResponse,
		Domain:  c.Domain,
		UUID:    message.UUID,
		Method:  message.Method,
		URL:     message.URL,
		Status:  status,
		Body:    body,
		Headers: headers,
	}
}

func (c *Client) makeLocalRequest(message common.Message, localURL string) (*http.Response, error) {
	req, err := http.NewRequest(message.Method, localURL, bytes.NewReader(message.Body))
	if err != nil {
		return nil, err
	}

	if hosts, ok := message.Headers["Host"]; ok && len(hosts) > 0 {
		req.Host = hosts[0]
	}

	common.CopyHeaders(message.Headers, req.Header)
	return c.HTTPClient.Do(req)
}

func (c *Client) logResponse(message common.Message, localURL string, statusCode int) {
	fmt.Printf("%s %s %s -> %d\n", common.FormatTime(time.Now()), message.Method, localURL, statusCode)
}

func (c *Client) forwardResponse(message common.Message, res *http.Response) error {
	resBody, err := io.ReadAll(res.Body)

	if err != nil {
		return fmt.Errorf("could not read response body: %w", err)
	}

	responseMsg := c.buildResponseMessage(message, res.StatusCode, resBody, res.Header)
	return c.Conn.WriteJSON(responseMsg)
}

func (c *Client) respondToServer(message common.Message, res *http.Response, err error) {
	if err != nil {
		c.sendErrorResponse(message, http.StatusBadGateway, "Bad Gateway")
		return
	}
	defer res.Body.Close()
	c.forwardResponse(message, res)
}

func (c *Client) sendErrorResponse(message common.Message, status int, errorMessage string) {
	responseMsg := c.buildResponseMessage(message, status, []byte(errorMessage), nil)
	err := c.Conn.WriteJSON(responseMsg)

	if err != nil {
		log.Printf("Failed to send error response: %v", err)
	}
}

func (c *Client) handleServerHTTPRequest(message common.Message, localURL string) {
	res, err := c.makeLocalRequest(message, localURL)

	statusCode := http.StatusBadGateway

	if err == nil {
		statusCode = res.StatusCode
	}

	c.logResponse(message, localURL, statusCode)
	c.respondToServer(message, res, err)
}

func (c *Client) handleDomainRegistered() {
	common.ClearTerminal()
	fmt.Println("Connected to server.")
	fmt.Printf("Your tunnel is available at: %s\n", c.Tunnel)
	fmt.Println("Waiting for incoming HTTP requests...")
	fmt.Println()
	fmt.Println("-------------------")
	fmt.Println()
}

func (c *Client) handleDomainTaken() {
	fmt.Println("Domain is already taken. Please choose another one.")
	closeWebsocket(c.Conn)

}

func (c *Client) buildLocalURL(requestPath string) string {
	baseURL, _ := url.Parse(c.Local)
	baseURL.Path = path.Join(baseURL.Path, requestPath)
	return baseURL.String()
}

func (c *Client) handleHTTPRequest(message common.Message) {
	localURL := c.buildLocalURL(message.URL)
	c.handleServerHTTPRequest(message, localURL)
}

func (c *Client) handleConnection() {
	var message common.Message

	for {
		err := c.Conn.ReadJSON(&message)
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}

		switch message.Type {
		case common.MessageTypeDomainRegistered:
			c.handleDomainRegistered()
		case common.MessageTypeDomainTaken:
			c.handleDomainTaken()
		case common.MessageTypeHTTPRequest:
			c.handleHTTPRequest(message)
		}
	}
}

func validateAndParseServerURL(rawURL string) (*ServerConfig, error) {
	parsed, err := parseURL(rawURL)
	if err != nil {
		return nil, err
	}

	wsScheme := "ws"
	if parsed.Scheme == "https" {
		wsScheme = "wss"
	}

	return &ServerConfig{
		HTTPScheme: parsed.Scheme,
		WSScheme:   wsScheme,
		Host:       parsed.Host,
	}, nil
}

func parseURL(rawURL string) (url.URL, error) {
	if rawURL == "" {
		return url.URL{}, fmt.Errorf("url is empty")
	}

	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return url.URL{}, fmt.Errorf("invalid url: %w", err)
	}

	supportedSchemes := []string{"http", "https"}
	if !slices.Contains(supportedSchemes, parsed.Scheme) {
		return url.URL{}, fmt.Errorf("unsupported url scheme: %s", parsed.Scheme)
	}

	if parsed.Host == "" {
		return url.URL{}, fmt.Errorf("url missing host")
	}

	return *parsed, nil
}

func buildWebSocketURL(s *ServerConfig, domain string) string {
	return fmt.Sprintf("%s://%s.%s/ws", s.WSScheme, domain, s.Host)
}

func validateClientConfig(domain, local string) error {
	if domain == "" {
		return fmt.Errorf("domain is required. Use -domain flag")
	}

	if local == "" {
		return fmt.Errorf("local is required. Use -local flag")
	}

	_, err := parseURL(local)

	if err != nil {
		return fmt.Errorf("invalid local URL: %w", err)
	}

	return nil
}

func connectToServer(websocketURL string) (*websocket.Conn, error) {
	conn, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)

	return conn, err
}

func NewClient(server, domain, local string) (*Client, error) {
	if err := validateClientConfig(domain, local); err != nil {
		return nil, err
	}

	serverConfig, err := validateAndParseServerURL(server)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	conn, err := connectToServer(buildWebSocketURL(serverConfig, domain))
	if err != nil {
		return nil, err
	}

	return &Client{
		Domain: domain,
		Conn:   conn,
		Local:  local,
		Tunnel: fmt.Sprintf("%s://%s.%s", serverConfig.HTTPScheme, domain, serverConfig.Host),
		HTTPClient: &http.Client{
			Timeout:       common.RequestTimeout,
			Transport:     &http.Transport{DisableKeepAlives: true},                                              // Disable connection reuse
			CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }, // Don't follow redirects, instead pass them back to the browser
		},
	}, nil
}

func main() {
	var server = flag.String("server", common.DefaultClientServerURL, "Server URL")
	var domain = flag.String("domain", "", "Custom domain")
	var local = flag.String("local", "", "Local server URL")
	flag.Parse()

	client, err := NewClient(*server, *domain, *local)
	if err != nil {
		log.Fatal(err)
	}

	defer client.Conn.Close()
	client.handleConnection()
}
