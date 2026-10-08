package client

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/dbackowski/wormhole/common"
)

var ErrResponseTooLarge = errors.New("local server response body too large")

type ProxyRequest struct {
	Method  string
	URL     string
	Headers map[string][]string
	Body    []byte
}

func NewProxyRequest(msg *common.Message) ProxyRequest {
	return ProxyRequest{
		Method:  msg.Method,
		URL:     msg.URL,
		Headers: msg.Headers,
		Body:    msg.Body,
	}
}

type ProxyResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}

type LocalProxy struct {
	httpClient       *http.Client
	upgradeClient    *http.Client
	baseURL          url.URL
	tunnelURL        string
	maxResponseBytes int64
}

func NewLocalProxy(baseURL url.URL, tunnelURL string, timeout time.Duration) *LocalProxy {
	transport := &http.Transport{
		DisableKeepAlives: true,
		// Bound the handshake for upgrades, whose client has no overall Timeout.
		DialContext:           (&net.Dialer{Timeout: timeout}).DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
	}
	noRedirects := func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &LocalProxy{
		baseURL:          baseURL,
		tunnelURL:        tunnelURL,
		maxResponseBytes: common.MaxRequestBodySize,
		httpClient: &http.Client{
			Timeout:       timeout,
			Transport:     transport,
			CheckRedirect: noRedirects,
		},
		// No Timeout: it would also cut off the upgraded connection, which
		// lives for as long as the socket does.
		upgradeClient: &http.Client{
			Transport:     transport,
			CheckRedirect: noRedirects,
		},
	}
}

func (lp *LocalProxy) newRequest(req ProxyRequest) (*http.Request, error) {
	hostHeader, ok := req.Headers["Host"]
	if !ok || len(hostHeader) == 0 {
		return nil, fmt.Errorf("missing required Host header")
	}
	url, err := common.JoinURLPath(lp.baseURL, req.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to build URL: %w", err)
	}
	httpReq, err := http.NewRequest(req.Method, url, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	httpReq.Host = hostHeader[0]
	common.CopyHTTPHeaders(req.Headers, httpReq.Header)
	return httpReq, nil
}

func (lp *LocalProxy) Forward(req ProxyRequest) (*ProxyResponse, error) {
	httpReq, err := lp.newRequest(req)
	if err != nil {
		return nil, err
	}

	httpResp, err := lp.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return lp.readResponse(httpResp)
}

// Upgrade sends a WebSocket handshake to the local app. On 101 the response
// Body is the open connection, an io.ReadWriteCloser the caller must close;
// any other status is an ordinary response for readResponse.
func (lp *LocalProxy) Upgrade(req ProxyRequest) (*http.Response, error) {
	httpReq, err := lp.newRequest(req)
	if err != nil {
		return nil, err
	}

	// Without an extension the frames stay plain, so the web UI can show the
	// messages.
	// ponytail: disables permessage-deflate on every tunneled socket; inflate
	// in frameParser instead if the bandwidth matters.
	httpReq.Header.Del("Sec-WebSocket-Extensions")

	httpResp, err := lp.upgradeClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return httpResp, nil
}

func (lp *LocalProxy) readResponse(httpResp *http.Response) (*ProxyResponse, error) {
	defer httpResp.Body.Close()

	// Read one byte past the limit so an exactly-at-limit body is allowed while
	// anything larger is detected and rejected instead of buffered in full.
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, lp.maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(body)) > lp.maxResponseBytes {
		return nil, ErrResponseTooLarge
	}

	common.RemoveHopByHopHeaders(httpResp.Header)

	return &ProxyResponse{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header,
		Body:       body,
	}, nil
}
