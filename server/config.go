package server

import (
	"flag"
	"fmt"
	"os"

	"github.com/dbackowski/wormhole/common"
)

type Config struct {
	Port  int
	Debug bool
	Host  string

	// Login is enabled when both are set; they come from the environment only,
	// to keep the secret off the command line.
	GoogleClientID     string
	GoogleClientSecret string
	DBPath             string
}

func ParseFlags(version string) *Config {
	port := flag.Int("port", common.DefaultServerPort, "Port to run the server on")
	debug := flag.Bool("debug", false, "Enable debug mode")
	host := flag.String("host", "", "Public hostname of this server (e.g. wormhole.tools). When set, /health and /metrics are only served on this host; requests to subdomains are tunneled. Required on a real domain: when unset, apex requests are treated as a tunnel (502) and the apex's first label is shadowed.")
	dbPath := flag.String("db", "wormhole.db", "SQLite database holding the allowlist and client tokens (used when login is enabled)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	cfg := &Config{
		Port:               *port,
		Debug:              *debug,
		Host:               *host,
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		DBPath:             *dbPath,
	}

	if cfg.Host == "" {
		cfg.Host = os.Getenv("WORMHOLE_HOST")
	}

	return cfg
}
