package server

import (
	"flag"
	"fmt"
	"net/http"

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

func Run(cfg *Config) error {
	server := NewServer(&cfg.Debug)

	http.HandleFunc("/ws", server.ServeWebSocket)
	http.HandleFunc("/", server.ServeHTTP)

	addr := fmt.Sprintf(":%d", cfg.Port)
	fmt.Println("WebSocket server started on " + addr)

	return http.ListenAndServe(addr, nil)
}
