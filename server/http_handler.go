package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/google/uuid"
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	connection, domain, err := s.getConnectionForDomain(r)

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

func isWebSocketUpgradeRequest(headers http.Header) bool {
	return headers.Get("Connection") == "Upgrade" &&
		headers.Get("Upgrade") == "websocket"
}

func prepareRequestHeaders(r *http.Request) map[string][]string {
	headers := make(map[string][]string)
	headers["Host"] = []string{r.Host}

	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values
		}
	}

	return headers
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

	return &common.Message{
		Type:    common.MessageTypeHTTPRequest,
		UUID:    uuid.New().String(),
		URL:     r.URL.String(),
		Method:  r.Method,
		Headers: prepareRequestHeaders(r),
		Body:    reqBody,
	}, nil
}

func (s *Server) sendHTTPError(w http.ResponseWriter, message string, statusCode int) {
	http.Error(w, message, statusCode)
}

func (s *Server) forwardAndWaitForResponse(w http.ResponseWriter, connection *Connection, requestMsg *common.Message, domain string) {
	s.logForwardingRequest(requestMsg, domain)

	responseChan, cancelCleanup, err := s.registerAndForwardRequest(connection, requestMsg)
	if err != nil {
		s.sendHTTPError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer s.CleanupRequest(domain, requestMsg.UUID)
	defer cancelCleanup()
	s.handleResponse(w, responseChan)
}

func (s *Server) registerAndForwardRequest(connection *Connection, requestMsg *common.Message) (chan *common.Message, context.CancelFunc, error) {
	ctx := context.Background()
	responseChan, cancelCleanup := connection.Requests.Register(ctx, requestMsg.UUID)
	if err := connection.Conn.WriteJSON(requestMsg); err != nil {
		connection.Requests.Cleanup(requestMsg.UUID)
		return nil, cancelCleanup, err
	}

	return responseChan, cancelCleanup, nil
}

func (s *Server) writeSuccessResponse(w http.ResponseWriter, responseMsg *common.Message) {
	common.CopyHTTPHeaders(responseMsg.Headers, w.Header())
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
