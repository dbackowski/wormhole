package main

import (
	"log"

	"github.com/dbackowski/wormhole/client"
	"github.com/dbackowski/wormhole/common"
)

func main() {
	common.ClearTerminal()

	clientCfg := client.ParseFlags()
	c, err := client.NewClient(clientCfg)

	if err != nil {
		log.Fatal("Error creating client", err)
	}

	defer c.Conn.Close()
	c.HandleConnection()
}
