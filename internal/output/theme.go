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

func (r *PlainRenderer) ProgressBar(label string, current, total int64) {
	if r.skip() {
		return
	}
	if total <= 0 {
		r.Progress(label, current, total)
		return
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	const width = 24
	ratio := float64(current) / float64(total)
	filled := int(ratio * width)
	if current > 0 && filled == 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
	percent := int(ratio * 100)
	if percent > 100 {
		percent = 100
	}
	fmt.Fprintf(r.w, "%s %s [%s] %d/%d %d%%\n", r.colorize(ansiCyan, "->"), label, r.progressBarColor(bar, filled), current, total, percent)
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
	fmt.Fprintf(r.w, "%s: %v\n", r.colorize(ansiBlue, key), value)
}

func (r *PlainRenderer) List(title string, items []string) {
	if r.skip() {
		return
	}
	if strings.TrimSpace(title) != "" {
		fmt.Fprintf(r.w, "%s\n", r.emphasis(title))
	}
	if len(items) == 0 {
		fmt.Fprintf(r.w, "  %s\n", r.dim("<none>"))
		return
	}
	for _, item := range items {
		fmt.Fprintf(r.w, "  %s %s\n", r.colorize(ansiCyan, "-"), item)
	}
}

func (r *PlainRenderer) Command(command string) {
	if r.skip() {
		return
	}
	if strings.TrimSpace(command) == "" {
		return
	}
	fmt.Fprintf(r.w, "%s %s\n", r.colorize(ansiMagenta, "cmd"), command)
}

func (r *PlainRenderer) Empty(message, next string) {
	if r.skip() {
		return
	}
	r.Warning(message)
	if strings.TrimSpace(next) != "" {
		fmt.Fprintf(r.w, "next %s\n", next)
	}
}

func (r *PlainRenderer) Panel(title string, lines []string) {
	if r.skip() {
		return
	}
	if strings.TrimSpace(title) != "" {
		r.Section(title)
	}
	for _, line := range lines {
		fmt.Fprintf(r.w, "  %s\n", line)
	}
}

func (r *PlainRenderer) Table(headers []string, rows [][]string) {
	if r.skip() {
		return
	}
	if len(headers) == 0 {
		return
	}
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = lipgloss.Width(header)
	}
	for _, row := range rows {
		for i := range headers {
			if i < len(row) && lipgloss.Width(row[i]) > widths[i] {
				widths[i] = lipgloss.Width(row[i])
			}
		}
	}
	for i, header := range headers {
		if i > 0 {
			fmt.Fprint(r.w, "  ")
		}
		fmt.Fprint(r.w, r.tableCell(r.colorize(ansiBlue, header), widths[i]))
	}
	fmt.Fprintln(r.w)
	for _, row := range rows {
		for i := range headers {
			if i > 0 {
				fmt.Fprint(r.w, "  ")
			}
			value := ""
			if i < len(row) {
				value = row[i]
			}
			fmt.Fprint(r.w, r.tableCell(value, widths[i]))
		}
		fmt.Fprintln(r.w)
	}
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

func (r *PlainRenderer) dim(text string) string {
	return r.colorize(ansiDim, text)
}

func (r *PlainRenderer) emphasis(text string) string {
	if !r.allowColor() {
		return text
	}
	return lipgloss.NewStyle().Bold(true).Render(text)
}

func (r *PlainRenderer) progressBarColor(bar string, filled int) string {
	if !r.allowColor() || filled <= 0 {
		return bar
	}
	return r.colorize(ansiGreen, bar[:filled]) + r.dim(bar[filled:])
}

func (r *PlainRenderer) tableCell(text string, width int) string {
	pad := width - lipgloss.Width(text)
	if pad <= 0 {
		return text
	}
	return text + strings.Repeat(" ", pad)
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
