package progress

import (
	"fmt"
	"io"
)

type Options struct {
	Quiet bool
}

type TextReporter struct {
	w       io.Writer
	quiet   bool
	label   string
	total   int64
	current int64
}

func NewTextReporter(w io.Writer, opts Options) *TextReporter {
	return &TextReporter{w: w, quiet: opts.Quiet}
}

func (r *TextReporter) Start(label string, total int64) {
	r.label = label
	r.total = total
	r.current = 0
	r.print()
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
	r.print()
}

func (r *TextReporter) print() {
	if r.quiet || r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "%s %d/%d\n", r.label, r.current, r.total)
}
