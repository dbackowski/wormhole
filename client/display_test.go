package client

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// frameLines returns what a frame shows, one string per terminal row.
func frameLines(frame string) []string {
	return strings.Split(ansiEscape.ReplaceAllString(frame, ""), "\n")
}

func testLogs(n int) []RequestLog {
	logs := make([]RequestLog, n)
	for i := range logs {
		logs[i] = RequestLog{Method: "GET", URL: fmt.Sprintf("/req/%d", i), StatusCode: 200}
	}
	return logs
}

func TestRenderFrame(t *testing.T) {
	frame := Frame{
		TunnelURL: "https://myapp.example.com",
		LocalURL:  "http://localhost:3000",
		WebUIPort: 4040,
		Logs:      testLogs(3),
	}

	output := strings.Join(frameLines(renderFrame(frame, 24, 80)), "\n")

	for _, want := range []string{
		"Wormhole",
		"Tunnel:  https://myapp.example.com → http://localhost:3000",
		"Web UI:  http://localhost:4040",
		"recent requests",
		"/req/0",
		"/req/2",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("frame missing %q\ngot:\n%s", want, output)
		}
	}
}

func TestRenderFrame_FillsTerminalHeight(t *testing.T) {
	tests := []struct {
		name      string
		rows      int
		status    string
		wantFirst string // oldest request still shown
	}{
		{"small terminal", 24, "", "/req/86"},
		{"tall terminal", 60, "", "/req/50"},
		{"status line takes two rows", 24, "reconnecting...", "/req/88"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := Frame{Status: tt.status, Logs: testLogs(100)}
			lines := frameLines(renderFrame(frame, tt.rows, 80))

			if want := tt.rows - bottomMargin; len(lines) != want {
				t.Fatalf("frame has %d lines, want %d", len(lines), want)
			}
			if last := lines[len(lines)-1]; !strings.HasSuffix(last, "/req/99") {
				t.Errorf("last line = %q, want the newest request", last)
			}
			joined := strings.Join(lines, "\n")
			if !strings.Contains(joined, tt.wantFirst+"\n") {
				t.Errorf("frame missing oldest expected request %q", tt.wantFirst)
			}
			if tt.status != "" && !strings.Contains(joined, tt.status) {
				t.Errorf("frame missing status %q", tt.status)
			}
		})
	}
}

func TestRenderFrame_Empty(t *testing.T) {
	output := renderFrame(Frame{}, 24, 80)

	if !strings.Contains(output, "waiting for requests") {
		t.Errorf("empty frame missing the waiting hint\ngot:\n%s", output)
	}
}

func TestRenderFrame_TinyTerminal(t *testing.T) {
	for _, rows := range []int{0, 1, 5} {
		lines := frameLines(renderFrame(Frame{Logs: testLogs(10)}, rows, 80))
		if len(lines) > max(rows, 1) {
			t.Errorf("rows=%d: frame has %d lines", rows, len(lines))
		}
	}
}

func TestRequestLine(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 45, 0, time.UTC)
	long := "/" + strings.Repeat("a", 300)

	tests := []struct {
		name string
		log  RequestLog
		cols int
		want []string
	}{
		{
			name: "short url",
			log:  RequestLog{Timestamp: ts, Method: "POST", URL: "/api/data", StatusCode: 201, Duration: 42 * time.Millisecond, ResponseBodySize: 1536},
			cols: 80,
			want: []string{"10:30:45", "POST", "201  42ms  1.5KB  /api/data"},
		},
		{
			name: "long url is cut to the terminal width",
			log:  RequestLog{Timestamp: ts, Method: "GET", URL: long, StatusCode: 200},
			cols: 80,
			want: []string{"200", "/aaaa", "…"},
		},
		{
			name: "note survives a long url",
			log:  RequestLog{Timestamp: ts, Method: "GET", URL: long, StatusCode: 101, Stream: StreamInfo{Open: true}},
			cols: 80,
			want: []string{"…  websocket open"},
		},
		{
			name: "long error leaves room for the url",
			log:  RequestLog{Timestamp: ts, Method: "GET", URL: long, StatusCode: 502, Error: strings.Repeat("e", 300)},
			cols: 80,
			want: []string{"/" + strings.Repeat("a", 18) + "…  eee"},
		},
		{
			name: "closed websocket",
			log:  RequestLog{Timestamp: ts, Method: "GET", URL: "/ws", StatusCode: 101, Stream: StreamInfo{ClosedAt: ts}},
			cols: 80,
			want: []string{"    -  /ws  websocket closed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ansiEscape.ReplaceAllString(requestLine(tt.log, tt.cols), "")

			if width := len([]rune(got)); width >= tt.cols {
				t.Errorf("line is %d columns wide, want under %d: %q", width, tt.cols, got)
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("line missing %q: %q", want, got)
				}
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0ms"},
		{800 * time.Microsecond, "0ms"},
		{42 * time.Millisecond, "42ms"},
		{999 * time.Millisecond, "999ms"},
		{time.Second, "1.0s"},
		{10*time.Second + 400*time.Millisecond, "10.4s"},
	}

	for _, tt := range tests {
		if got := formatDuration(tt.d); got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0B"},
		{1023, "1023B"},
		{1536, "1.5KB"},
		{10<<10 - 1, "10.0KB"},
		{10 << 10, "10KB"},
		{1<<20 - 1, "1023KB"},
		{10 << 20, "10.0MB"},
	}

	for _, tt := range tests {
		if got := formatBytes(tt.n); got != tt.want || len(got) > 6 {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestRenderFrame_Totals(t *testing.T) {
	output := renderFrame(Frame{Logs: testLogs(3), Total: 1204, Rate: 0.3}, 24, 80)
	if !strings.Contains(output, "recent requests · 1204 total · 0.3/s") {
		t.Errorf("frame missing totals\ngot:\n%s", output)
	}
	if output := renderFrame(Frame{}, 24, 80); strings.Contains(output, "total") {
		t.Errorf("frame with no requests yet shows totals\ngot:\n%s", output)
	}
}

func TestTerminalDisplayImplementsDisplayInterface(t *testing.T) {
	var _ Display = TerminalDisplay{}
}

func TestColorStatus(t *testing.T) {
	tests := []struct {
		status    int
		wantColor string
	}{
		{200, ansiGreen},
		{201, ansiGreen},
		{301, ansiCyan},
		{400, ansiYellow},
		{404, ansiYellow},
		{500, ansiRed},
		{503, ansiRed},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.status), func(t *testing.T) {
			got := colorStatus(tt.status)
			code := fmt.Sprintf("%d", tt.status)
			want := tt.wantColor + code + ansiReset
			if got != want {
				t.Errorf("colorStatus(%d) = %q, want %q", tt.status, got, want)
			}
		})
	}
}

func TestEnterExitAltScreen(t *testing.T) {
	enterOutput := captureStdout(t, func() {
		EnterAltScreen()
	})
	wantEnter := altScreenOn + clearScreen + cursorHome + hideCursor + wrapOff
	if enterOutput != wantEnter {
		t.Errorf("EnterAltScreen() wrote %q, want %q", enterOutput, wantEnter)
	}

	exitOutput := captureStdout(t, func() {
		ExitAltScreen()
	})
	wantExit := wrapOn + showCursor + altScreenOff
	if exitOutput != wantExit {
		t.Errorf("ExitAltScreen() wrote %q, want %q", exitOutput, wantExit)
	}
}

// A log gets each request once, as plain text, when it has finished, and the
// status when it changes, however often the display loop redraws.
func TestLineDisplay(t *testing.T) {
	var out bytes.Buffer
	d := NewLineDisplay(&out)
	ts := time.Date(2026, 1, 2, 14, 2, 11, 0, time.UTC)
	f := Frame{TunnelURL: "https://app.example.com", LocalURL: "http://localhost:3000", WebUIPort: 4040}
	render := func() []string {
		out.Reset()
		d.Render(f)
		if strings.Contains(out.String(), "\x1b") {
			t.Errorf("escape codes in log output: %q", out.String())
		}
		return strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	}

	if got := render(); len(got) != 1 || got[0] != "Tunnel: https://app.example.com → http://localhost:3000 (Web UI: http://localhost:4040)" {
		t.Errorf("first frame = %q, want the tunnel line", got)
	}
	if render(); out.Len() != 0 {
		t.Errorf("unchanged frame printed %q", out.String())
	}

	f.Logs = []RequestLog{
		{UUID: "a", Timestamp: ts, Method: "GET", URL: "/done", StatusCode: 200, ResponseBodySize: 2048, Done: true},
		{UUID: "b", Timestamp: ts, Method: "GET", URL: "/events", StatusCode: 200},
		{UUID: "c", Timestamp: ts, Method: "POST", URL: "/webhook/" + strings.Repeat("x", 300), StatusCode: 502, Error: "request failed", Done: true},
		{UUID: "d", Timestamp: ts, Method: "GET", URL: "/socket", StatusCode: 101, Stream: StreamInfo{Open: true}},
	}
	got := render()
	if len(got) != 3 || !strings.HasPrefix(got[0], "14:02:11  GET     200") || !strings.Contains(got[0], "2.0KB  /done") ||
		!strings.HasSuffix(got[1], strings.Repeat("x", 300)+"  request failed") || !strings.Contains(got[2], "/socket  websocket open") {
		t.Errorf("lines = %q, want /done with its size, /webhook uncut, /socket, and not the unfinished /events", got)
	}

	// A response still streaming is printed once it ends, with its error.
	f.Logs[1].Done, f.Logs[1].Error = true, "browser closed the connection"
	f.Logs[3].Stream = StreamInfo{ClosedAt: ts}
	if got := render(); len(got) != 1 || !strings.HasSuffix(got[0], "/events  browser closed the connection") {
		t.Errorf("after /events ended = %q, want only it", got)
	}

	// Entries the history has dropped are forgotten, not printed again.
	f.Logs = f.Logs[3:]
	if render(); out.Len() != 0 || len(d.printed) != 1 {
		t.Errorf("after the history moved on: printed %q, remembering %d", out.String(), len(d.printed))
	}

	f.Status = "reconnecting... (attempt 1)"
	if got := render(); len(got) != 1 || got[0] != f.Status {
		t.Errorf("status change = %q, want %q", got, f.Status)
	}
	if render(); out.Len() != 0 {
		t.Errorf("unchanged status printed again: %q", out.String())
	}
	f.Status = ""
	if got := render(); len(got) != 1 || got[0] != "reconnected" {
		t.Errorf("status cleared = %q, want reconnected", got)
	}
}
