package progress

import (
	"fmt"
	"io"

	"remork/internal/output"
)

type Options struct {
	Quiet bool
	Color output.ColorMode
}

type TextReporter struct {
	w       io.Writer
	quiet   bool
	color   output.ColorMode
	label   string
	total   int64
	current int64
}

func NewTextReporter(w io.Writer, opts Options) *TextReporter {
	return &TextReporter{w: w, quiet: opts.Quiet, color: opts.Color}
}

func (r *TextReporter) Start(label string, total int64) {
	r.label = label
	r.total = total
	r.current = 0
	if r.quiet || r.w == nil {
		return
	}
	renderer := output.NewPlainRenderer(r.w, output.PlainOptions{Quiet: r.quiet, Color: r.color})
	if total > 1 {
		renderer.Step(fmt.Sprintf("%s 0/%d", label, total))
		return
	}
	renderer.Step(label)
}

func (r *TextReporter) Advance(delta int64) {
	r.current += delta
	if r.current > r.total {
		r.current = r.total
	}
	r.print()
}

func (r *TextReporter) Done() {
	r.current = r.total
	if r.quiet || r.w == nil {
		return
	}
	renderer := output.NewPlainRenderer(r.w, output.PlainOptions{Quiet: r.quiet, Color: r.color})
	if r.total > 1 {
		renderer.Success(fmt.Sprintf("%s %d/%d", r.label, r.current, r.total))
		return
	}
	renderer.Success(r.label)
}

func (r *TextReporter) print() {
	if r.quiet || r.w == nil {
		return
	}
	if r.total <= 1 {
		return
	}
	fmt.Fprintf(r.w, "%s %d/%d\n", r.label, r.current, r.total)
}
