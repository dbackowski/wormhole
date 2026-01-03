package main

import (
	"os"

	"github.com/dbackowski/wormhole/client"
	"github.com/dbackowski/wormhole/common"
)

func main() {
	common.ClearTerminal()

	clientCfg := client.ParseFlags()
	c, err := client.NewClient(clientCfg)

	if err != nil {
		c.Logger.Error("Error creating client:", "error", err)
		os.Exit(1)
	}

	defer c.Conn.Close()
	c.HandleConnection()
}
