package client

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/dbackowski/wormhole/common"
)

func (c *Client) buildResponseMessage(message common.Message, status int, body []byte, headers http.Header) common.Message {
	return common.Message{
		Type:    common.MessageTypeHTTPResponse,
		Domain:  c.Domain,
		UUID:    message.UUID,
		Method:  message.Method,
		URL:     message.URL,
		Status:  status,
		Body:    body,
		Headers: headers,
	}
}

func (c *Client) makeLocalRequest(message common.Message, localURL string) (*http.Response, error) {
	req, err := http.NewRequest(message.Method, localURL, bytes.NewReader(message.Body))
	if err != nil {
		return nil, err
	}

	if hosts, ok := message.Headers["Host"]; ok && len(hosts) > 0 {
		req.Host = hosts[0]
	}

	common.CopyHTTPHeaders(message.Headers, req.Header)
	return c.HTTPClient.Do(req)
}

func (c *Client) forwardResponse(message common.Message, res *http.Response) error {
	resBody, err := io.ReadAll(res.Body)

	if err != nil {
		return fmt.Errorf("could not read response body: %w", err)
	}

	responseMsg := c.buildResponseMessage(message, res.StatusCode, resBody, res.Header)
	return c.Conn.WriteJSON(responseMsg)
}

func (c *Client) respondToServer(message common.Message, res *http.Response, err error) error {
	if err != nil {
		c.sendErrorResponse(message, http.StatusBadGateway, http.StatusText(http.StatusBadGateway))
		return fmt.Errorf("local request failed: %w", err)
	}
	defer res.Body.Close()

	if err = c.forwardResponse(message, res); err != nil {
		return fmt.Errorf("failed to forward response to server: %w", err)
	}

	return nil
}

func (c *Client) sendErrorResponse(message common.Message, status int, errorMessage string) {
	responseMsg := c.buildResponseMessage(message, status, []byte(errorMessage), nil)
	err := c.Conn.WriteJSON(responseMsg)

	if err != nil {
		log.Printf("Failed to send error response: %v", err)
	}
}

func (c *Client) handleServerHTTPRequest(message common.Message, localURL string) {
	res, err := c.makeLocalRequest(message, localURL)

	statusCode := http.StatusBadGateway

	if err == nil {
		statusCode = res.StatusCode
	}

	if err := c.respondToServer(message, res, err); err != nil {
		log.Printf("Request failed: %v", err)
		c.logResponse(message, localURL, http.StatusBadGateway)
	} else {
		c.logResponse(message, localURL, statusCode)
	}

	c.printSummary()
}
