package client

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dbackowski/wormhole/common"
)

func (c *Client) handleDomainRegistered(msg *common.Message) error {
	c.RefreshTerminalOutput()
	return nil
}

func (c *Client) handleDomainTaken(msg *common.Message) error {
	fmt.Println("Domain is already taken. Please choose another one.")

	if err := closeWebsocket(c.Conn); err != nil {
		return fmt.Errorf("failed to close websocket after domain taken: %w", err)
	}

	return nil
}

func (c *Client) handleHTTPRequest(msg *common.Message) error {
	proxyResp, err := c.proxy.Forward(buildProxyRequest(msg))

	var statusCode int

	if err != nil {
		statusCode = http.StatusBadGateway
	} else {
		statusCode = proxyResp.StatusCode
	}

	if err := c.sendResponse(msg, proxyResp, err); err != nil {
		return fmt.Errorf("failed to send response for %s: %w", msg.UUID, err)
	}

	c.history.Add(RequestLog{
		Timestamp:  time.Now(),
		Method:     msg.Method,
		URL:        msg.URL,
		StatusCode: statusCode,
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
