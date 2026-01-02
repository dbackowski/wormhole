package main

import (
	"os"

	"github.com/dbackowski/wormhole/client"
	"github.com/dbackowski/wormhole/common"
)

func main() {
	common.ClearTerminal()

	clientCfg := client.ParseFlags()
	client, err := client.NewClient(clientCfg)

	if err != nil {
		client.Logger.Error("Error creating client:", "error", err)
		os.Exit(1)
	}

	defer client.Conn.Close()
	client.HandleConnection()
}
