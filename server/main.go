package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

type Connections struct {
	Domain string
	Conn   *websocket.Conn
}

type Message struct {
	Type string `json:"type"`
}

var connections = make(map[string]*Connections)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Error upgrading:", err)
		return
	}

	log.Printf("Domain %s is already taken", "damian")

	var domain = strings.Split(r.Host, ".")[0]

	if checkIfDomainAvailable(domain) {
		domainTakenMsg := Message{
			Type: "domain_taken",
		}

		conn.WriteJSON(domainTakenMsg)
		conn.Close()
		return
	}

	connections[domain] = &Connections{
		Domain: domain,
		Conn:   conn,
	}

	fmt.Printf("New connection for domain: %s\n", domain)

	go handleConnection(conn)
}

func checkIfDomainAvailable(domain string) bool {
	_, exists := connections[domain]
	return exists
}

func handleConnection(conn *websocket.Conn) {

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("Error reading message:", err)
			break
		}
		fmt.Printf("Received: %s\n", message)

		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			fmt.Println("Error writing message:", err)
			break
		}
	}

	defer conn.Close()
}

func main() {
	flag.Int("port", 8080, "Port to run the server on (default: 8080, can also use PORT env var)")
	flag.Parse()

	port := flag.Lookup("port").Value.String()
	http.HandleFunc("/ws", wsHandler)
	fmt.Println("WebSocket server started on :" + port)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}
