package client

import (
	"fmt"
	"os"
	"strings"
	"time"

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
	clearLine    = "\033[K"
	clearBelow   = "\033[J"
	wrapOff      = "\033[?7l"
	wrapOn       = "\033[?7h"
	hideCursor   = "\033[?25l"
	showCursor   = "\033[?25h"
	altScreenOn  = "\033[?1049h"
	altScreenOff = "\033[?1049l"
)

var restoreTerminal func()

// Wrapping is turned off so that a line too long for the terminal is clipped
// instead of taking a second row and pushing the top of the frame off screen.
func EnterAltScreen() {
	fmt.Print(altScreenOn + clearScreen + cursorHome + hideCursor + wrapOff)

	fd := int(os.Stdin.Fd())
	oldState, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return
	}

	saved := *oldState

	newState := *oldState
	newState.Lflag &^= unix.ECHO | unix.ICANON
	newState.Cc[unix.VMIN] = 1
	newState.Cc[unix.VTIME] = 0

	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, &newState); err != nil {
		return
	}

	restoreTerminal = func() {
		unix.IoctlSetTermios(fd, ioctlSetTermios, &saved)
	}
}

func ExitAltScreen() {
	if restoreTerminal != nil {
		restoreTerminal()
	}
	fmt.Print(wrapOn + showCursor + altScreenOff)
}

func WaitForInput(quit chan<- struct{}, onClear func()) {
	buf := make([]byte, 1)
	for {
		_, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		switch buf[0] {
		case 'q', 'Q':
			close(quit)
			return
		case 'c', 'C':
			onClear()
		}
	}
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
	Render(f Frame)
}

// Frame is everything one redraw of the terminal shows.
type Frame struct {
	TunnelURL string
	LocalURL  string
	WebUIPort int
	Status    string
	Logs      []RequestLog // oldest first
}

type TerminalDisplay struct{}

func (TerminalDisplay) Render(f Frame) {
	rows, cols := terminalSize()
	fmt.Print(renderFrame(f, rows, cols))
}

// terminalSize falls back to 80x24 when stdout is not a terminal.
func terminalSize() (rows, cols int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Row == 0 || ws.Col == 0 {
		return 24, 80
	}
	return int(ws.Row), int(ws.Col)
}

// Rows left empty under the request list.
const bottomMargin = 1

// renderFrame draws f with as many of the latest requests as fit in rows. The
// result is written at once over the previous frame, each line erasing what is
// left of the old one, so the screen does not flicker as it does when cleared
// first.
func renderFrame(f Frame, rows, cols int) string {
	rows -= bottomMargin

	lines := []string{
		ansiBold + "Wormhole" + ansiReset,
		"",
		fmt.Sprintf("  Tunnel:  %s → %s", f.TunnelURL, f.LocalURL),
		fmt.Sprintf("  Web UI:  http://localhost:%d", f.WebUIPort),
		"",
		"  " + ansiDim + "Press q to quit, c to clear" + ansiReset,
		"",
	}
	if f.Status != "" {
		lines = append(lines, "  "+ansiYellow+f.Status+ansiReset, "")
	}

	title := "recent requests"
	if len(f.Logs) == 0 {
		title = "waiting for requests"
	}
	lines = append(lines, ansiDim+"── "+title+" ──"+ansiReset, "")

	logs := f.Logs
	if n := max(rows-len(lines), 0); len(logs) > n {
		logs = logs[len(logs)-n:]
	}
	for _, rl := range logs {
		lines = append(lines, requestLine(rl, cols))
	}
	// No newline after the last line: on the bottom row it would scroll.
	lines = lines[:min(len(lines), max(rows, 0))]

	return cursorHome + strings.Join(lines, clearLine+"\n") + clearLine + clearBelow
}

// Columns before the URL: indent, time, method, status and duration with their
// gaps.
const requestPrefixWidth = 2 + 8 + 2 + 7 + 1 + 3 + 1 + 5 + 2

func requestLine(rl RequestLog, cols int) string {
	note, noteColor := "", ansiDim
	switch {
	case rl.Error != "":
		note, noteColor = rl.Error, ansiRed
	case rl.Stream.Open:
		note, noteColor = "websocket open", ansiGreen
	case !rl.Stream.ClosedAt.IsZero():
		note = "websocket closed"
	}

	// The last column stays free: erasing to the end of a full line would take
	// its last character.
	room := cols - 1 - requestPrefixWidth
	noteWidth := 0
	if note != "" {
		noteWidth = 2 + len([]rune(note))
	}
	// A long note may take at most half the room from the URL.
	url := truncate(rl.URL, max(room-noteWidth, room/2))
	note = truncate(note, room-2-len([]rune(url)))

	line := fmt.Sprintf("  %s%s%s  %-7s %s %s%5s%s  %s",
		ansiDim, rl.Timestamp.Format(time.TimeOnly), ansiReset,
		rl.Method, colorStatus(rl.StatusCode),
		ansiDim, formatDuration(rl.Duration), ansiReset, url)
	if note != "" {
		line += "  " + noteColor + note + ansiReset
	}
	return line
}

// formatDuration fits the 5 columns the terminal gives it up to 99.9 s, well
// past the request timeout.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// truncate cuts s to width columns, ending it with an ellipsis if it was cut.
// ponytail: counts runes, not display cells, so wide characters (CJK, emoji)
// run over and are clipped by the terminal; use a width table if that matters.
func truncate(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 0 {
		return ""
	}
	return string(r[:width-1]) + "…"
}
