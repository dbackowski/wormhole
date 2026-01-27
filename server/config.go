package server

import (
	"flag"

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
	return common.ValidatePort(c.Port)
}
