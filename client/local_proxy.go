package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/dbackowski/wormhole/common"
)

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
	httpClient *http.Client
	baseURL    url.URL
	tunnelURL  string
}

func NewLocalProxy(baseURL url.URL, tunnelURL string, timeout time.Duration) *LocalProxy {
	return &LocalProxy{
		baseURL:   baseURL,
		tunnelURL: tunnelURL,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (lp *LocalProxy) Forward(req ProxyRequest) (*ProxyResponse, error) {
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

	httpResp, err := lp.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &ProxyResponse{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header,
		Body:       body,
	}, nil
}
