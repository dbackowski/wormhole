package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type   string `json:"type"`
	Method string `json:"method,omitempty"`
	URL    string `json:"url,omitempty"`
}

func closeWebsocket(c *websocket.Conn) {
	err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Println("write close:", err)
		return
	}
}

func handleServerHTTPRequest(message Message, localURL string) {
	req, err := http.NewRequest(message.Method, localURL, nil)
	if err != nil {
		fmt.Printf("client: could not create request: %s\n", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("client: error making http request: %s\n", err)
	}

	fmt.Printf("client: got response!\n")
	fmt.Printf("client: status code: %d\n", res.StatusCode)

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("client: could not read response body: %s\n", err)
	}
	fmt.Printf("client: response body: %s\n", resBody)
	defer res.Body.Close()
}

func main() {
	var serverURL = flag.String("server", "localhost:8080", "Server URL")
	var domain = flag.String("domain", "", "Custom domain")
	var local = flag.String("local", "", "Local server URL")

	flag.Parse()

	if *domain == "" {
		log.Fatal("domain is required. Use -domain flag")
	}

	if *local == "" {
		log.Fatal("local is required. Use -local flag")
	}

	var websocketURL = fmt.Sprintf("ws://%s.%s/ws", *domain, *serverURL)
	fmt.Println("Connecting to", websocketURL)

	c, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	for {
		var message Message

		err := c.ReadJSON(&message)
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}

		switch message.Type {
		case "domain_taken":
			fmt.Println("Domain is already taken. Please choose another one.")
			closeWebsocket(c)
			return
		case "http_request":
			fmt.Println("Received HTTP request notification from server.")
			localURL := *local + message.URL
			fmt.Printf("Forwarding to local server at %s\n", localURL)
			jsonMessage, _ := json.MarshalIndent(message, "", "  ")
			fmt.Println(string(jsonMessage))
			handleServerHTTPRequest(message, localURL)
		}
	}
}
