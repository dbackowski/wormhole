package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type string `json:"type"`
}

func closeWebsocket(c *websocket.Conn) {
	err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Println("write close:", err)
		return
	}
}

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
		}
	}
}
