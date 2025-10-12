package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Connection struct {
	Domain   string
	Conn     *websocket.Conn
	Requests map[string]chan *Message
}

type Message struct {
	Type   string `json:"type"`
	Domain string `json:"domain,omitempty"`
	UUID   string `json:"uuid"`
	Method string `json:"method,omitempty"`
	URL    string `json:"url,omitempty"`
	Body   []byte `json:"body,omitempty"`
	Status int    `json:"status,omitempty"`
}

var connections = make(map[string]*Connection)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func GenerateUUID() string {
	return uuid.New().String()
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Error upgrading:", err)
		return
	}

	var domain = strings.Split(r.Host, ".")[0]

	if checkIfDomainAvailable(domain) {
		domainTakenMsg := Message{
			Type: "domain_taken",
		}

		conn.WriteJSON(domainTakenMsg)
		conn.Close()
		return
	}

	connections[domain] = &Connection{
		Domain:   domain,
		Conn:     conn,
		Requests: make(map[string]chan *Message),
	}

	fmt.Printf("New connection for domain: %s\n", domain)

	go handleWebSocketConnection(conn)
}

func checkIfDomainAvailable(domain string) bool {
	_, exists := connections[domain]
	return exists
}

func handleWebSocketConnection(conn *websocket.Conn) {
	defer conn.Close()

	for {
		var message Message

		err := conn.ReadJSON(&message)
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}

		switch message.Type {
		case "http_response":
			fmt.Println("Received HTTP response from client.")
			jsonMessage, _ := json.MarshalIndent(message, "", "  ")
			fmt.Println(string(jsonMessage))

			connection, exists := connections[message.Domain]

			if exists {
				fmt.Printf("Forwarding response for UUID %s with status %d\n", message.UUID, message.Status)
				connection.Requests[message.UUID] <- &message
			}
		}
	}
}

func handleHTTPConnection(w http.ResponseWriter, r *http.Request) {
	var domain = strings.Split(r.Host, ".")[0]
	connection, exists := connections[domain]

	if !exists {
		http.Error(w, "Tunnel not found", http.StatusNotFound)
		return
	} else {
		requestMsg := Message{
			Type:   "http_request",
			UUID:   GenerateUUID(),
			URL:    r.URL.String(),
			Method: r.Method,
		}
		jsonMessage, _ := json.MarshalIndent(requestMsg, "", "  ")
		fmt.Println(string(jsonMessage))
		connection.Conn.WriteJSON(requestMsg)

		connection.Requests[requestMsg.UUID] = make(chan *Message)

		responseMsg := <-connection.Requests[requestMsg.UUID]
		fmt.Printf("Forwarding response for UUID %s with status %d\n", responseMsg.UUID, responseMsg.Status)
		w.WriteHeader(responseMsg.Status)
		w.Write(responseMsg.Body)
		delete(connection.Requests, requestMsg.UUID)
	}
}

func main() {
	flag.Int("port", 8080, "Port to run the server on (default: 8080, can also use PORT env var)")
	flag.Parse()

	port := flag.Lookup("port").Value.String()
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/", handleHTTPConnection)
	fmt.Println("WebSocket server started on :" + port)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}
