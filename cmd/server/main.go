package main

import (
	"fmt"
	"os"

	"github.com/dbackowski/wormhole/common"
	"github.com/dbackowski/wormhole/server"
)

func main() {
	common.ClearTerminal()

	cfg := server.ParseFlags()
	if err := server.Run(cfg); err != nil {
		fmt.Println("Error starting server:", err)
		os.Exit(1)
	}
}
