package output

import (
	"io"
	"os"
)

const (
	ansiGreen  = "32"
	ansiYellow = "33"
	ansiCyan   = "36"
	ansiRed    = "31"
)

func Info(w io.Writer, text string) string {
	return colorize(w, ansiCyan, text)
}

func Success(w io.Writer, text string) string {
	return colorize(w, ansiGreen, text)
}

func Warning(w io.Writer, text string) string {
	return colorize(w, ansiYellow, text)
}

func Error(w io.Writer, text string) string {
	return colorize(w, ansiRed, text)
}

func colorize(w io.Writer, code, text string) string {
	if !supportsColor(w) {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func supportsColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
