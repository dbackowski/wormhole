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
	version := common.Version(version)
	cfg := server.ParseFlags(version)
	srv, err := server.NewServer(cfg)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create server: %v\n", err)
		os.Exit(1)
	}

	srv.Logger.Info("wormhole-server", "version", version)

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
