package client

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dbackowski/wormhole/common"
)

type LocalProxy struct {
	client    *http.Client
	baseURL   url.URL
	tunnelURL string
}

func NewLocalProxy(baseURL url.URL, tunnelURL string, timeout time.Duration) *LocalProxy {
	return &LocalProxy{
		baseURL:   baseURL,
		tunnelURL: tunnelURL,
		// No overall Timeout: a body streams for as long as it runs, and an
		// event stream or an upgraded connection indefinitely. timeout bounds
		// connecting and waiting for the response headers.
		client: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives:     true,
				DialContext:           (&net.Dialer{Timeout: timeout}).DialContext,
				TLSHandshakeTimeout:   timeout,
				ResponseHeaderTimeout: timeout,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// newRequest builds the local request for req, with body as its body if the
// server said one follows: a Content-Length, or Transfer-Encoding chunked.
func (lp *LocalProxy) newRequest(ctx context.Context, req *common.Message, body io.Reader) (*http.Request, error) {
	hostHeader, ok := req.Headers["Host"]
	if !ok || len(hostHeader) == 0 {
		return nil, fmt.Errorf("missing required Host header")
	}
	url, err := common.JoinURLPath(lp.baseURL, req.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to build URL: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	httpReq.Host = hostHeader[0]
	common.CopyHTTPHeaders(req.Headers, httpReq.Header)

	headers := http.Header(req.Headers)
	length, _ := strconv.ParseInt(headers.Get("Content-Length"), 10, 64)
	chunked := strings.EqualFold(headers.Get("Transfer-Encoding"), "chunked")
	if body != nil && (length > 0 || chunked) {
		httpReq.Body = io.NopCloser(body)
		httpReq.ContentLength = length
		if chunked {
			httpReq.ContentLength = -1
		}
	}
	return httpReq, nil
}

// Forward sends req to the local app with body, and returns its response with
// the hop-by-hop headers removed. The caller must close the response Body.
func (lp *LocalProxy) Forward(ctx context.Context, req *common.Message, body io.Reader) (*http.Response, error) {
	httpReq, err := lp.newRequest(ctx, req, body)
	if err != nil {
		return nil, err
	}

	httpResp, err := lp.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	common.RemoveHopByHopHeaders(httpResp.Header)
	return httpResp, nil
}

// Upgrade sends a WebSocket handshake to the local app. On 101 the response
// Body is the open connection, an io.ReadWriteCloser the caller must close;
// any other status is an ordinary response, hop-by-hop headers included.
func (lp *LocalProxy) Upgrade(req *common.Message) (*http.Response, error) {
	// Not tied to the tunnel's stream: cancelling it could take the upgraded
	// connection down with it.
	httpReq, err := lp.newRequest(context.Background(), req, nil)
	if err != nil {
		return nil, err
	}

	// Without an extension the frames stay plain, so the web UI can show the
	// messages.
	// ponytail: disables permessage-deflate on every tunneled socket; inflate
	// in frameParser instead if the bandwidth matters.
	httpReq.Header.Del("Sec-WebSocket-Extensions")

	httpResp, err := lp.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return httpResp, nil
}
