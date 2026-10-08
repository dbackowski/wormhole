package server

import (
	"io"
	"net/http"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/google/uuid"
)

// tunnelUpgrade forwards a WebSocket handshake to the client and, once the
// local app accepts it, relays the raw connection over the tunnel. The bytes
// are not interpreted, so subprotocols, pings and close codes are negotiated
// end to end between the browser and the app.
func (s *Server) tunnelUpgrade(w http.ResponseWriter, r *http.Request, connection *Connection, domain string) {
	headers := s.prepareRequestHeaders(r)
	// Dropped as hop-by-hop by prepareRequestHeaders, but the handshake needs them.
	headers["Connection"] = []string{"Upgrade"}
	headers["Upgrade"] = []string{"websocket"}

	requestMsg := &common.Message{
		Type:    common.MessageTypeUpgradeRequest,
		UUID:    uuid.New().String(),
		URL:     r.URL.String(),
		Method:  r.Method,
		Headers: headers,
	}
	id := requestMsg.UUID

	// Opened before the request is sent: the client may relay the app's first
	// frames right behind its 101, before w is hijacked.
	stream, err := connection.streams.Open(id)
	if err != nil {
		http.Error(w, "too many open WebSocket connections", http.StatusServiceUnavailable)
		return
	}

	// Until Pipe takes over, any exit leaves the client holding a stream (or a
	// handshake in flight) for this UUID. Pipe removes the stream itself, so
	// this is a no-op once it has run.
	defer func() {
		if connection.streams.Abort(id) {
			connection.SendMessage(&common.Message{Type: common.MessageTypeStreamClose, UUID: id}) //nolint:errcheck
		}
	}()

	s.requestLogger.LogHTTPRequest(domain, id, requestMsg.Method, requestMsg.URL, requestMsg.Headers, nil)

	responseChan, cancelCleanup := connection.RegisterRequest(r.Context(), id)
	defer cancelCleanup()

	if err := connection.SendMessage(requestMsg); err != nil {
		s.Logger.Debug("failed to forward upgrade to tunnel client", "domain", domain, "uuid", id, "error", err)
		s.writeDisconnectedResponse(w)
		return
	}

	responseMsg := s.awaitResponse(r.Context(), w, connection, responseChan)
	if responseMsg == nil {
		return
	}
	if responseMsg.Status != http.StatusSwitchingProtocols {
		s.writeSuccessResponse(w, responseMsg)
		return
	}

	conn, brw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		s.Logger.Error("WebSocket hijack failed", "domain", domain, "uuid", id, "error", err)
		http.Error(w, "WebSocket upgrade failed", http.StatusInternalServerError)
		return
	}

	// routeRequest's deadlines would otherwise cut the socket off mid-session.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return
	}

	// Written by hand: a 101 must not carry the Content-Length that
	// http.Response.Write would add. Header.Write drops invalid names and
	// strips CR/LF from values, so the client cannot inject lines.
	brw.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	http.Header(responseMsg.Headers).Write(brw) //nolint:errcheck // surfaced by Flush
	brw.WriteString("\r\n")
	if err := brw.Flush(); err != nil {
		conn.Close()
		return
	}

	// brw.Reader first: it may already hold frames the browser sent behind
	// the handshake.
	browser := struct {
		io.Reader
		io.Writer
		io.Closer
	}{brw.Reader, conn, conn}

	connection.streams.Pipe(id, stream, browser, connection.SendMessage)
	s.Logger.Info("WebSocket closed", "domain", domain, "uuid", id)
}
