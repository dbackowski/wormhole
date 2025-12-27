package server

import (
	"fmt"
	"time"

	"github.com/dbackowski/wormhole/common"
)

type Server struct {
	connManager *ConnectionManager
	dispatcher  *common.MessageDispatcher
	debug       bool
}

func NewServer(debug *bool) *Server {
	server := Server{
		connManager: NewConnectionManager(),
		debug:       *debug,
	}
	server.setupMessageHandlers()
	return &server
}

func (s *Server) setupMessageHandlers() {
	s.dispatcher = common.NewMessageDispatcher()
	s.dispatcher.Register(common.MessageTypeHTTPResponse, s.handleHTTPResponse)
}

func (s *Server) DeliverResponse(message *common.Message) {
	connection, exists := s.connManager.GetConnection(message.Domain)

	if exists {
		connection.Requests.Deliver(message)
	}
}

func (s *Server) CleanupRequest(domain string, uuid string) {
	connection, exists := s.connManager.GetConnection(domain)

	if exists {
		connection.Requests.Cleanup(uuid)
	}
}

func (s *Server) handleHTTPResponse(msg *common.Message) {
	if s.debug {
		fmt.Println("Received HTTP response from client:")
		common.PrettyPrintMessage(*msg)
	} else {
		fmt.Printf("%s Received HTTP response %d for UUID: %s\n", common.FormatTime(time.Now()), msg.Status, msg.UUID)
	}
	s.DeliverResponse(msg)
}
