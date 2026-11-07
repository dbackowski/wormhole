package server

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

const (
	minValidHTTPStatus = 100
	maxValidHTTPStatus = 999
)

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
	s.requests.Add(1)
	connection.stats.served.Add(1)

	if websocket.IsWebSocketUpgrade(r) {
		s.tunnelUpgrade(w, r, connection, domain)
		return
	}

	s.tunnelHTTP(w, r, connection, domain)
}

// tunnelHTTP forwards r to the client and streams both bodies through the
// tunnel, so neither is held whole in memory and a response such as
// Server-Sent Events reaches the browser as the local app writes it.
func (s *Server) tunnelHTTP(w http.ResponseWriter, r *http.Request, connection *Connection, domain string) {
	id := rand.Text()
	st, err := connection.bodies.Open(id, connection.SendMessage)
	if err != nil {
		http.Error(w, "too many concurrent requests", http.StatusServiceUnavailable)
		return
	}

	requestMsg := &common.Message{
		Type:    common.MessageTypeHTTPRequest,
		UUID:    id,
		URL:     r.URL.String(),
		Method:  r.Method,
		Headers: s.prepareRequestHeaders(r),
	}
	s.logRequest(domain, requestMsg)

	responseChan, stopWaiting := connection.requests.Register(id)
	defer stopWaiting()

	if err := connection.SendMessage(requestMsg); err != nil {
		connection.bodies.Abort(id)
		s.Logger.Debug("failed to forward request to tunnel client", "domain", domain, "uuid", id, "error", err)
		s.writeDisconnectedResponse(w)
		return
	}

	// Lets the client cancel the local request: the browser left, the upload
	// broke off, or the response could not be written.
	reset := func() {
		if connection.bodies.Abort(id) {
			connection.SendMessage(&common.Message{Type: common.MessageTypeStreamReset, UUID: id}) //nolint:errcheck // the tunnel may be gone
		}
	}
	// An idle response, such as an event stream, notices the browser leaving
	// only through the context.
	defer context.AfterFunc(r.Context(), reset)()

	rc := http.NewResponseController(w)
	// HTTP/1 handlers may otherwise not read the upload once the response has
	// started, and a local app is free to answer before reading all of it.
	rc.EnableFullDuplex() //nolint:errcheck // HTTP/2 is full duplex anyway

	bodySent := make(chan struct{})
	uploadFailed := false // set before bodySent closes
	go func() {
		defer close(bodySent)
		err := st.Send(idleReader{r.Body, rc, s.bodyReadTimeout})
		// Not a stream error: the browser stalled or broke off the upload.
		uploadFailed = err != nil && !errors.Is(err, common.ErrStreamClosed) && !errors.Is(err, common.ErrStreamReset)
		// A read deadline left behind would cut off a long response: the
		// server cancels the request once a read of the idle connection fails.
		rc.SetReadDeadline(time.Time{}) //nolint:errcheck
	}()

	complete := false
	defer func() {
		select {
		case <-bodySent:
		default:
			// The response ended before the upload: unblock the read in progress.
			rc.SetReadDeadline(time.Now()) //nolint:errcheck
		}
		if !complete {
			reset()
		}
		connection.bodies.Abort(id) // stops a Send waiting for credit
		<-bodySent
	}()

	responseMsg := s.awaitResponse(r.Context(), w, connection, responseChan, bodySent, func() bool { return uploadFailed })
	if responseMsg != nil {
		complete = s.writeStreamedResponse(w, rc, responseMsg, st)
	}
}

// idleReader gives each read of an upload its own deadline, so a large body
// may take as long as it needs while a stalled one is still cut off.
type idleReader struct {
	body    io.Reader
	rc      *http.ResponseController
	timeout time.Duration
}

func (ir idleReader) Read(p []byte) (int, error) {
	ir.rc.SetReadDeadline(time.Now().Add(ir.timeout)) //nolint:errcheck // nothing to extend where unsupported
	return ir.body.Read(p)
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

	// How the client learns whether a body follows and how long it is.
	// Transfer-Encoding went with the hop-by-hop headers.
	delete(headers, "Content-Length")
	switch {
	case r.ContentLength > 0:
		headers["Content-Length"] = []string{strconv.FormatInt(r.ContentLength, 10)}
	case r.ContentLength < 0:
		headers["Transfer-Encoding"] = []string{"chunked"}
	}

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

	// Always overwritten: routing already trusts r.Host, while an inbound value
	// is whatever the visitor sent, and apps build absolute URLs from it.
	if r.Host != "" {
		headers.Set("X-Forwarded-Host", r.Host)
	}
}

func (s *Server) forwardedProto(r *http.Request) string {
	if r.TLS != nil || s.host != "" {
		return "https"
	}
	return "http"
}

// logRequest leaves out the query string, header values and the body: on a
// shared server they carry visitors' tokens, cookies and passwords. The tunnel
// owner sees them in the client's Web UI.
func (s *Server) logRequest(domain string, msg *common.Message) {
	path, _, _ := strings.Cut(msg.URL, "?")
	s.Logger.Info("HTTP request forwarded", "domain", domain, "uuid", msg.UUID, "method", msg.Method, "path", path)
	s.Logger.Debug("HTTP request details", "uuid", msg.UUID, "headers", slices.Sorted(maps.Keys(msg.Headers)))
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

// writeStreamedResponse writes the client's response headers, then the body
// as it arrives on body, and reports whether the body was complete. A body cut
// short aborts the response, so the browser cannot take it for a whole one.
func (s *Server) writeStreamedResponse(w http.ResponseWriter, rc *http.ResponseController, responseMsg *common.Message, body io.Reader) bool {
	if responseMsg.Status < minValidHTTPStatus || responseMsg.Status > maxValidHTTPStatus {
		s.writeResponse(w, responseMsg.Status, nil)
		return false
	}

	common.CopyHTTPHeaders(responseMsg.Headers, w.Header())
	// The request's deadline may have passed during a long upload, and each
	// write gets its own, so a long download is not cut off either.
	rc.SetWriteDeadline(time.Now().Add(s.writeTimeout)) //nolint:errcheck
	w.WriteHeader(responseMsg.Status)
	rc.Flush() //nolint:errcheck // headers go out before a slow first event

	buf := make([]byte, common.StreamChunkSize)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			rc.SetWriteDeadline(time.Now().Add(s.writeTimeout)) //nolint:errcheck
			if _, werr := w.Write(buf[:n]); werr != nil {
				return false
			}
			rc.Flush() //nolint:errcheck
		}
		if err == io.EOF {
			return true
		}
		if err != nil {
			panic(http.ErrAbortHandler)
		}
	}
}

func (s *Server) writeTimeoutResponse(w http.ResponseWriter) {
	s.tunnelErrors.Add(1)
	s.writeResponse(w, http.StatusGatewayTimeout, []byte("Gateway timeout"))
}

func (s *Server) writeDisconnectedResponse(w http.ResponseWriter) {
	s.tunnelErrors.Add(1)
	s.writeResponse(w, http.StatusBadGateway, []byte("tunnel client disconnected"))
}

// awaitResponse returns the client's response headers, or nil once it has
// answered w itself with a timeout or disconnect. The client gets
// s.responseTimeout from the moment bodySent closes, so a long upload does
// not count against it. An upload that failed, which uploadFailed reports once
// bodySent has closed, is answered with 400.
func (s *Server) awaitResponse(ctx context.Context, w http.ResponseWriter, connection *Connection, responseChan chan *common.Message, bodySent <-chan struct{}, uploadFailed func() bool) *common.Message {
	var timeout <-chan time.Time
	for {
		select {
		case <-bodySent:
			if uploadFailed() {
				http.Error(w, "failed to read request body", http.StatusBadRequest)
				return nil
			}
			bodySent = nil
			timer := time.NewTimer(s.responseTimeout)
			defer timer.Stop()
			timeout = timer.C
		case responseMsg := <-responseChan:
			return responseMsg
		case <-timeout:
			s.writeTimeoutResponse(w)
			return nil
		case <-connection.Done():
			// Both cases can be ready at once when a response lands just before the
			// tunnel closes, and select would pick at random. Prefer the response.
			select {
			case responseMsg := <-responseChan:
				return responseMsg
			default:
			}
			// No response can ever arrive now. Answer with the same status a request
			// that arrived after the disconnect would get.
			s.writeDisconnectedResponse(w)
			return nil
		case <-ctx.Done():
			// A failed upload cancels the request too, and the browser is not
			// necessarily gone: report the failure rather than a timeout. The
			// upload ends at once here, its stream reset by tunnelHTTP.
			if bodySent != nil {
				<-bodySent
				if uploadFailed() {
					http.Error(w, "failed to read request body", http.StatusBadRequest)
					return nil
				}
			}
			s.writeTimeoutResponse(w)
			return nil
		}
	}
}
