package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dbackowski/wormhole/client"
	"github.com/dbackowski/wormhole/common"
)

var version = "dev"

func main() {
	clientCfg := client.ParseFlags(common.Version(version))
	c, err := client.NewClient(clientCfg)

	// No token yet, or the server stopped accepting it: log in through the
	// browser. Not for a token given with -auth-token, which is how scripts and
	// CI connect, where a browser would only hang the run.
	if errors.Is(err, client.ErrUnauthorized) && !flagSet("auth-token") {
		c, err = loginAndConnect(clientCfg)
	}

	if err != nil {
		if errors.Is(err, client.ErrDomainTaken) {
			fmt.Fprintf(os.Stderr, "Domain %q is already taken. Please choose another one with -domain.\n", clientCfg.Domain)
		} else {
			fmt.Fprintf(os.Stderr, "Error creating client: %v\n", err)
		}
		os.Exit(1)
	}

	webUI := client.NewWebUI(c, clientCfg.WebUIPort)

	// Bound before entering the alt screen: an error printed there is wiped by
	// the next refresh and discarded on exit.
	if err := webUI.Start(); err != nil {
		c.Shutdown()
		fmt.Fprintf(os.Stderr, "Web UI could not start on port %d: %v. Choose another with -webui-port.\n", clientCfg.WebUIPort, err)
		os.Exit(1)
	}

	// Printed after the alt screen is left (defers run in reverse), so it stays
	// visible. Registered first so it runs last: os.Exit skips no other cleanup,
	// and the non-zero status lets a supervisor restart the client.
	var lostErr error
	defer func() {
		if lostErr != nil {
			fmt.Fprintf(os.Stderr, "Connection to the server was lost: %v. Exiting.\n", lostErr)
			os.Exit(1)
		}
	}()

	client.EnterAltScreen()
	defer client.ExitAltScreen()

	c.RefreshTerminalOutput()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go c.RunDisplay()

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

	webUI.Shutdown(ctx) //nolint:errcheck // exiting anyway
	c.Shutdown()        //nolint:errcheck
}

func loginAndConnect(cfg *client.Config) (*client.Client, error) {
	token, email, err := client.Login(cfg.ServerURL, os.Stdout)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Logged in as %s.\n", email)

	if err := common.SaveAuthToken(common.DefaultConfigPath(), token); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save the token, you will have to log in again next time: %v\n", err)
	}

	cfg.AuthToken = token
	return client.NewClient(cfg)
}

func flagSet(name string) (set bool) {
	flag.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}
