package client

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dbackowski/wormhole/common"
)

func (c *Client) handleDomainRegistered(msg *common.Message) {
	c.RefreshTerminalOutput()
}

func (c *Client) handleDomainTaken(msg *common.Message) {
	fmt.Println("Domain is already taken. Please choose another one.")
	err := closeWebsocket(c.Conn)
	if err != nil {
		c.Logger.Error("Failed to close websocket", "error", err)
	}
}

func (c *Client) handleHTTPRequest(msg *common.Message) {
	proxyResp, err := c.proxy.Forward(buildProxyRequest(msg))

	statusCode := http.StatusBadGateway

	if proxyResp != nil {
		statusCode = proxyResp.StatusCode
	}

	c.history.Add(RequestLog{
		Timestamp:  time.Now(),
		Method:     msg.Method,
		URL:        msg.URL,
		StatusCode: statusCode,
	})

	if sendErr := c.sendResponse(msg, proxyResp, err); sendErr != nil {
		c.Logger.Error("Failed to send response", "error", sendErr)
		c.RefreshTerminalOutput()
		return
	}

	c.RefreshTerminalOutput()
}

func (c *Client) setupMessageHandlers() {
	c.dispatcher = common.NewMessageDispatcher()

	c.dispatcher.Register(common.MessageTypeDomainRegistered, c.handleDomainRegistered)
	c.dispatcher.Register(common.MessageTypeDomainTaken, c.handleDomainTaken)
	c.dispatcher.Register(common.MessageTypeHTTPRequest, c.handleHTTPRequest)
}
