package server

import (
	"flag"
	"fmt"

	"github.com/dbackowski/wormhole/common"
)

type Config struct {
	Port  int
	Debug bool
}

func ParseFlags() *Config {
	port := flag.Int("port", common.DefaultServerPort, "Port to run the server on")
	debug := flag.Bool("debug", false, "Enable debug mode")
	flag.Parse()

	return &Config{
		Port:  *port,
		Debug: *debug,
	}
}

func (c *Config) Validate() error {
	if c.Port < common.MinValidPort || c.Port > common.MaxValidPort {
		return fmt.Errorf("invalid port: %d (must be %d-%d)", c.Port, common.MinValidPort, common.MaxValidPort)
	}

	return nil
}
