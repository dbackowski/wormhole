package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Connection struct {
	Domain   string
	Conn     *websocket.Conn
	Requests map[string]chan *common.Message
}

type Server struct {
	clients map[string]*Connection
	debug   bool
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func GenerateUUID() string {
	return uuid.New().String()
}

func (s *Server) AddConnection(domain string, conn *websocket.Conn) error {
	_, exists := s.clients[domain]
	if exists {
		return fmt.Errorf("domain %s is already taken", domain)
	}

	s.clients[domain] = &Connection{
		Domain:   domain,
		Conn:     conn,
		Requests: make(map[string]chan *common.Message),
	}

	return nil
}

func (s *Server) AddMessageToRequestsQueue(message *common.Message) {
	connection, exists := s.clients[message.Domain]

	if exists {
		connection.Requests[message.UUID] <- message
	}
}

func (s *Server) RemoveMessageUUIDFromRequestsQueue(domain string, uuid string) {
	delete(s.clients[domain].Requests, uuid)
}

func (s *Server) RemoveConnection(domain string) {
	delete(s.clients, domain)
}

func (s *Server) ServeWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Error upgrading:", err)
		return
	}

	var domain = strings.Split(r.Host, ".")[0]

	err = s.AddConnection(domain, conn)

	if err != nil {
		conn.WriteJSON(common.Message{Type: common.MessageTypeDomainTaken})
		conn.Close()
		return
	}

	conn.WriteJSON(common.Message{Type: common.MessageTypeDomainRegistered})
	fmt.Printf("Registered connection for domain: %s\n", domain)

	go s.handleWebSocketConnection(domain, conn)
}

func (s *Server) handleWebSocketConnection(domain string, conn *websocket.Conn) {
	defer conn.Close()

	for {
		var message common.Message

		err := conn.ReadJSON(&message)
		if err != nil {
			log.Printf("Read error: %v", err)
			fmt.Printf("Closing connection for domain: %s\n", domain)
			s.RemoveConnection(domain)
			return
		}

		switch message.Type {
		case common.MessageTypeHTTPResponse:
			if s.debug {
				fmt.Println("Received HTTP response from client:")
				common.PrettyPrintMessage(message)
			} else {
				fmt.Printf("Received HTTP response %d for UUID: %s\n", message.Status, message.UUID)
			}
			s.AddMessageToRequestsQueue(&message)
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var domain = strings.Split(r.Host, ".")[0]
	connection, exists := s.clients[domain]

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

		headers["Host"] = []string{r.Host}

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

		requestMsg := common.Message{
			Type:    common.MessageTypeHTTPRequest,
			UUID:    GenerateUUID(),
			URL:     r.URL.String(),
			Method:  r.Method,
			Headers: headers,
			Body:    reqBody,
		}

		if s.debug {
			fmt.Println("Forwarding HTTP request to client:")
			common.PrettyPrintMessage(requestMsg)
		} else {
			fmt.Printf("Forwarding %s request %s for %s to domain %s\n", requestMsg.Method, requestMsg.UUID, requestMsg.URL, domain)
		}

		connection.Conn.WriteJSON(requestMsg)
		connection.Requests[requestMsg.UUID] = make(chan *common.Message)

		select {
		case responseMsg := <-connection.Requests[requestMsg.UUID]:
			common.CopyHeaders(responseMsg.Headers, w.Header())
			w.WriteHeader(responseMsg.Status)
			w.Write(responseMsg.Body)
			s.RemoveMessageUUIDFromRequestsQueue(domain, requestMsg.UUID)
		case <-time.After(10 * time.Second):
			w.WriteHeader(http.StatusRequestTimeout)
			w.Write([]byte("Request timeout"))
			s.RemoveMessageUUIDFromRequestsQueue(domain, requestMsg.UUID)
		}
	}
}

func main() {
	fmt.Printf("\x1bc")

	var port = flag.Int("port", 8080, "Port to run the server on")
	var debug = flag.Bool("debug", false, "Enable debug mode")

	flag.Parse()

	var server = Server{
		clients: make(map[string]*Connection),
		debug:   *debug,
	}

	http.HandleFunc("/ws", server.ServeWebSocket)
	http.HandleFunc("/", server.ServeHTTP)
	fmt.Println("WebSocket server started on :" + strconv.Itoa(*port))
	err := http.ListenAndServe(":"+strconv.Itoa(*port), nil)

	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}
