package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dbackowski/wormhole/common"
	"github.com/google/uuid"
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	domain, err := s.extractDomain(r.Host)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	connection, err := s.connManager.GetConnection(domain)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	requestMsg, err := s.buildRequestMessage(r)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.forwardAndWaitForResponse(r.Context(), w, connection, requestMsg, domain)
}

func isWebSocketUpgradeRequest(headers http.Header) bool {
	return strings.EqualFold(headers.Get("Connection"), "upgrade") &&
		strings.EqualFold(headers.Get("Upgrade"), "websocket")
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
	if isWebSocketUpgradeRequest(r.Header) {
		return nil, fmt.Errorf("WebSocket upgrade not supported")
	}

	defer r.Body.Close()
	reqBody, err := io.ReadAll(r.Body)

	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
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

func (s *Server) forwardAndWaitForResponse(ctx context.Context, w http.ResponseWriter, connection *Connection, requestMsg *common.Message, domain string) {
	s.logForwardingRequest(requestMsg, domain)

	responseChan, cancelCleanup, err := s.registerAndForwardRequest(ctx, connection, requestMsg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer cancelCleanup()
	defer s.cleanupRequest(domain, requestMsg.UUID)
	s.handleResponse(ctx, w, responseChan)
}

func (s *Server) registerAndForwardRequest(ctx context.Context, connection *Connection, requestMsg *common.Message) (chan *common.Message, context.CancelFunc, error) {
	responseChan, cancelCleanup := connection.RegisterRequest(ctx, requestMsg.UUID)
	if err := connection.SendMessage(requestMsg); err != nil {
		connection.CleanupRequest(requestMsg.UUID)
		return nil, cancelCleanup, err
	}

	return responseChan, cancelCleanup, nil
}

func (s *Server) writeSuccessResponse(w http.ResponseWriter, responseMsg *common.Message) {
	common.CopyHTTPHeaders(responseMsg.Headers, w.Header())
	w.WriteHeader(responseMsg.Status)
	if _, err := w.Write(responseMsg.Body); err != nil {
		s.Logger.Debug("failed to write response body", "error", err)
	}
}

func (s *Server) writeTimeoutResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusRequestTimeout)
	if _, err := w.Write([]byte("Request timeout")); err != nil {
		s.Logger.Debug("failed to write timeout response body", "error", err)
	}
}

func (s *Server) handleResponse(ctx context.Context, w http.ResponseWriter, responseChan chan *common.Message) {
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
	s.requestLogger.LogHTTPRequest(
		domain,
		requestMsg.UUID,
		requestMsg.Method,
		requestMsg.URL,
		requestMsg.Headers,
		requestMsg.Body,
	)
}
