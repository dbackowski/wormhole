package main

import (
	"context"
	"errors"
	"fmt"
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

	// Bound before entering the alt screen: an error printed there is wiped by
	// the next refresh and discarded on exit.
	if err := webUI.Start(); err != nil {
		c.Shutdown()
		fmt.Printf("Web UI could not start on port %d: %v. Choose another with -webui-port.\n", clientCfg.WebUIPort, err)
		os.Exit(1)
	}

	// Printed after the alt screen is left (defers run in reverse), so it stays
	// visible.
	var lostErr error
	defer func() {
		if lostErr != nil {
			fmt.Printf("Connection to the server was lost: %v. Exiting.\n", lostErr)
		}
	}()

	client.EnterAltScreen()
	defer client.ExitAltScreen()

	c.RefreshTerminalOutput()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	quitCh := make(chan struct{})
	go client.WaitForInput(quitCh, c.ClearHistory)

	disconnectedCh := make(chan error, 1)
	go func() {
		for {
			c.HandleConnection()
			if err := c.ReconnectWithBackoff(quitCh); err != nil {
				disconnectedCh <- err
				return
			}
		}
	}()

	select {
	case <-sigCh:
	case <-quitCh:
	case lostErr = <-disconnectedCh:
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
