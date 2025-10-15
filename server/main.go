package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	Type    string              `json:"type"`
	Domain  string              `json:"domain,omitempty"`
	UUID    string              `json:"uuid"`
	Method  string              `json:"method,omitempty"`
	URL     string              `json:"url,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
	Status  int                 `json:"status,omitempty"`
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

	go handleWebSocketConnection(domain, conn)
}

func checkIfDomainAvailable(domain string) bool {
	_, exists := connections[domain]
	return exists
}

func handleWebSocketConnection(domain string, conn *websocket.Conn) {
	defer conn.Close()

	for {
		var message Message

		err := conn.ReadJSON(&message)
		if err != nil {
			log.Printf("Read error: %v", err)
			fmt.Printf("Closing connection for domain: %s\n", domain)
			delete(connections, domain)
			return
		}

		switch message.Type {
		case "http_response":
			fmt.Println("Received HTTP response from client:")
			prettyPrintMessage(message)

			connection, exists := connections[message.Domain]

			if exists {
				connection.Requests[message.UUID] <- &message
			}
		}
	}
}

func prettyPrintMessage(msg Message) {
	clone := msg
	clone.Body = nil
	jsonMessage, _ := json.MarshalIndent(clone, "", "  ")
	fmt.Println(string(jsonMessage))
}

func handleHTTPConnection(w http.ResponseWriter, r *http.Request) {
	var domain = strings.Split(r.Host, ".")[0]
	connection, exists := connections[domain]

	if !exists {
		http.Error(w, "Tunnel not found", http.StatusNotFound)
		return
	} else {
		reqBody, err := io.ReadAll(r.Body)
		defer r.Body.Close()

		if err != nil {
			fmt.Printf("client: could not read response body: %s\n", err)
			return
		}
		headers := make(map[string][]string)

		for key, values := range r.Header {
			// Ignore WebSocket upgrade requests, solve how to handle them later
			if key == "Connection" && values[0] == "Upgrade" {
				fmt.Println("Ignoring WebSocket upgrade request")
				http.Error(w, "WebSocket upgrade not supported", http.StatusBadRequest)
				return
			}

			if len(values) > 0 {
				headers[key] = values
			}
		}

		requestMsg := Message{
			Type:    "http_request",
			UUID:    GenerateUUID(),
			URL:     r.URL.String(),
			Method:  r.Method,
			Headers: headers,
			Body:    reqBody,
		}

		fmt.Println("Forwarding HTTP request to client:")
		prettyPrintMessage(requestMsg)

		err = connection.Conn.WriteJSON(requestMsg)

		if err != nil {
			fmt.Printf("Error sending WebSocket message: %v\n", err)
			return
		}

		connection.Requests[requestMsg.UUID] = make(chan *Message)
		responseMsg := <-connection.Requests[requestMsg.UUID]

		for key, values := range responseMsg.Headers {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

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
