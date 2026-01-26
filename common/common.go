package common

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	MinValidPort             = 1
	MaxValidPort             = 65535
	DefaultServerPort        = 8080
	DefaultWebUIPort         = 4040
	DefaultClientServerURL   = "https://wormhole.tools"
	TimeFormat               = "2006-01-02 15:04:05"
	RequestTimeout           = 10 * time.Second
	RequestTimeoutBuffer     = RequestTimeout + 5*time.Second
	ClientRequestHistorySize = 50
	ClientTerminalMaxLogs    = 25
	ServerShutdownTimeout    = 10 * time.Second
)

type MessageType string

const (
	MessageTypeHTTPRequest      MessageType = "http_request"
	MessageTypeHTTPResponse     MessageType = "http_response"
	MessageTypeDomainTaken      MessageType = "domain_taken"
	MessageTypeDomainRegistered MessageType = "domain_registered"
)

type Message struct {
	Type    MessageType         `json:"type"`
	Domain  string              `json:"domain,omitempty"`
	UUID    string              `json:"uuid"`
	Method  string              `json:"method,omitempty"`
	URL     string              `json:"url,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
	Status  int                 `json:"status,omitempty"`
}

func CopyHTTPHeaders(src http.Header, dest http.Header) {
	for key, values := range src {
		if http.CanonicalHeaderKey(key) == "Host" {
			continue
		}
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
