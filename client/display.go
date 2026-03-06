package client

import (
	"fmt"
	"time"

	"github.com/dbackowski/wormhole/common"
)

func ClearTerminal() {
	fmt.Printf("\x1bc")
}

func FormatTime(t time.Time) string {
	return t.Format(common.TimeFormat)
}

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
			FormatTime(rl.Timestamp), rl.Method, rl.URL, rl.StatusCode)
	}
}
