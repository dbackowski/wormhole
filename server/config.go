package server

import (
	"flag"
	"fmt"
	"os"

	"github.com/dbackowski/wormhole/common"
)

type Config struct {
	Port      int
	Debug     bool
	AuthToken string
	Host      string
}

func ParseFlags(version string) *Config {
	port := flag.Int("port", common.DefaultServerPort, "Port to run the server on")
	debug := flag.Bool("debug", false, "Enable debug mode")
	authToken := flag.String("auth-token", "", "Authentication token required for client connections")
	host := flag.String("host", "", "Public hostname of this server (e.g. wormhole.tools). When set, /health and /metrics are only served on this host; requests to subdomains are tunneled. Required on a real domain: when unset, apex requests are treated as a tunnel (502) and the apex's first label is shadowed.")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	cfg := &Config{
		Port:      *port,
		Debug:     *debug,
		AuthToken: *authToken,
		Host:      *host,
	}

	if cfg.AuthToken == "" {
		cfg.AuthToken = os.Getenv("AUTH_TOKEN")
	}

	if cfg.Host == "" {
		cfg.Host = os.Getenv("HOST")
	}

	if cfg.AuthToken == "" {
		fileCfg, err := common.LoadConfigFile(common.DefaultConfigPath())
		if err != nil {
			fmt.Printf("Warning: failed to load config file: %v\n", err)
		} else if fileCfg.AuthToken != "" {
			cfg.AuthToken = fileCfg.AuthToken
		}
	}

	return cfg
}

func (c *Config) Validate() error {
	return common.ValidatePort(c.Port)
}
