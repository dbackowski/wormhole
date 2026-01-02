package server

import (
	"context"
	"fmt"
	"io"
	"net/http"

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

	s.forwardAndWaitForResponse(w, connection, requestMsg, domain, r.Context())
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

func (s *Server) forwardAndWaitForResponse(w http.ResponseWriter, connection *Connection, requestMsg *common.Message, domain string, ctx context.Context) {
	s.logForwardingRequest(requestMsg, domain)

	responseChan, cancelCleanup, err := s.registerAndForwardRequest(connection, requestMsg, ctx)
	if err != nil {
		s.sendHTTPError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer s.CleanupRequest(domain, requestMsg.UUID)
	defer cancelCleanup()
	s.handleResponse(w, responseChan, ctx)
}

func (s *Server) registerAndForwardRequest(connection *Connection, requestMsg *common.Message, ctx context.Context) (chan *common.Message, context.CancelFunc, error) {
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

func (s *Server) handleResponse(w http.ResponseWriter, responseChan chan *common.Message, ctx context.Context) {
	select {
	case responseMsg, ok := <-responseChan:
		if !ok || responseMsg == nil {
			s.writeTimeoutResponse(w)
			return
		}
		s.writeSuccessResponse(w, responseMsg)
	case <-ctx.Done():
		s.writeTimeoutResponse(w)
	}
}

func (s *Server) logForwardingRequest(requestMsg *common.Message, domain string) {
	s.Logger.Info("Forwarding HTTP request to client",
		"domain", domain,
		"uuid", requestMsg.UUID,
		"method", requestMsg.Method,
		"url", requestMsg.URL,
	)

	s.Logger.Debug("Request details",
		"domain", domain,
		"uuid", requestMsg.UUID,
		"headers", requestMsg.Headers,
		"body", string(requestMsg.Body))
}
