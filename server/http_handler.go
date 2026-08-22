package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/dbackowski/wormhole/common"
	"github.com/google/uuid"
)

const (
	minValidHTTPStatus = 100
	maxValidHTTPStatus = 999
)

var errWebSocketUpgradeUnsupported = errors.New("WebSocket upgrade not supported")

func (s *Server) tunnelRequest(w http.ResponseWriter, r *http.Request) {
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

	requestMsg, err := s.buildRequestMessage(w, r)

	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		if errors.Is(err, errWebSocketUpgradeUnsupported) {
			s.Logger.Debug("Rejected WebSocket upgrade to tunnel (passthrough not supported)",
				"domain", domain, "remote_addr", r.RemoteAddr, "url", r.URL.String())
			http.Error(w, "WebSocket passthrough is not supported", http.StatusNotImplemented)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.forwardAndWaitForResponse(r.Context(), w, connection, requestMsg, domain)
}

func isWebSocketUpgradeRequest(headers http.Header) bool {
	return strings.EqualFold(headers.Get("Connection"), "upgrade") &&
		strings.EqualFold(headers.Get("Upgrade"), "websocket")
}

func (s *Server) prepareRequestHeaders(r *http.Request) map[string][]string {
	headers := http.Header{}

	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values
		}
	}

	common.RemoveHopByHopHeaders(headers)
	s.setForwardedHeaders(headers, r)
	headers["Host"] = []string{r.Host}

	return headers
}

func (s *Server) setForwardedHeaders(headers http.Header, r *http.Request) {
	if clientIP, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && clientIP != "" {
		if prior := headers.Get("X-Forwarded-For"); prior != "" {
			headers.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			headers.Set("X-Forwarded-For", clientIP)
		}
	}

	if headers.Get("X-Forwarded-Proto") == "" {
		headers.Set("X-Forwarded-Proto", s.forwardedProto(r))
	}

	if headers.Get("X-Forwarded-Host") == "" && r.Host != "" {
		headers.Set("X-Forwarded-Host", r.Host)
	}
}

func (s *Server) forwardedProto(r *http.Request) string {
	if r.TLS != nil || s.host != "" {
		return "https"
	}
	return "http"
}

func (s *Server) buildRequestMessage(w http.ResponseWriter, r *http.Request) (*common.Message, error) {
	if isWebSocketUpgradeRequest(r.Header) {
		return nil, errWebSocketUpgradeUnsupported
	}

	defer r.Body.Close()
	reqBody, err := io.ReadAll(http.MaxBytesReader(w, r.Body, common.MaxRequestBodySize))

	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	return &common.Message{
		Type:    common.MessageTypeHTTPRequest,
		UUID:    uuid.New().String(),
		URL:     r.URL.String(),
		Method:  r.Method,
		Headers: s.prepareRequestHeaders(r),
		Body:    reqBody,
	}, nil
}

func (s *Server) forwardAndWaitForResponse(ctx context.Context, w http.ResponseWriter, connection *Connection, requestMsg *common.Message, domain string) {
	s.requestLogger.LogHTTPRequest(domain, requestMsg.UUID, requestMsg.Method, requestMsg.URL, requestMsg.Headers, requestMsg.Body)

	// cancelCleanup drops the pending entry and releases the timeout context, so
	// it is deferred at acquisition rather than handed to a caller that may skip
	// it on an early return.
	responseChan, cancelCleanup := connection.RegisterRequest(ctx, requestMsg.UUID)
	defer cancelCleanup()

	if err := connection.SendMessage(requestMsg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.handleResponse(ctx, w, connection, responseChan)
}

func (s *Server) writeResponse(w http.ResponseWriter, status int, body []byte) {
	if status < minValidHTTPStatus || status > maxValidHTTPStatus {
		s.Logger.Error("client returned invalid HTTP status", "status", status)
		clear(w.Header())
		status, body = http.StatusBadGateway, []byte("upstream returned invalid status")
	}

	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		s.Logger.Debug("failed to write response body", "error", err)
	}
}

func (s *Server) writeSuccessResponse(w http.ResponseWriter, responseMsg *common.Message) {
	common.CopyHTTPHeaders(responseMsg.Headers, w.Header())
	s.writeResponse(w, responseMsg.Status, responseMsg.Body)
}

func (s *Server) writeTimeoutResponse(w http.ResponseWriter) {
	s.writeResponse(w, http.StatusRequestTimeout, []byte("Request timeout"))
}

func (s *Server) writeDisconnectedResponse(w http.ResponseWriter) {
	s.writeResponse(w, http.StatusBadGateway, []byte("tunnel client disconnected"))
}

func (s *Server) handleResponse(ctx context.Context, w http.ResponseWriter, connection *Connection, responseChan chan *common.Message) {
	select {
	case responseMsg, ok := <-responseChan:
		if !ok || responseMsg == nil {
			s.writeTimeoutResponse(w)
			return
		}
		s.writeSuccessResponse(w, responseMsg)
	case <-connection.Done():
		// Both cases can be ready at once when a response lands just before the
		// tunnel closes, and select would pick at random. Prefer the response.
		select {
		case responseMsg, ok := <-responseChan:
			if ok && responseMsg != nil {
				s.writeSuccessResponse(w, responseMsg)
				return
			}
		default:
		}
		// No response can ever arrive now. Answer with the same status a request
		// that arrived after the disconnect would get.
		s.writeDisconnectedResponse(w)
	case <-ctx.Done():
		s.writeTimeoutResponse(w)
	}
}
