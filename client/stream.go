package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/dbackowski/wormhole/common"
)

// dispatchUpgradeRequest opens the stream before returning to the message
// loop, so a stream_close the server sends right behind the request finds it.
func (c *Client) dispatchUpgradeRequest(msg *common.Message) error {
	stream, err := c.streams.Open(msg.UUID)
	if err != nil {
		if err := c.sendResponse(msg, ProxyResponse{
			StatusCode: http.StatusServiceUnavailable,
			Body:       []byte(http.StatusText(http.StatusServiceUnavailable)),
		}); err != nil {
			return fmt.Errorf("failed to send 503 for %s: %w", msg.UUID, err)
		}
		return nil
	}

	go func() {
		if err := c.handleUpgradeRequest(msg, stream); err != nil && c.Logger != nil {
			c.Logger.Error("WebSocket handling failed", "uuid", msg.UUID, "error", err)
		}
	}()
	return nil
}

func (c *Client) handleUpgradeRequest(msg *common.Message, stream *common.Stream) error {
	// A no-op once Pipe has run, which removes the stream itself.
	defer c.streams.Abort(msg.UUID)

	httpResp, err := c.proxy.Upgrade(NewProxyRequest(msg))
	if err != nil || httpResp.StatusCode != http.StatusSwitchingProtocols {
		var proxyResp *ProxyResponse
		if err == nil {
			proxyResp, err = c.proxy.readResponse(httpResp)
		}
		resolved := resolveProxyResponse(proxyResp, err)
		if sendErr := c.sendResponse(msg, resolved); sendErr != nil {
			return sendErr
		}
		c.recordRequest(msg, resolved, err, StreamInfo{})
		return nil
	}

	conn, ok := httpResp.Body.(io.ReadWriteCloser)
	if !ok {
		httpResp.Body.Close()
		resolved := resolveProxyResponse(nil, errors.New("upgraded connection is not writable"))
		return c.sendResponse(msg, resolved)
	}

	// Sent as is: the browser needs Upgrade, Connection and the
	// Sec-WebSocket-* headers that a plain response would drop as hop-by-hop.
	resolved := ProxyResponse{StatusCode: httpResp.StatusCode, Headers: httpResp.Header}
	if err := c.sendResponse(msg, resolved); err != nil {
		conn.Close()
		return err
	}
	c.recordRequest(msg, resolved, nil, StreamInfo{Open: true})

	tapped := &tapConn{
		ReadWriteCloser: conn,
		onRead:          c.streamRecorder(msg.UUID, true),
		onWrite:         c.streamRecorder(msg.UUID, false),
	}
	c.streams.Pipe(msg.UUID, stream, tapped, func(m *common.Message) error {
		return c.safeWriteJSON(m)
	})

	c.history.Update(msg.UUID, func(log *RequestLog) {
		log.Stream.Open = false
		log.Stream.ClosedAt = time.Now()
	})
	c.RefreshTerminalOutput()
	return nil
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

func (t *tapConn) Write(p []byte) (int, error) {
	n, err := t.ReadWriteCloser.Write(p)
	if n > 0 {
		t.onWrite(p[:n])
	}
	return n, err
}
