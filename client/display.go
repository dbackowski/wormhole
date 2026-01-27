package client

import (
	"fmt"

	"github.com/dbackowski/wormhole/common"
)

type Display interface {
	ShowConnectionInfo(tunnelURL string, webUIPort int)
	ShowRequestHistory(logs []RequestLog)
}

type TerminalDisplay struct {
	maxLogs int
}

func NewTerminalDisplay(maxLogs int) *TerminalDisplay {
	return &TerminalDisplay{maxLogs: maxLogs}
}

func (td *TerminalDisplay) ShowConnectionInfo(tunnelURL string, webUIPort int) {
	fmt.Println("Connected to server.")
	fmt.Printf("Your tunnel is available at: %s\n", tunnelURL)
	fmt.Printf("Web UI is available at: http://localhost:%d\n", webUIPort)
	fmt.Printf("Waiting for incoming HTTP requests...\n\n")
}

func (td *TerminalDisplay) ShowRequestHistory(logs []RequestLog) {
	fmt.Printf("------------------- last %d requests -------------------\n\n", td.maxLogs)
	for _, rl := range logs {
		fmt.Printf("%s %s %s -> %d\n",
			common.FormatTime(rl.Timestamp), rl.Method, rl.URL, rl.StatusCode)
	}
}
