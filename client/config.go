package client

import (
	"flag"
	"fmt"
	"net/url"
	"slices"

	"github.com/dbackowski/wormhole/common"
)

type Config struct {
	ServerURL string
	Domain    string
	Local     string
	WebUIPort int
}

type ServerConfig struct {
	HTTPScheme string
	WSScheme   string
	Host       string
}

func validateAndParseServerURL(rawURL string) (*ServerConfig, error) {
	parsed, err := parseURL(rawURL)
	if err != nil {
		return nil, err
	}

	wsScheme := "ws"
	if parsed.Scheme == "https" {
		wsScheme = "wss"
	}

	return &ServerConfig{
		HTTPScheme: parsed.Scheme,
		WSScheme:   wsScheme,
		Host:       parsed.Host,
	}, nil
}

func parseURL(rawURL string) (url.URL, error) {
	if rawURL == "" {
		return url.URL{}, fmt.Errorf("url is empty")
	}

	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return url.URL{}, fmt.Errorf("invalid url: %w", err)
	}

	supportedSchemes := []string{"http", "https"}
	if !slices.Contains(supportedSchemes, parsed.Scheme) {
		return url.URL{}, fmt.Errorf("unsupported url scheme: %s", parsed.Scheme)
	}

	if parsed.Host == "" {
		return url.URL{}, fmt.Errorf("url missing host")
	}

	return *parsed, nil
}

func validateClientConfig(domain, local string, port int) error {
	if domain == "" {
		return fmt.Errorf("domain is required. Use -domain flag")
	}

	if local == "" {
		return fmt.Errorf("local is required. Use -local flag")
	}

	_, err := parseURL(local)

	if err != nil {
		return fmt.Errorf("invalid local URL: %w", err)
	}

	err = common.ValidatePort(port)

	if err != nil {
		return err
	}

	return nil
}

func ParseFlags() *Config {
	server := flag.String("server", common.DefaultClientServerURL, "Server URL")
	domain := flag.String("domain", "", "Domain to register (e.g., myapp)")
	local := flag.String("local", "", "Local service URL to expose")
	webUIPort := flag.Int("webui-port", common.DefaultWebUIPort, "Port for the Web UI")

	flag.Parse()

	return &Config{
		ServerURL: *server,
		Domain:    *domain,
		Local:     *local,
		WebUIPort: *webUIPort,
	}
}
