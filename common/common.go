package common

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	DefaultServerPort      = 8080
	DefaultClientServerURL = "http://localhost:8080"
	TimeFormat             = "2006-01-02 15:04:05"
	RequestTimeout         = 10 * time.Second
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
	jsonMessage, err := json.MarshalIndent(clone, "", "  ")

	if err != nil {
		fmt.Printf("Failed to marshal message: %v\n", err)
		return
	}

	fmt.Println(string(jsonMessage))
}

func ClearTerminal() {
	fmt.Printf("\x1bc")
}

func FormatTime(t time.Time) string {
	return t.Format(TimeFormat)
}
