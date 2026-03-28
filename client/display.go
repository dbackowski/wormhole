package client

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dbackowski/wormhole/common"
	"golang.org/x/sys/unix"
)

const (
	ansiReset  = "\033[0m"
	ansiDim    = "\033[2m"
	ansiGreen  = "\033[32m"
	ansiCyan   = "\033[36m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiBold   = "\033[1m"

	cursorHome   = "\033[H"
	clearScreen  = "\033[2J"
	hideCursor   = "\033[?25l"
	showCursor   = "\033[?25h"
	altScreenOn  = "\033[?1049h"
	altScreenOff = "\033[?1049l"
)

var restoreTerminal func()

func EnterAltScreen() {
	fmt.Print(altScreenOn + clearScreen + cursorHome + hideCursor)

	fd := int(os.Stdin.Fd())
	oldState, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return
	}

	saved := *oldState

	newState := *oldState
	newState.Lflag &^= unix.ECHO | unix.ICANON
	newState.Cc[unix.VMIN] = 1
	newState.Cc[unix.VTIME] = 0

	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, &newState); err != nil {
		return
	}

	restoreTerminal = func() {
		unix.IoctlSetTermios(fd, unix.TIOCSETA, &saved)
	}
}

func ExitAltScreen() {
	if restoreTerminal != nil {
		restoreTerminal()
	}
	fmt.Print(showCursor + altScreenOff)
}

func WaitForQuit(quit chan<- struct{}) {
	buf := make([]byte, 1)
	for {
		_, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		if buf[0] == 'q' || buf[0] == 'Q' {
			close(quit)
			return
		}
	}
}

func ClearTerminal() {
	fmt.Print(cursorHome + clearScreen)
}

func FormatTime(t time.Time) string {
	return t.Format(common.TimeFormat)
}

func colorStatus(status int) string {
	code := fmt.Sprintf("%d", status)
	switch {
	case status < 300:
		return ansiGreen + code + ansiReset
	case status < 400:
		return ansiCyan + code + ansiReset
	case status < 500:
		return ansiYellow + code + ansiReset
	default:
		return ansiRed + code + ansiReset
	}
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
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s%s\n", ansiBold, "Wormhole", ansiReset)
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "  Tunnel:  %s\n", tunnelURL)
	fmt.Fprintf(&b, "  Web UI:  http://localhost:%d\n", webUIPort)
	fmt.Fprintf(&b, "\n  %sPress q to quit%s\n\n", ansiDim, ansiReset)
	fmt.Print(b.String())
}

func (td *TerminalDisplay) ShowRequestHistory(logs []RequestLog) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s── last %d requests ──%s\n\n", ansiDim, td.maxLogs, ansiReset)
	for _, rl := range logs {
		fmt.Fprintf(&b, "  %s%s%s  %-7s %-30s %s\n",
			ansiDim, FormatTime(rl.Timestamp), ansiReset,
			rl.Method, rl.URL, colorStatus(rl.StatusCode))
	}
	fmt.Print(b.String())
}
