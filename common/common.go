package common

import (
	"fmt"
	"net/http"
	"net/textproto"
	"strings"
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
	RequestTimeoutGrace      = 5 * time.Second
	RequestTimeoutBuffer     = RequestTimeout + RequestTimeoutGrace
	ClientRequestHistorySize = 50
	ClientTerminalMaxLogs    = 25
	ServerShutdownTimeout    = 10 * time.Second
	ClientShutdownTimeout    = 5 * time.Second
	ServerReadHeaderTimeout  = 10 * time.Second
	ServerIdleTimeout        = 120 * time.Second
	ServerBodyReadTimeout    = 30 * time.Second
	ServerWriteTimeout       = 60 * time.Second
	MaxRequestBodySize       = 10 << 20 // 10 MB
	MaxWebSocketMessageSize  = 16 << 20 // 16 MB
	PongWait                 = 30 * time.Second
	PingPeriod               = (PongWait * 9) / 10
	WriteWait                = 10 * time.Second
)

type Heartbeat struct {
	PongWait   time.Duration
	PingPeriod time.Duration
	WriteWait  time.Duration
}

func DefaultHeartbeat() Heartbeat {
	return Heartbeat{
		PongWait:   PongWait,
		PingPeriod: PingPeriod,
		WriteWait:  WriteWait,
	}
}

// ClientHeader marks a WebSocket upgrade as a tunnel client registering, rather
// than a browser opening a socket against a tunneled app. Browsers cannot set a
// request header on a WebSocket, so only a real client can send it.
const ClientHeader = "X-Wormhole-Client"

// SessionHeader carries a random secret a client generates once per process and
// sends on every dial. When a reconnect finds its domain still held by a
// connection registered with the same secret, that connection is a stale one
// from the same client (dropped uncleanly, heartbeat not yet expired), and the
// server replaces it instead of reporting the domain as taken.
const SessionHeader = "X-Wormhole-Session"

type MessageType string

const (
	MessageTypeHTTPRequest      MessageType = "http_request"
	MessageTypeHTTPResponse     MessageType = "http_response"
	MessageTypeDomainTaken      MessageType = "domain_taken"
	MessageTypeDomainRegistered MessageType = "domain_registered"
)

type Message struct {
	Type    MessageType         `json:"type"`
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

var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func RemoveHopByHopHeaders(h http.Header) {
	for _, f := range h["Connection"] {
		for sf := range strings.SplitSeq(f, ",") {
			if sf = textproto.TrimString(sf); sf != "" {
				h.Del(sf)
			}
		}
	}
	for _, k := range hopByHopHeaders {
		h.Del(k)
	}
}

func ValidatePort(port int) error {
	if port < MinValidPort || port > MaxValidPort {
		return fmt.Errorf("invalid port: %d (must be %d-%d)", port, MinValidPort, MaxValidPort)
	}
	return nil
}
