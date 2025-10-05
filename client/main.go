package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/gorilla/websocket"
)

func main() {
	var serverURL = flag.String("server", "localhost:8080", "Server URL")
	var domain = flag.String("domain", "", "Custom domain")
	flag.Parse()

	if *domain == "" {
		log.Fatal("domain is required. Use -domain flag")
	}

	var websocketURL = fmt.Sprintf("ws://%s.%s/ws", *domain, *serverURL)
	fmt.Println("Connecting to", websocketURL)

	c, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	err = c.WriteMessage(websocket.TextMessage, []byte("Hello, WebSocket!"))

	if err != nil {
		log.Println("write:", err)
		return
	}

	err = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Println("write close:", err)
		return
	}
}
