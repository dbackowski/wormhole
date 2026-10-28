package client

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dbackowski/wormhole/common"
)

func (c *Client) handleHTTPRequest(msg *common.Message) error {
	start := time.Now()
	proxyResp, forwardErr := c.proxy.Forward(msg)
	duration := time.Since(start)
	resolved := resolveProxyResponse(proxyResp, forwardErr)

	if err := c.sendResponse(msg, resolved); err != nil {
		return fmt.Errorf("failed to send response for %s: %w", msg.UUID, err)
	}

	c.recordRequest(msg, resolved, forwardErr, StreamInfo{}, duration)
	return nil
}

func (c *Client) recordRequest(msg *common.Message, resolved ProxyResponse, forwardErr error, stream StreamInfo, duration time.Duration) {
	errText := ""
	if forwardErr != nil {
		errText = forwardErr.Error()
	}

	c.history.Add(RequestLog{
		UUID:            msg.UUID,
		Timestamp:       time.Now(),
		Method:          msg.Method,
		URL:             msg.URL,
		StatusCode:      resolved.StatusCode,
		RequestHeaders:  msg.Headers,
		RequestBody:     msg.Body,
		ResponseHeaders: resolved.Headers,
		ResponseBody:    resolved.Body,
		Error:           errText,
		Duration:        duration,
		Stream:          stream,
	})

	c.requestRedraw()
}

func (c *Client) dispatchHTTPRequest(msg *common.Message) error {
	select {
	case c.requestSem <- struct{}{}:
		go func() {
			defer func() { <-c.requestSem }()
			c.handleHTTPRequest(msg) //nolint:errcheck // nowhere to report it, see HandleConnection
		}()
	default:
		if err := c.sendResponse(msg, ProxyResponse{
			StatusCode: http.StatusServiceUnavailable,
			Body:       []byte(http.StatusText(http.StatusServiceUnavailable)),
		}); err != nil {
			return fmt.Errorf("failed to send 503 for %s: %w", msg.UUID, err)
		}
	}
	return nil
}

func (c *Client) setupMessageHandlers() {
	if c.requestSem == nil {
		c.requestSem = make(chan struct{}, MaxConcurrentRequests)
	}
	if c.streams == nil {
		c.streams = common.NewStreams()
	}

	c.handlers = common.Handlers{
		common.MessageTypeHTTPRequest:    c.dispatchHTTPRequest,
		common.MessageTypeUpgradeRequest: c.dispatchUpgradeRequest,
		common.MessageTypeStreamData: func(msg *common.Message) error {
			c.streams.Deliver(msg.UUID, msg.Body)
			return nil
		},
		common.MessageTypeStreamClose: func(msg *common.Message) error {
			c.streams.Finish(msg.UUID)
			return nil
		},
	}
}
