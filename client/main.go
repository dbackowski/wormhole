package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

type Client struct {
	Domain     string
	Local      string
	ServerHost string
	Conn       *websocket.Conn
	Debug      bool
	HTTPClient *http.Client
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
	if c.Debug {
		fmt.Printf("Response status code: %d\n", statusCode)
	} else {
		fmt.Printf("%s %s %s -> %d\n", common.FormatTime(time.Now()), message.Method, localURL, statusCode)
	}
}

func (c *Client) forwardResponse(message common.Message, res *http.Response) error {
	resBody, err := io.ReadAll(res.Body)

	if err != nil {
		return fmt.Errorf("could not read response body: %w", err)
	}

	responseMsg := c.buildResponseMessage(message, res.StatusCode, resBody, res.Header)
	return c.Conn.WriteJSON(responseMsg)
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

	if err != nil {
		c.logResponse(message, localURL, 502)
		c.sendErrorResponse(message, http.StatusBadGateway, "Bad Gateway")
		return
	}
	defer res.Body.Close()

	c.logResponse(message, localURL, res.StatusCode)
	err = c.forwardResponse(message, res)

	if err != nil {
		c.sendErrorResponse(message, http.StatusBadGateway, "Bad Gateway")
		return
	}
}

func (c *Client) handleDomainRegistered() {
	common.ClearTerminal()
	fmt.Println("Connected to server.")
	scheme := strings.Split(c.Local, "://")[0]
	fmt.Printf("Your tunnel is available at: %s://%s.%s\n", scheme, c.Domain, c.ServerHost)
	fmt.Println("Waiting for incoming HTTP requests...")
	fmt.Println()
	fmt.Println("-------------------")
	fmt.Println()
}

func (c *Client) handleDomainTaken() {
	fmt.Println("Domain is already taken. Please choose another one.")
	closeWebsocket(c.Conn)

}
func (c *Client) handleHTTPRequest(message common.Message) {
	localURL := c.Local + message.URL

	if c.Debug {
		fmt.Println("Received HTTP request notification from server:")
		common.PrettyPrintMessage(message)
		fmt.Printf("Forwarding to local server at %s\n", localURL)
	}

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

func buildWebSocketURL(serverURL, domain string) (string, string, error) {
	parsed, err := parseURL(serverURL)
	if err != nil {
		return "", "", err
	}

	scheme := "ws"
	if parsed.Scheme == "https" {
		scheme = "wss"
	}

	host := parsed.Host
	wsURL := fmt.Sprintf("%s://%s.%s/ws", scheme, domain, host)
	return wsURL, host, nil
}

func NewClient(server, domain, local string, debug bool) (*Client, error) {
	if domain == "" {
		return nil, fmt.Errorf("domain is required. Use -domain flag")
	}

	if local == "" {
		return nil, fmt.Errorf("local is required. Use -local flag")
	}

	_, err := parseURL(local)

	if err != nil {
		return nil, fmt.Errorf("invalid local URL: %w", err)
	}

	websocketURL, serverHost, err := buildWebSocketURL(server, domain)

	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server at %s: %w", websocketURL, err)
	}

	return &Client{
		Domain:     domain,
		Conn:       conn,
		Local:      local,
		ServerHost: serverHost,
		Debug:      debug,
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
	var debug = flag.Bool("debug", false, "Enable debug mode")
	flag.Parse()

	client, err := NewClient(*server, *domain, *local, *debug)
	if err != nil {
		log.Fatal(err)
	}

	defer client.Conn.Close()
	client.handleConnection()
}
