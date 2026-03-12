package common

import (
	"net/url"
	"path"
)

func JoinURLPath(baseURL, urlPath string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	p, err := url.Parse(urlPath)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(u.Path, p.Path)
	if p.RawQuery != "" {
		u.RawQuery = p.RawQuery
	}
	return u.String(), nil
}

func BuildSubdomainURL(scheme, subdomain, host, urlPath string) string {
	u := &url.URL{
		Scheme: scheme,
		Host:   subdomain + "." + host,
		Path:   urlPath,
	}
	return u.String()
}
