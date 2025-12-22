package server

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) extractDomain(host string) (string, error) {
	parts := strings.Split(host, ".")

	if len(parts) == 0 || parts[0] == "" {
		return "", fmt.Errorf("invalid host: %s", host)
	}
	return parts[0], nil
}

func (s *Server) getConnectionForDomain(r *http.Request) (*Connection, string, error) {
	domain, err := s.extractDomain(r.Host)

	if err != nil {
		return nil, "", err
	}

	connection, exists := s.connManager.GetConnection(domain)

	if !exists {
		return nil, domain, fmt.Errorf("tunnel not found for domain: %s", domain)
	}

	return connection, domain, nil
}
