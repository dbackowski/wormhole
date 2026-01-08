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
	tunnelURL  string
}

func NewLocalProxy(baseURL, tunnelURL string, timeout time.Duration) *LocalProxy {
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
	httpReq, err := http.NewRequest(req.Method, lp.buildURL(req.URL), bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	common.CopyHTTPHeaders(req.Headers, httpReq.Header)

	// Rewrite Origin, Referer, and Host headers to match the local service
	// This prevents CSRF/Origin validation errors in the proxied application
	lp.rewriteHeaders(httpReq.Header)

	httpResp, err := lp.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Rewrite response headers to use tunnel URL instead of local URL
	lp.rewriteResponseHeaders(httpResp.Header)

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

func (lp *LocalProxy) rewriteHeaders(headers http.Header) {
	baseURL, err := url.Parse(lp.baseURL)
	if err != nil {
		return
	}

	if headers.Get("Origin") != "" {
		headers.Set("Origin", fmt.Sprintf("%s://%s", baseURL.Scheme, baseURL.Host))
	}

	if referer := headers.Get("Referer"); referer != "" {
		if refererURL, err := url.Parse(referer); err == nil {
			refererURL.Scheme = baseURL.Scheme
			refererURL.Host = baseURL.Host
			headers.Set("Referer", refererURL.String())
		}
	}

	headers.Set("Host", baseURL.Host)
}

func (lp *LocalProxy) rewriteResponseHeaders(headers http.Header) {
	baseURL, err := url.Parse(lp.baseURL)
	if err != nil {
		return
	}

	tunnelURL, err := url.Parse(lp.tunnelURL)
	if err != nil {
		return
	}

	if location := headers.Get("Location"); location != "" {
		if locationURL, err := url.Parse(location); err == nil {
			if locationURL.Host == baseURL.Host || locationURL.Host == "" {
				locationURL.Scheme = tunnelURL.Scheme
				locationURL.Host = tunnelURL.Host
				headers.Set("Location", locationURL.String())
			}
		}
	}
}
