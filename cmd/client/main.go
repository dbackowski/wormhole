package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/dbackowski/wormhole/client"
	"github.com/dbackowski/wormhole/common"
)

func main() {
	common.ClearTerminal()

	clientCfg := client.ParseFlags()
	c, err := client.NewClient(clientCfg)

	if err != nil {
		fmt.Printf("Error creating client: %v\n", err)
		os.Exit(1)
	}

	webUI, err := client.NewWebUI(c, clientCfg.WebUIPort)
	if err != nil {
		fmt.Printf("Error creating web UI: %v\n", err)
		os.Exit(1)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := webUI.Start(); err != nil && err != http.ErrServerClosed {
			c.Logger.Error("Web UI error", "error", err)
		}
	}()

	go c.HandleConnection()

	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), common.ClientShutdownTimeout)
	defer cancel()

	if err := webUI.Shutdown(ctx); err != nil {
		c.Logger.Error("Web UI shutdown error", "error", err)
	}

	if err := c.Shutdown(); err != nil {
		c.Logger.Error("Client shutdown error", "error", err)
	}
}
