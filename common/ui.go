package common

import (
	"fmt"
	"io"
	"os"
)

type UIWriter struct {
	output io.Writer
}

func NewUIWriter() *UIWriter {
	return &UIWriter{output: os.Stdout}
}

func (u *UIWriter) Print(msg string) {
	fmt.Fprint(u.output, msg)
}

func (u *UIWriter) Printf(format string, args ...any) {
	fmt.Fprintf(u.output, format, args...)
}

func (u *UIWriter) Println(msg string) {
	fmt.Fprintln(u.output, msg)
}

func (u *UIWriter) ClearScreen() {
	fmt.Fprintf(u.output, "\x1bc")
}
