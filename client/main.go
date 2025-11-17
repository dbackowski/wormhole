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
}

func closeWebsocket(c *websocket.Conn) {
	err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Println("write close:", err)
		return
	}
}

func (client *Client) buildResponseMessage(message common.Message, status int, body []byte, headers http.Header) common.Message {
	return common.Message{
		Type:    common.MessageTypeHTTPResponse,
		Domain:  client.Domain,
		UUID:    message.UUID,
		Method:  message.Method,
		URL:     message.URL,
		Status:  status,
		Body:    body,
		Headers: headers,
	}
}

func makeHTTPRequestFromMessage(message common.Message, localURL string) (*http.Response, error) {
	req, err := http.NewRequest(message.Method, localURL, bytes.NewReader(message.Body))
	if err != nil {
		return nil, err
	}

	req.Host = message.Headers["Host"][0]
	common.CopyHeaders(message.Headers, req.Header)

	var customClient = &http.Client{
		Timeout:       common.RequestTimeout,
		Transport:     &http.Transport{DisableKeepAlives: true},                                              // Disable connection reuse
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }, // Don't follow redirects, instead pass them back to the browser
	}

	return customClient.Do(req)
}

func (client *Client) handleServerHTTPRequest(message common.Message, localURL string) {
	res, err := makeHTTPRequestFromMessage(message, localURL)

	if err != nil {
		responseMsg := client.buildResponseMessage(message, 502, []byte("Bad Gateway"), nil)

		if client.Debug {
			fmt.Printf("client: error making http request: %s\n", err)
		} else {
			fmt.Printf("%s %s -> %d\n", responseMsg.Method, localURL, responseMsg.Status)
		}

		client.Conn.WriteJSON(responseMsg)
		return
	}
	defer res.Body.Close()

	if client.Debug {
		fmt.Printf("Response status code: %d\n", res.StatusCode)
	} else {
		fmt.Printf("%s %s %s -> %d\n", common.FormatTime(time.Now()), message.Method, localURL, res.StatusCode)
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("client: could not read response body: %s\n", err)
		return
	}

	responseMsg := client.buildResponseMessage(message, res.StatusCode, resBody, res.Header)
	client.Conn.WriteJSON(responseMsg)
}

func (client *Client) handleConnection() {
	var message common.Message

	for {
		err := client.Conn.ReadJSON(&message)
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}

		switch message.Type {
		case common.MessageTypeDomainRegistered:
			common.ClearTerminal()
			fmt.Println("Connected to server.")
			scheme := strings.Split(client.Local, "://")[0]
			fmt.Println("Your tunnel is available at:", scheme+"://"+client.Domain+"."+client.ServerHost)
			fmt.Println("Waiting for incoming HTTP requests...")
			fmt.Println()
			fmt.Println("-------------------")
			fmt.Println()

		case common.MessageTypeDomainTaken:
			fmt.Println("Domain is already taken. Please choose another one.")
			closeWebsocket(client.Conn)
			return

		case common.MessageTypeHTTPRequest:
			localURL := client.Local + message.URL

			if client.Debug {
				fmt.Println("Received HTTP request notification from server:")
				common.PrettyPrintMessage(message)
				fmt.Printf("Forwarding to local server at %s\n", localURL)
			}

			client.handleServerHTTPRequest(message, localURL)
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

func main() {
	var server = flag.String("server", "http://localhost:8080", "Server URL")
	var domain = flag.String("domain", "", "Custom domain")
	var local = flag.String("local", "", "Local server URL")
	var debug = flag.Bool("debug", false, "Enable debug mode")

	flag.Parse()

	if *domain == "" {
		log.Fatal("domain is required. Use -domain flag")
	}

	if *local == "" {
		log.Fatal("local is required. Use -local flag")
	}

	_, err := parseURL(*local)

	if err != nil {
		log.Fatalf("Invalid local URL: %v\n", err)
	}

	websocketURL, serverHost, err := buildWebSocketURL(*server, *domain)

	if err != nil {
		log.Fatalf("Invalid server URL: %v\n", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		log.Fatalf("Failed to connect to server at %s: %v\n", websocketURL, err)
	}

	var client = Client{
		Domain:     *domain,
		Conn:       conn,
		Local:      *local,
		ServerHost: serverHost,
		Debug:      *debug,
	}

	defer conn.Close()
	client.handleConnection()
}
