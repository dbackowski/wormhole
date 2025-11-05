package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
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

func makeHTTPRequestFromMessage(message common.Message, localURL string) (*http.Response, error) {
	req, err := http.NewRequest(message.Method, localURL, bytes.NewReader(message.Body))
	if err != nil {
		return nil, err
	}

	req.Host = message.Headers["Host"][0]
	common.CopyHeaders(message.Headers, req.Header)

	var customClient = &http.Client{
		Timeout:       10 * time.Second,
		Transport:     &http.Transport{DisableKeepAlives: true},                                              // Disable connection reuse
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }, // Don't follow redirects, instead pass them back to the browser
	}

	res, err := customClient.Do(req)
	return res, err
}

func (client *Client) handleServerHTTPRequest(message common.Message, localURL string) {
	res, err := makeHTTPRequestFromMessage(message, localURL)

	if err != nil {
		responseMsg := common.Message{
			Type:   common.MessageTypeHTTPResponse,
			Domain: client.Domain,
			UUID:   message.UUID,
			Method: message.Method,
			URL:    message.URL,
			Status: 502,
			Body:   []byte("Bad Gateway"),
		}

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
		fmt.Printf("%s %s -> %d\n", message.Method, localURL, res.StatusCode)
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("client: could not read response body: %s\n", err)
		return
	}

	responseMsg := common.Message{
		Type:    common.MessageTypeHTTPResponse,
		Domain:  client.Domain,
		UUID:    message.UUID,
		Method:  message.Method,
		URL:     message.URL,
		Headers: res.Header,
		Body:    resBody,
		Status:  res.StatusCode,
	}

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
			fmt.Printf("\x1bc") // Clear terminal
			fmt.Println("Connected to server.")
			scheme := strings.Split(client.ServerHost, "://")[0]
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
	var ws_scheme string

	if strings.HasPrefix(*server, "https://") {
		ws_scheme = "wss"
	} else {
		ws_scheme = "ws"
	}

	serverHost := strings.TrimPrefix(*server, "http://")
	serverHost = strings.TrimPrefix(serverHost, "https://")
	serverHost = strings.TrimRight(serverHost, "/")

	var websocketURL = fmt.Sprintf("%s://%s.%s/ws", ws_scheme, *domain, serverHost)

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
