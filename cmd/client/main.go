package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/dbackowski/wormhole/client"
	"github.com/dbackowski/wormhole/common"
)

var version = "dev"

func main() {
	clientCfg := client.ParseFlags(version)
	c, err := client.NewClient(clientCfg)

	if err != nil {
		if errors.Is(err, client.ErrDomainTaken) {
			fmt.Printf("Domain %q is already taken. Please choose another one with -domain.\n", clientCfg.Domain)
		} else {
			fmt.Printf("Error creating client: %v\n", err)
		}
		os.Exit(1)
	}

	webUI, err := client.NewWebUI(c, clientCfg.WebUIPort)
	if err != nil {
		fmt.Printf("Error creating web UI: %v\n", err)
		os.Exit(1)
	}

	connectionLost := false
	defer func() {
		if connectionLost {
			fmt.Println("Connection to the server was lost. Exiting.")
		}
	}()

	client.EnterAltScreen()
	defer client.ExitAltScreen()

	c.RefreshTerminalOutput()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	quitCh := make(chan struct{})
	go client.WaitForQuit(quitCh)

	go func() {
		if err := webUI.Start(); err != nil && err != http.ErrServerClosed {
			c.Logger.Error("Web UI error", "error", err)
		}
	}()

	disconnectedCh := make(chan struct{})
	go func() {
		c.HandleConnection()
		close(disconnectedCh)
	}()

	select {
	case <-sigCh:
	case <-quitCh:
	case <-disconnectedCh:
		connectionLost = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), common.ClientShutdownTimeout)
	defer cancel()

	if err := webUI.Shutdown(ctx); err != nil {
		c.Logger.Error("Web UI shutdown error", "error", err)
	}

	if err := c.Shutdown(); err != nil {
		c.Logger.Error("Client shutdown error", "error", err)
	}
}
