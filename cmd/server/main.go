package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dbackowski/wormhole/common"
	"github.com/dbackowski/wormhole/server"
)

var version = "dev"

func main() {
	fmt.Printf("\x1bc")

	cfg := server.ParseFlags(version)
	srv, err := server.NewServer(cfg)

	if err != nil {
		fmt.Printf("Failed to create server: %v\n", err)
		os.Exit(1)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil {
			srv.Logger.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), common.ServerShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		srv.Logger.Error("Shutdown error", "error", err)
		os.Exit(1)
	}
}
