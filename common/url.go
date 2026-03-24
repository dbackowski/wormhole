package common

import (
	"net/url"
	"path"
)

func JoinURLPath(baseURL url.URL, urlPath string) (string, error) {
	p, err := url.Parse(urlPath)
	if err != nil {
		return "", err
	}
	baseURL.Path = path.Join(baseURL.Path, p.Path)
	if p.RawQuery != "" {
		baseURL.RawQuery = p.RawQuery
	}
	return baseURL.String(), nil
}

func BuildSubdomainURL(scheme, subdomain, host, urlPath string) string {
	u := &url.URL{
		Scheme: scheme,
		Host:   subdomain + "." + host,
		Path:   urlPath,
	}
	return u.String()
}
