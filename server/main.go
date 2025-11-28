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

type ConnectionManager struct {
	mu      sync.RWMutex
	clients map[string]*Connection
}

type Server struct {
	connManager *ConnectionManager
	debug       bool
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		clients: make(map[string]*Connection),
	}
}

func isWebSocketUpgradeRequest(headers http.Header) bool {
	return headers.Get("Connection") == "Upgrade" &&
		headers.Get("Upgrade") == "websocket"
}

func (cm *ConnectionManager) AddConnection(domain string, conn *websocket.Conn) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	_, exists := cm.clients[domain]
	if exists {
		return fmt.Errorf("domain %s is already taken", domain)
	}

	cm.clients[domain] = &Connection{
		Domain:   domain,
		Conn:     conn,
		Requests: make(map[string]chan *common.Message),
	}

	return nil
}

func (cm *ConnectionManager) GetConnection(domain string) (*Connection, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	connection, exists := cm.clients[domain]

	return connection, exists
}

func (cm *ConnectionManager) RemoveConnection(domain string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.clients, domain)
}

func (c *Connection) AddMessageToRequestsQueue(message *common.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch, exists := c.Requests[message.UUID]
	if !exists {
		fmt.Printf("Received late response for UUID %s (already timed out)\n", message.UUID)
		return
	}

	ch <- message
}

func (c *Connection) RemoveMessageUUIDFromRequestsQueue(uuid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Requests, uuid)
}

func (s *Server) extractDomain(host string) (string, error) {
	parts := strings.Split(host, ".")

	if len(parts) == 0 || parts[0] == "" {
		return "", fmt.Errorf("invalid host: %s", host)
	}
	return parts[0], nil
}

func (s *Server) RegisterRequest(message *common.Message) {
	connection, exists := s.connManager.GetConnection(message.Domain)

	if exists {
		connection.AddMessageToRequestsQueue(message)
	}
}

func (s *Server) UnregisterRequest(domain string, uuid string) {
	connection, exists := s.connManager.GetConnection(domain)

	if exists {
		connection.RemoveMessageUUIDFromRequestsQueue(uuid)
	}
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

	err = s.connManager.AddConnection(domain, conn)

	if err != nil {
		if err = conn.WriteJSON(common.Message{Type: common.MessageTypeDomainTaken}); err != nil {
			fmt.Printf("Failed to send message: %v\n", err)
		}
		conn.Close()
		return
	}

	if err = conn.WriteJSON(common.Message{Type: common.MessageTypeDomainRegistered}); err != nil {
		fmt.Printf("Failed to send message: %v\n", err)
		s.connManager.RemoveConnection(domain)
		return
	}

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
			s.connManager.RemoveConnection(domain)
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
			s.RegisterRequest(&message)
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

	connection, exists := s.connManager.GetConnection(domain)

	if !exists {
		return nil, domain, fmt.Errorf("tunnel not found for domain: %s", domain)
	}

	return connection, domain, nil
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
		UUID:    uuid.New().String(),
		URL:     r.URL.String(),
		Method:  r.Method,
		Headers: headers,
		Body:    reqBody,
	}, nil
}

func (s *Server) logForwardingRequest(requestMsg *common.Message, domain string) {
	if s.debug {
		fmt.Println("Forwarding HTTP request to client:")
		common.PrettyPrintMessage(*requestMsg)
	} else {
		fmt.Printf("%s Forwarding %s request %s for %s to domain %s\n",
			common.FormatTime(time.Now()),
			requestMsg.Method,
			requestMsg.UUID,
			requestMsg.URL,
			domain)
	}
}

func (s *Server) forwardAndWaitForResponse(w http.ResponseWriter, connection *Connection, requestMsg *common.Message, domain string) {
	s.logForwardingRequest(requestMsg, domain)

	responseChan, err := s.registerAndForwardRequest(connection, requestMsg)
	if err != nil {
		s.sendHTTPError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer s.UnregisterRequest(domain, requestMsg.UUID)

	s.handleResponse(w, responseChan)
}

func (s *Server) registerAndForwardRequest(connection *Connection, requestMsg *common.Message) (chan *common.Message, error) {
	connection.mu.Lock()
	connection.Requests[requestMsg.UUID] = make(chan *common.Message)
	connection.mu.Unlock()

	if err := connection.Conn.WriteJSON(requestMsg); err != nil {
		return nil, fmt.Errorf("failed to forward request to client: %v", err)
	}
	return connection.Requests[requestMsg.UUID], nil
}

func (s *Server) writeSuccessResponse(w http.ResponseWriter, responseMsg *common.Message) {
	common.CopyHeaders(responseMsg.Headers, w.Header())
	w.WriteHeader(responseMsg.Status)
	w.Write(responseMsg.Body)
}

func (s *Server) writeTimeoutResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusRequestTimeout)
	w.Write([]byte("Request timeout"))
}

func (s *Server) handleResponse(w http.ResponseWriter, responseChan chan *common.Message) {
	select {
	case responseMsg := <-responseChan:
		s.writeSuccessResponse(w, responseMsg)
	case <-time.After(common.RequestTimeout):
		s.writeTimeoutResponse(w)
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	connection, domain, err := s.getConnectionForRequest(r)

	if err != nil {
		s.sendHTTPError(w, err.Error(), http.StatusBadRequest)
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
	var port = flag.Int("port", common.DefaultServerPort, "Port to run the server on")
	var debug = flag.Bool("debug", false, "Enable debug mode")

	flag.Parse()

	server := Server{
		connManager: NewConnectionManager(),
		debug:       *debug,
	}

	http.HandleFunc("/ws", server.ServeWebSocket)
	http.HandleFunc("/", server.ServeHTTP)
	fmt.Println("WebSocket server started on :" + strconv.Itoa(*port))
	err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)

	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}
