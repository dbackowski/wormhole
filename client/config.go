package client

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"

	"github.com/dbackowski/wormhole/common"
)

type Config struct {
	ServerURL string
	Domain    string
	Local     string
	WebUIPort int
	AuthToken string
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

// domainLabel is a single DNS label. The server routes on the first label of
// the Host, so a dot would register only the part before it.
var domainLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func validateClientConfig(domain, local string, port int) (url.URL, error) {
	if domain == "" {
		return url.URL{}, fmt.Errorf("domain is required. Use -domain flag")
	}

	if !domainLabel.MatchString(domain) {
		return url.URL{}, fmt.Errorf("invalid domain %q: use lowercase letters, digits and hyphens (up to 63), with no dots", domain)
	}

	if local == "" {
		return url.URL{}, fmt.Errorf("local is required. Use -local flag")
	}

	parsed, err := parseURL(local)
	if err != nil {
		return url.URL{}, fmt.Errorf("invalid local URL: %w", err)
	}

	if err := common.ValidatePort(port); err != nil {
		return url.URL{}, fmt.Errorf("invalid web UI port: %w", err)
	}

	return parsed, nil
}

func ParseFlags(version string) *Config {
	server := flag.String("server", common.DefaultClientServerURL, "Server URL")
	domain := flag.String("domain", "", "Domain to register (e.g., myapp)")
	local := flag.String("local", "", "Local service URL to expose")
	webUIPort := flag.Int("webui-port", common.DefaultWebUIPort, "Port for the Web UI")
	authToken := flag.String("auth-token", "", "Authentication token for server connection")
	showVersion := flag.Bool("version", false, "Print version and exit")

	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	cfg := &Config{
		ServerURL: *server,
		Domain:    *domain,
		Local:     *local,
		WebUIPort: *webUIPort,
		AuthToken: *authToken,
	}

	if cfg.AuthToken == "" {
		cfg.AuthToken = common.AuthTokenFromConfigFile()
	}

	return cfg
}
