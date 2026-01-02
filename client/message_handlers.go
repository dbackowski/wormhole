package client

import (
	"net/http"

	"github.com/dbackowski/wormhole/common"
)

func (c *Client) handleDomainRegistered(msg *common.Message) {
	c.printSummary()
}

func (c *Client) handleDomainTaken(msg *common.Message) {
	c.ui.Println("Domain is already taken. Please choose another one.")
	err := closeWebsocket(c.Conn)
	if err != nil {
		c.Logger.Error("Failed to close websocket", "error", err)
	}
}

func (c *Client) handleHTTPRequest(msg *common.Message) {
	localURL, err := c.buildLocalURL(msg.URL)
	if err != nil {
		c.sendErrorResponse(*msg, http.StatusBadRequest, "Invalid request URL")
		return
	}

	c.handleServerHTTPRequest(*msg, localURL)
}

func (c *Client) setupMessageHandlers() {
	c.dispatcher = common.NewMessageDispatcher()

	c.dispatcher.Register(common.MessageTypeDomainRegistered, c.handleDomainRegistered)
	c.dispatcher.Register(common.MessageTypeDomainTaken, c.handleDomainTaken)
	c.dispatcher.Register(common.MessageTypeHTTPRequest, c.handleHTTPRequest)
}
