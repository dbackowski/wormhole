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
}

func ParseFlags(version string) *Config {
	port := flag.Int("port", common.DefaultServerPort, "Port to run the server on")
	debug := flag.Bool("debug", false, "Enable debug mode")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	return &Config{
		Port:  *port,
		Debug: *debug,
	}
}

func (c *Config) Validate() error {
	return common.ValidatePort(c.Port)
}
