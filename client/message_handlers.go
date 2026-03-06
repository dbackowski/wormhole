package client

import (
	"fmt"
	"time"

	"github.com/dbackowski/wormhole/common"
)

func (c *Client) handleDomainRegistered(_ *common.Message) error {
	c.RefreshTerminalOutput()
	return nil
}

func (c *Client) handleDomainTaken(_ *common.Message) error {
	fmt.Println("Domain is already taken. Please choose another one.")

	if err := closeWebsocket(c.Conn); err != nil {
		return fmt.Errorf("failed to close websocket after domain taken: %w", err)
	}

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

func (c *Client) setupMessageHandlers() {
	c.dispatcher = common.NewMessageDispatcher()

	c.dispatcher.Register(common.MessageTypeDomainRegistered, c.handleDomainRegistered)
	c.dispatcher.Register(common.MessageTypeDomainTaken, c.handleDomainTaken)
	c.dispatcher.Register(common.MessageTypeHTTPRequest, c.handleHTTPRequest)
}
