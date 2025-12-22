package main

import (
	"log"

	"github.com/dbackowski/wormhole/client"
	"github.com/dbackowski/wormhole/common"
)

func main() {
	common.ClearTerminal()

	clientCfg := client.ParseFlags()
	client, err := client.NewClient(clientCfg)

	if err != nil {
		log.Fatal("Error creating client:", err)
	}

	defer client.Conn.Close()
	client.HandleConnection()
}
