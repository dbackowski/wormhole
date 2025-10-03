package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/gorilla/websocket"
)

func main() {
	var serverURL = flag.String("server", "http://localhost:8080", "Server URL")

	flag.Parse()

	fmt.Println("Connecting to", *serverURL)

	c, _, err := websocket.DefaultDialer.Dial("ws://localhost:8080/ws", nil)
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
