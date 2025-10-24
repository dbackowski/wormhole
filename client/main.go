package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type    string              `json:"type"`
	Domain  string              `json:"domain,omitempty"`
	UUID    string              `json:"uuid"`
	Method  string              `json:"method,omitempty"`
	URL     string              `json:"url,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
	Status  int                 `json:"status,omitempty"`
}

func prettyPrintMessage(msg Message) {
	clone := msg
	clone.Body = nil
	jsonMessage, _ := json.MarshalIndent(clone, "", "  ")
	fmt.Println(string(jsonMessage))
}

func closeWebsocket(c *websocket.Conn) {
	err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Println("write close:", err)
		return
	}
}

func handleServerHTTPRequest(conn *websocket.Conn, domain string, message Message, localURL string, debug bool) {
	req, err := http.NewRequest(message.Method, localURL, bytes.NewReader(message.Body))
	if err != nil {
		fmt.Printf("client: could not create request: %s\n", err)
		return
	}

	req.Host = message.Headers["Host"][0]

	for key, values := range message.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	var customClient = &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true, // Disable connection reuse
		},
		// Don't follow redirects, instead pass them back to the browser
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	res, err := customClient.Do(req)

	if err != nil {
		responseMsg := Message{
			Type:   "http_response",
			Domain: domain,
			UUID:   message.UUID,
			Method: message.Method,
			URL:    message.URL,
			Status: 502,
			Body:   []byte("Bad Gateway"),
		}

		if debug {
			fmt.Printf("client: error making http request: %s\n", err)
		} else {
			fmt.Printf("%s %s -> %d\n", responseMsg.Method, localURL, responseMsg.Status)
		}

		conn.WriteJSON(responseMsg)
		return
	}
	defer res.Body.Close()

	if debug {
		fmt.Printf("Response status code: %d\n", res.StatusCode)
	} else {
		fmt.Printf("%s %s -> %d\n", message.Method, localURL, res.StatusCode)
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("client: could not read response body: %s\n", err)
		return
	}

	headers := make(map[string][]string)

	for key, values := range res.Header {
		if len(values) > 0 {
			headers[key] = values
		}
	}

	responseMsg := Message{
		Type:    "http_response",
		Domain:  domain,
		UUID:    message.UUID,
		Method:  message.Method,
		URL:     message.URL,
		Headers: headers,
		Body:    resBody,
		Status:  res.StatusCode,
	}

	conn.WriteJSON(responseMsg)
}

func main() {
	fmt.Printf("\x1bc")

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
		log.Fatal("dial:", err)
	}

	fmt.Println("Connected to server.")
	scheme := strings.Split(*server, "://")[0]
	fmt.Println("Your tunnel is available at:", scheme+"://"+*domain+"."+serverHost)
	fmt.Println("Waiting for incoming HTTP requests...")

	defer conn.Close()

	for {
		var message Message

		err := conn.ReadJSON(&message)
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}

		switch message.Type {
		case "domain_taken":
			fmt.Println("Domain is already taken. Please choose another one.")
			closeWebsocket(conn)
			return
		case "http_request":
			localURL := *local + message.URL

			if *debug {
				fmt.Println("Received HTTP request notification from server:")
				prettyPrintMessage(message)
				fmt.Printf("Forwarding to local server at %s\n", localURL)
			}

			handleServerHTTPRequest(conn, *domain, message, localURL, *debug)
		}
	}
}
