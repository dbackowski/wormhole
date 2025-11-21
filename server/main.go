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
	mu       sync.RWMutex
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

func (s *Server) GetConnection(domain string) (*Connection, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	connection, exists := s.clients[domain]

	return connection, exists
}

func (s *Server) RemoveConnection(domain string) {
	s.mu.Lock()
	delete(s.clients, domain)
	s.mu.Unlock()
}

func (s *Server) AddMessageToRequestsQueue(message *common.Message) {
	connection, exists := s.GetConnection(message.Domain)

	if exists {
		connection.AddMessageToRequestsQueue(message)
	}
}

func (s *Server) RemoveMessageUUIDFromRequestsQueue(domain string, uuid string) {
	connection, exists := s.GetConnection(domain)

	if exists {
		connection.RemoveMessageUUIDFromRequestsQueue(uuid)
	}
}

func (c *Connection) AddMessageToRequestsQueue(message *common.Message) {
	c.mu.Lock()
	c.Requests[message.UUID] <- message
	c.mu.Unlock()
}

func (c *Connection) RemoveMessageUUIDFromRequestsQueue(uuid string) {
	c.mu.Lock()
	delete(c.Requests, uuid)
	c.mu.Unlock()
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

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Panic in handleWebSocketConnection for %s: %v", domain, r)
			}
		}()
		s.handleWebSocketConnection(domain, conn)
	}()
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

func (s *Server) sendHTTPError(w http.ResponseWriter, message string, statusCode int) {
	http.Error(w, message, statusCode)
}

func (s *Server) getConnectionForRequest(r *http.Request) (*Connection, string, error) {
	domain, err := s.extractDomain(r.Host)

	if err != nil {
		return nil, "", err
	}

	s.mu.RLock()
	connection, exists := s.clients[domain]
	s.mu.RUnlock()

	if !exists {
		return nil, domain, fmt.Errorf("tunnel not found for domain: %s", domain)
	}

	return connection, domain, nil
}

func isWebSocketUpgradeRequest(headers http.Header) bool {
	return headers.Get("Connection") == "Upgrade" &&
		headers.Get("Upgrade") == "websocket"
}

func (s *Server) buildRequestMessage(r *http.Request) (*common.Message, error) {
	defer r.Body.Close()
	reqBody, err := io.ReadAll(r.Body)

	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	if isWebSocketUpgradeRequest(r.Header) {
		return nil, fmt.Errorf("WebSocket upgrade not supported")
	}

	headers := make(map[string][]string)
	headers["Host"] = []string{r.Host}

	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values
		}
	}

	return &common.Message{
		Type:    common.MessageTypeHTTPRequest,
		UUID:    GenerateUUID(),
		URL:     r.URL.String(),
		Method:  r.Method,
		Headers: headers,
		Body:    reqBody,
	}, nil
}

func (s *Server) forwardAndWaitForResponse(w http.ResponseWriter, connection *Connection, requestMsg *common.Message, domain string) {
	if s.debug {
		fmt.Println("Forwarding HTTP request to client:")
		common.PrettyPrintMessage(*requestMsg)
	} else {
		fmt.Printf("%s Forwarding %s request %s for %s to domain %s\n", common.FormatTime(time.Now()), requestMsg.Method, requestMsg.UUID, requestMsg.URL, domain)
	}

	s.mu.Lock()
	connection.Requests[requestMsg.UUID] = make(chan *common.Message)
	s.mu.Unlock()
	defer s.RemoveMessageUUIDFromRequestsQueue(domain, requestMsg.UUID)
	connection.Conn.WriteJSON(requestMsg)

	select {
	case responseMsg := <-connection.Requests[requestMsg.UUID]:
		common.CopyHeaders(responseMsg.Headers, w.Header())
		w.WriteHeader(responseMsg.Status)
		w.Write(responseMsg.Body)
	case <-time.After(common.RequestTimeout):
		w.WriteHeader(http.StatusRequestTimeout)
		w.Write([]byte("Request timeout"))
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	connection, domain, err := s.getConnectionForRequest(r)

	if err != nil {
		s.sendHTTPError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if connection == nil {
		s.sendHTTPError(w, "Tunnel not found", http.StatusNotFound)
		return
	}

	requestMsg, err := s.buildRequestMessage(r)

	if err != nil {
		s.sendHTTPError(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.forwardAndWaitForResponse(w, connection, requestMsg, domain)
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
