package client

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/dbackowski/wormhole/common"
)

// dispatchUpgradeRequest opens the stream before returning to the message
// loop, so a stream_reset the server sends right behind the request finds it.
func (c *Client) dispatchUpgradeRequest(msg *common.Message) error {
	stream, err := c.streams.Open(msg.UUID, c.safeWriteJSON)
	if err != nil {
		return c.sendWholeResponse(msg.UUID, http.StatusServiceUnavailable)
	}

	go c.handleUpgradeRequest(msg, stream)
	return nil
}

func (c *Client) handleUpgradeRequest(msg *common.Message, stream *common.Stream) {
	// A no-op once Pipe has run, which removes the stream itself.
	defer c.streams.Abort(msg.UUID)

	start := time.Now()
	httpResp, err := c.proxy.Upgrade(msg)
	if err != nil {
		c.relayError(msg, nil, err, start)
		return
	}
	if httpResp.StatusCode != http.StatusSwitchingProtocols {
		common.RemoveHopByHopHeaders(httpResp.Header)
		c.relayResponse(msg, stream, httpResp, nil, start)
		return
	}

	duration := time.Since(start)

	conn, ok := httpResp.Body.(io.ReadWriteCloser)
	if !ok {
		httpResp.Body.Close()
		c.relayError(msg, nil, errors.New("upgraded connection is not writable"), start)
		return
	}

	// Sent as is: the browser needs Upgrade, Connection and the
	// Sec-WebSocket-* headers that a plain response would drop as hop-by-hop.
	if err := c.safeWriteJSON(&common.Message{
		Type:    common.MessageTypeHTTPResponse,
		UUID:    msg.UUID,
		Status:  httpResp.StatusCode,
		Headers: httpResp.Header,
	}); err != nil {
		conn.Close()
		return
	}
	c.recordRequest(msg, httpResp.StatusCode, httpResp.Header, nil, StreamInfo{Open: true}, duration)

	tapped := &tapConn{
		ReadWriteCloser: conn,
		onRead:          c.streamRecorder(msg.UUID, true),
		onWrite:         c.streamRecorder(msg.UUID, false),
	}
	c.streams.Pipe(msg.UUID, stream, tapped)

	c.history.Update(msg.UUID, func(log *RequestLog) {
		log.Stream.Open = false
		log.Stream.ClosedAt = time.Now()
	})
	c.requestRedraw()
}

// streamRecorder returns a tap that counts the bytes going one way through a
// WebSocket and records the messages they carry.
func (c *Client) streamRecorder(uuid string, fromApp bool) func([]byte) {
	parser := &frameParser{}

	return func(data []byte) {
		msgs := parser.feed(data)
		now := time.Now()

		c.history.Update(uuid, func(log *RequestLog) {
			if fromApp {
				log.Stream.BytesFromApp += int64(len(data))
			} else {
				log.Stream.BytesToApp += int64(len(data))
			}
			for _, m := range msgs {
				m.Timestamp, m.FromApp = now, fromApp
				log.Messages = append(log.Messages, m)
			}
			log.Stream.Messages += len(msgs)
			if n := len(log.Messages) - MaxStoredMessages; n > 0 {
				log.Messages = slices.Delete(log.Messages, 0, n)
			}
		})
	}
}

// tapConn shows every byte read from or written to the local app to a
// callback, unchanged. Read is the app sending, Write is the app receiving.
type tapConn struct {
	io.ReadWriteCloser
	onRead, onWrite func([]byte)
}

func (t *tapConn) Read(p []byte) (int, error) {
	n, err := t.ReadWriteCloser.Read(p)
	if n > 0 {
		t.onRead(p[:n])
	}
	return n, err
}

// Write records before writing: once the app has the bytes it can reply, and
// the reply must not land in the history ahead of the message it answers.
func (t *tapConn) Write(p []byte) (int, error) {
	t.onWrite(p)
	return t.ReadWriteCloser.Write(p)
}
