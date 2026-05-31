package common

import (
	"net/url"
	"strings"
)

func JoinURLPath(baseURL url.URL, urlPath string) (string, error) {
	p, err := url.Parse(urlPath)
	if err != nil {
		return "", err
	}

	joined := joinEscapedPaths(baseURL.EscapedPath(), p.EscapedPath())

	baseURL.RawPath = joined
	baseURL.Path, err = url.PathUnescape(joined)
	if err != nil {
		return "", err
	}

	if p.RawQuery != "" {
		baseURL.RawQuery = p.RawQuery
	}
	return baseURL.String(), nil
}

func joinEscapedPaths(base, ref string) string {
	if ref == "" {
		return base
	}
	base = strings.TrimSuffix(base, "/")
	if !strings.HasPrefix(ref, "/") {
		ref = "/" + ref
	}
	return base + ref
}

func BuildSubdomainURL(scheme, subdomain, host, urlPath string) string {
	u := &url.URL{
		Scheme: scheme,
		Host:   subdomain + "." + host,
		Path:   urlPath,
	}
	return u.String()
}
