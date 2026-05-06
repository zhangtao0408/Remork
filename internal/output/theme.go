package output

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

type PlainOptions struct {
	Color ColorMode
	Quiet bool
}

type PlainRenderer struct {
	w     io.Writer
	color ColorMode
	quiet bool
}

func NewPlainRenderer(w io.Writer, opts PlainOptions) *PlainRenderer {
	if opts.Color == "" {
		opts.Color = ColorAuto
	}
	return &PlainRenderer{w: w, color: opts.Color, quiet: opts.Quiet}
}

func (r *PlainRenderer) Section(title string) {
	if r.skip() {
		return
	}
	fmt.Fprintf(r.w, "%s\n", r.emphasis("== "+title+" =="))
}

func (r *PlainRenderer) Step(message string) {
	if r.skip() {
		return
	}
	fmt.Fprintf(r.w, "%s %s\n", r.colorize(ansiCyan, "->"), message)
}

func (r *PlainRenderer) Progress(message string, current, total int64) {
	if r.skip() {
		return
	}
	if total > 0 {
		fmt.Fprintf(r.w, "%s %d/%d\n", message, current, total)
		return
	}
	fmt.Fprintf(r.w, "%s\n", message)
}

func (r *PlainRenderer) Success(message string) {
	if r.skip() {
		return
	}
	fmt.Fprintf(r.w, "%s %s\n", r.colorize(ansiGreen, "ok"), message)
}

func (r *PlainRenderer) Warning(message string) {
	if r.skip() {
		return
	}
	fmt.Fprintf(r.w, "%s %s\n", r.colorize(ansiYellow, "warn"), message)
}

func (r *PlainRenderer) Error(message, next string) {
	if r.skip() {
		return
	}
	fmt.Fprintf(r.w, "%s %s\n", r.colorize(ansiRed, "error"), message)
	if strings.TrimSpace(next) != "" {
		fmt.Fprintf(r.w, "next %s\n", next)
	}
}

func (r *PlainRenderer) KeyValue(key string, value any) {
	if r.skip() {
		return
	}
	fmt.Fprintf(r.w, "%s: %v\n", key, value)
}

func (r *PlainRenderer) Summary(title string, items map[string]int) {
	if r.skip() {
		return
	}
	if title != "" {
		r.Section(title)
	}
	for _, key := range []string{"create", "update", "delete", "skipped", "downloaded", "meta", "deleted", "conflicts"} {
		if value, ok := items[key]; ok {
			r.KeyValue(key, value)
		}
	}
}


func (r *PlainRenderer) ProductTitle(title, subtitle string) {
	if r.skip() {
		return
	}
	fmt.Fprintf(r.w, "%s\n", r.emphasis(title))
	if strings.TrimSpace(subtitle) != "" {
		fmt.Fprintf(r.w, "%s\n", subtitle)
	}
}

func (r *PlainRenderer) ActionList(title string, actions []string) {
	if r.skip() {
		return
	}
	if strings.TrimSpace(title) != "" {
		fmt.Fprintf(r.w, "%s\n", r.emphasis(title))
	}
	for i, action := range actions {
		fmt.Fprintf(r.w, "  %d. %s\n", i+1, action)
	}
}

func (r *PlainRenderer) Next(commands []string) {
	if r.skip() || len(commands) == 0 {
		return
	}
	fmt.Fprintf(r.w, "%s\n", r.emphasis("Next"))
	for _, command := range commands {
		fmt.Fprintf(r.w, "  %s\n", r.colorize(ansiCyan, command))
	}
}

func (r *PlainRenderer) skip() bool {
	return r == nil || r.quiet || r.w == nil
}

func (r *PlainRenderer) colorize(code, text string) string {
	if !r.allowColor() {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (r *PlainRenderer) emphasis(text string) string {
	if !r.allowColor() {
		return text
	}
	return lipgloss.NewStyle().Bold(true).Render(text)
}

func (r *PlainRenderer) allowColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	switch r.color {
	case ColorNever:
		return false
	case ColorAlways:
		return true
	default:
		return supportsColor(r.w)
	}
}

func ParseColorMode(value string) (ColorMode, error) {
	switch ColorMode(value) {
	case "", ColorAuto:
		return ColorAuto, nil
	case ColorAlways:
		return ColorAlways, nil
	case ColorNever:
		return ColorNever, nil
	default:
		return "", fmt.Errorf("invalid color mode %q: expected auto, always, or never", value)
	}
}
