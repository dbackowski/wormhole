package client

import (
	"fmt"

	"github.com/dbackowski/wormhole/common"
)

func (c *Client) handleDomainRegistered() {
	c.printSummary()
}

func (c *Client) handleDomainTaken() {
	fmt.Println("Domain is already taken. Please choose another one.")
	closeWebsocket(c.Conn)
}

func (c *Client) handleHTTPRequest(message common.Message) {
	localURL := c.buildLocalURL(message.URL)
	c.handleServerHTTPRequest(message, localURL)
}

func (c *Client) setupMessageHandlers() {
	c.dispatcher = common.NewMessageDispatcher()

	c.dispatcher.Register(common.MessageTypeDomainRegistered, func(msg *common.Message) {
		c.handleDomainRegistered()
	})

	c.dispatcher.Register(common.MessageTypeDomainTaken, func(msg *common.Message) {
		c.handleDomainTaken()
	})

	c.dispatcher.Register(common.MessageTypeHTTPRequest, func(msg *common.Message) {
		c.handleHTTPRequest(*msg)
	})
}
