package common

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Message struct {
	Type    string              `json:"type"`
	Domain  string              `json:"domain,omitempty"`
	UUID    string              `json:"uuid"`
	Method  string              `json:"method,omitempty"`
	URL     string              `json:"url,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
	Status  int                 `json:"status,omitempty"`
}

const (
	MessageTypeHTTPRequest      = "http_request"
	MessageTypeHTTPResponse     = "http_response"
	MessageTypeDomainTaken      = "domain_taken"
	MessageTypeDomainRegistered = "domain_registered"
)

func CopyHeaders(src http.Header, dest http.Header) {
	for key, values := range src {
		for _, value := range values {
			dest.Add(key, value)
		}
	}
}

func PrettyPrintMessage(msg Message) {
	clone := msg
	clone.Body = nil
	jsonMessage, _ := json.MarshalIndent(clone, "", "  ")
	fmt.Println(string(jsonMessage))
}
