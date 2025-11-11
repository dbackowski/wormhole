package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	mu      sync.RWMutex
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

func (s *Server) extractDomain(host string) (string, error) {
	parts := strings.Split(host, ".")

	if len(parts) == 0 || parts[0] == "" {
		return "", fmt.Errorf("invalid host: %s", host)
	}
	return parts[0], nil
}

func (s *Server) AddConnection(domain string, conn *websocket.Conn) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	s.mu.RLock()
	connection, exists := s.clients[message.Domain]
	s.mu.RUnlock()

	if exists {
		connection.Requests[message.UUID] <- message
	}
}

func (s *Server) RemoveMessageUUIDFromRequestsQueue(domain string, uuid string) {
	if _, exists := s.clients[domain]; exists {
		delete(s.clients[domain].Requests, uuid)
	}
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

	domain, err := s.extractDomain(r.Host)

	if err != nil {
		conn.Close()
		return
	}

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
				fmt.Printf("%s Received HTTP response %d for UUID: %s\n", common.FormatTime(time.Now()), message.Status, message.UUID)
			}
			s.AddMessageToRequestsQueue(&message)
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	domain, err := s.extractDomain(r.Host)
	if err != nil {
		http.Error(w, "Invalid host", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	connection, exists := s.clients[domain]
	s.mu.RUnlock()

	if !exists {
		http.Error(w, "Tunnel not found", http.StatusNotFound)
		return
	} else {
		defer r.Body.Close()
		reqBody, err := io.ReadAll(r.Body)

		if err != nil {
			fmt.Printf("Error reading request body: %s\n", err)
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
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
			fmt.Printf("%s Forwarding %s request %s for %s to domain %s\n", common.FormatTime(time.Now()), requestMsg.Method, requestMsg.UUID, requestMsg.URL, domain)
		}

		connection.Conn.WriteJSON(requestMsg)
		connection.Requests[requestMsg.UUID] = make(chan *common.Message)

		select {
		case responseMsg := <-connection.Requests[requestMsg.UUID]:
			common.CopyHeaders(responseMsg.Headers, w.Header())
			w.WriteHeader(responseMsg.Status)
			w.Write(responseMsg.Body)
			s.RemoveMessageUUIDFromRequestsQueue(domain, requestMsg.UUID)
		case <-time.After(common.RequestTimeout):
			w.WriteHeader(http.StatusRequestTimeout)
			w.Write([]byte("Request timeout"))
			s.RemoveMessageUUIDFromRequestsQueue(domain, requestMsg.UUID)
		}
	}
}

func main() {
	common.ClearTerminal()
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
