package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"remork/internal/output"
	"remork/internal/tui"
)

type Options struct {
	Quiet bool
	Color output.ColorMode
}

type TextReporter struct {
	mu      sync.Mutex
	w       io.Writer
	quiet   bool
	color   output.ColorMode
	label   string
	total   int64
	current int64
	frame   int
	stop    chan struct{}
	stopped chan struct{}
}

func NewTextReporter(w io.Writer, opts Options) *TextReporter {
	return &TextReporter{w: w, quiet: opts.Quiet, color: opts.Color}
}

func (r *TextReporter) Start(label string, total int64) {
	r.stopSpinner()
	r.mu.Lock()
	r.label = label
	r.total = total
	r.current = 0
	r.frame = 0
	if r.quiet || r.w == nil {
		r.mu.Unlock()
		return
	}
	r.writeLiveLocked()
	r.mu.Unlock()
	r.startSpinner()
}

func (r *TextReporter) Advance(delta int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current += delta
	if r.current > r.total {
		r.current = r.total
	}
	r.frame++
	r.writeLiveLocked()
}

func (r *TextReporter) Done() {
	r.DoneMessage("")
}

func (r *TextReporter) DoneMessage(message string) {
	r.stopSpinner()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current = r.total
	if r.quiet || r.w == nil {
		return
	}
	if strings.TrimSpace(message) == "" {
		message = r.doneMessageLocked()
	}
	fmt.Fprintf(r.w, "\r\x1b[2K%s %s\n", r.successMarkerLocked(), message)
}

func (r *TextReporter) FailMessage(message string) {
	r.stopSpinner()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.quiet || r.w == nil {
		return
	}
	if strings.TrimSpace(message) == "" {
		message = r.label
	}
	fmt.Fprintf(r.w, "\r\x1b[2K%s %s\n", r.errorMarkerLocked(), message)
}

func (r *TextReporter) startSpinner() {
	if r.quiet || r.w == nil {
		return
	}
	stop := make(chan struct{})
	stopped := make(chan struct{})
	r.mu.Lock()
	r.stop = stop
	r.stopped = stopped
	r.mu.Unlock()

	go func() {
		defer close(stopped)
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.mu.Lock()
				r.frame++
				r.writeLiveLocked()
				r.mu.Unlock()
			case <-stop:
				return
			}
		}
	}()
}

func (r *TextReporter) stopSpinner() {
	r.mu.Lock()
	stop := r.stop
	stopped := r.stopped
	r.stop = nil
	r.stopped = nil
	r.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-stopped
}

func (r *TextReporter) writeLiveLocked() {
	if r.quiet || r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "\r\x1b[2K%s %s", r.infoMarkerLocked(r.spinnerFrameLocked()), r.liveMessageLocked())
}

func (r *TextReporter) liveMessageLocked() string {
	if r.total <= 1 {
		return r.label
	}
	current, total := r.clampedProgressLocked()
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
	return fmt.Sprintf("%s [%s] %d/%d %d%%", r.label, bar, current, total, percent)
}

func (r *TextReporter) doneMessageLocked() string {
	if r.total <= 1 {
		return r.label
	}
	current, total := r.clampedProgressLocked()
	return fmt.Sprintf("%s %d/%d", r.label, current, total)
}

func (r *TextReporter) clampedProgressLocked() (int64, int64) {
	total := r.total
	if total <= 0 {
		total = 1
	}
	current := r.current
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	return current, total
}

func (r *TextReporter) spinnerFrameLocked() string {
	frames := tui.RemorkSpinnerFrames()
	if len(frames) == 0 {
		return "."
	}
	return frames[r.frame%len(frames)]
}

func (r *TextReporter) infoMarkerLocked(marker string) string {
	switch r.color {
	case output.ColorNever:
		return marker
	case output.ColorAlways:
		return "\x1b[36m" + marker + "\x1b[0m"
	default:
		return output.Info(r.w, marker)
	}
}

func (r *TextReporter) successMarkerLocked() string {
	switch r.color {
	case output.ColorNever:
		return "ok"
	case output.ColorAlways:
		return "\x1b[32m" + "ok" + "\x1b[0m"
	default:
		return output.Success(r.w, "ok")
	}
}

func (r *TextReporter) errorMarkerLocked() string {
	switch r.color {
	case output.ColorNever:
		return "error"
	case output.ColorAlways:
		return "\x1b[31m" + "error" + "\x1b[0m"
	default:
		return output.Error(r.w, "error")
	}
}
