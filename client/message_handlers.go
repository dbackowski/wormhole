package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/dbackowski/wormhole/common"
)

// dispatchHTTPRequest opens the request's stream before returning to the
// message loop, so the body chunks the server sends right behind it find it.
func (c *Client) dispatchHTTPRequest(msg *common.Message) error {
	select {
	case c.requestSem <- struct{}{}:
	default:
		return c.sendWholeResponse(msg.UUID, http.StatusServiceUnavailable)
	}

	st, err := c.bodies.Open(msg.UUID, c.safeWriteJSON)
	if err != nil {
		<-c.requestSem
		return c.sendWholeResponse(msg.UUID, http.StatusServiceUnavailable)
	}

	go func() {
		defer func() { <-c.requestSem }()
		c.handleHTTPRequest(msg, st)
	}()
	return nil
}

func (c *Client) handleHTTPRequest(msg *common.Message, st *common.Stream) {
	defer c.bodies.Abort(msg.UUID)

	// The server resets the stream when the browser leaves, which cancels the
	// local request.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-st.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	start := time.Now()
	reqBody := &bodyCapture{}
	resp, err := c.proxy.Forward(ctx, msg, io.TeeReader(st, reqBody))
	if err != nil {
		c.relayError(msg, reqBody, err, start)
		return
	}
	c.relayResponse(msg, st, resp, reqBody, start)
}

// relayResponse sends resp to the server, its body streamed on st, and
// records it: listed once its headers are out, so a long response shows while
// it runs, and completed when its body is.
func (c *Client) relayResponse(msg *common.Message, st *common.Stream, resp *http.Response, reqBody *bodyCapture, start time.Time) {
	defer resp.Body.Close()

	if err := c.safeWriteJSON(&common.Message{
		Type:    common.MessageTypeHTTPResponse,
		UUID:    msg.UUID,
		Status:  resp.StatusCode,
		Headers: resp.Header,
	}); err != nil {
		return
	}
	c.recordRequest(msg, resp.StatusCode, resp.Header, nil, StreamInfo{}, time.Since(start))

	respBody := &bodyCapture{}
	err := st.Send(io.TeeReader(resp.Body, respBody))
	if errors.Is(err, common.ErrStreamReset) {
		err = errors.New("browser closed the connection")
	} else if err != nil {
		err = errors.New("response cut off: " + err.Error())
	}
	c.finishRecord(msg.UUID, reqBody, respBody, err, time.Since(start))
}

// relayError answers a request the local app could not take with 502.
func (c *Client) relayError(msg *common.Message, reqBody *bodyCapture, err error, start time.Time) {
	c.sendWholeResponse(msg.UUID, http.StatusBadGateway) //nolint:errcheck // nowhere to report it, see HandleConnection
	c.recordRequest(msg, http.StatusBadGateway, nil, err, StreamInfo{}, time.Since(start))

	respBody := &bodyCapture{}
	respBody.Write([]byte(http.StatusText(http.StatusBadGateway))) //nolint:errcheck
	c.finishRecord(msg.UUID, reqBody, respBody, nil, time.Since(start))
}

// sendWholeResponse sends a response the client makes up itself, with the
// status text as its body.
func (c *Client) sendWholeResponse(uuid string, status int) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	for _, m := range []*common.Message{
		{Type: common.MessageTypeHTTPResponse, UUID: uuid, Status: status},
		{Type: common.MessageTypeStreamData, UUID: uuid, Body: []byte(http.StatusText(status))},
		{Type: common.MessageTypeStreamClose, UUID: uuid},
	} {
		if err := common.WriteJSON(c.Conn, m); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) recordRequest(msg *common.Message, status int, headers http.Header, err error, stream StreamInfo, duration time.Duration) {
	errText := ""
	if err != nil {
		errText = err.Error()
	}

	c.history.Add(RequestLog{
		UUID:            msg.UUID,
		Timestamp:       time.Now(),
		Method:          msg.Method,
		URL:             msg.URL,
		StatusCode:      status,
		RequestHeaders:  msg.Headers,
		ResponseHeaders: headers,
		Error:           errText,
		Duration:        duration,
		Stream:          stream,
	})

	c.requestRedraw()
}

// finishRecord adds the bodies to a recorded request once they have been
// relayed. reqBody is nil for a WebSocket handshake.
func (c *Client) finishRecord(uuid string, reqBody, respBody *bodyCapture, err error, duration time.Duration) {
	c.history.Update(uuid, func(log *RequestLog) {
		if reqBody != nil {
			log.RequestBody, log.RequestBodySize = reqBody.get()
		}
		log.ResponseBody, log.ResponseBodySize = respBody.get()
		log.Duration = duration
		if err != nil {
			log.Error = err.Error()
		}
	})
	c.requestRedraw()
}

// bodyCapture keeps the head of a body for the history, MaxStoredBodySize
// bytes at most, and counts all of it. The request body can still be read
// while the response is recorded, hence the lock.
type bodyCapture struct {
	mu   sync.Mutex
	head []byte
	size int
}

func (b *bodyCapture) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.size += len(p)
	if room := MaxStoredBodySize - len(b.head); room > 0 {
		b.head = append(b.head, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (b *bodyCapture) get() ([]byte, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.head), b.size
}

func (c *Client) setupMessageHandlers() {
	if c.requestSem == nil {
		c.requestSem = make(chan struct{}, MaxConcurrentRequests)
	}
	if c.streams == nil {
		c.streams = common.NewStreams(common.MaxStreamsPerTunnel)
	}
	if c.bodies == nil {
		c.bodies = common.NewStreams(MaxConcurrentRequests)
	}

	c.handlers = common.StreamHandlers(c.bodies, c.streams)
	maps.Copy(c.handlers, common.Handlers{
		common.MessageTypeHTTPRequest:    c.dispatchHTTPRequest,
		common.MessageTypeUpgradeRequest: c.dispatchUpgradeRequest,
	})
}
