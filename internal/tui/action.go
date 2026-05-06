package tui

import (
	"fmt"
	"strings"

	"remork/internal/output"
)

type ActionState string

const (
	ActionQueued  ActionState = "queued"
	ActionRunning ActionState = "running"
	ActionDone    ActionState = "done"
	ActionFailed  ActionState = "failed"
	ActionSkipped ActionState = "skipped"
)

type ActionItem struct {
	Label string
	State ActionState
}

type ActionTrack struct {
	Title        string
	Actions      []ActionItem
	SpinnerFrame string
	Color        output.ColorMode
}

func RemorkSpinnerFrames() []string {
	return []string{".", "o", "O", "°", "O", "o", "."}
}

func (t ActionTrack) View() string {
	var b strings.Builder
	if t.Title != "" {
		fmt.Fprintf(&b, "%s\n", t.Title)
	}
	for _, action := range t.Actions {
		fmt.Fprintf(&b, "  %s %s\n", t.symbol(action.State), action.Label)
	}
	return b.String()
}

func (t ActionTrack) symbol(state ActionState) string {
	switch state {
	case ActionRunning:
		if t.SpinnerFrame != "" {
			return t.SpinnerFrame
		}
		return "."
	case ActionDone:
		return "✓"
	case ActionFailed:
		return "×"
	case ActionSkipped:
		return "-"
	default:
		return "·"
	}
}
