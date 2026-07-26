package client

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dbackowski/wormhole/common"
)

func (c *Client) handleDomainRegistered(_ *common.Message) error {
	c.RefreshTerminalOutput()
	return nil
}

func (c *Client) handleHTTPRequest(msg *common.Message) error {
	proxyResp, err := c.proxy.Forward(NewProxyRequest(msg))
	resolved := resolveProxyResponse(proxyResp, err)

	if err := c.sendResponse(msg, resolved); err != nil {
		return fmt.Errorf("failed to send response for %s: %w", msg.UUID, err)
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
	})

	c.RefreshTerminalOutput()
	return nil
}

func (c *Client) dispatchHTTPRequest(msg *common.Message) error {
	select {
	case c.requestSem <- struct{}{}:
		go func() {
			defer func() { <-c.requestSem }()
			if err := c.handleHTTPRequest(msg); err != nil && c.Logger != nil {
				c.Logger.Error("HTTP request handling failed", "uuid", msg.UUID, "error", err)
			}
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
	c.dispatcher = common.NewMessageDispatcher()
	if c.requestSem == nil {
		c.requestSem = make(chan struct{}, MaxConcurrentRequests)
	}

	c.dispatcher.Register(common.MessageTypeDomainRegistered, c.handleDomainRegistered)
	c.dispatcher.Register(common.MessageTypeHTTPRequest, c.dispatchHTTPRequest)
}
