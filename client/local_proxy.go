package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/dbackowski/wormhole/common"
)

type ProxyRequest struct {
	Method  string
	URL     string
	Headers map[string][]string
	Body    []byte
}

type ProxyResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}

type LocalProxy struct {
	httpClient *http.Client
	baseURL    string
}

func NewLocalProxy(baseURL string, timeout time.Duration) *LocalProxy {
	return &LocalProxy{
		baseURL: baseURL,
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
	httpReq, err := http.NewRequest(req.Method, lp.buildURL(req.URL), bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

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

func (lp *LocalProxy) buildURL(path string) string {
	u, _ := url.Parse(lp.baseURL)
	u.Path = filepath.Join(u.Path, path)
	return u.String()
}
