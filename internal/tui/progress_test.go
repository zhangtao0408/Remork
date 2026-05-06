package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestProgressModelMovesFromLoadingToSuccess(t *testing.T) {
	model := NewProgressModel("Sync")
	model, _ = model.Update(StepStartedMsg{Label: "fetching remote manifest", Total: 1})
	model, _ = model.Update(StepDoneMsg{})
	model, _ = model.Update(CompletedMsg{Summary: "downloaded 2 files"})

	view := model.View()
	for _, want := range []string{"Sync", "ok fetching remote manifest", "downloaded 2 files"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view should contain %q, got:\n%s", want, view)
		}
	}
}

func TestProgressModelShowsErrorsWithNextStep(t *testing.T) {
	model := NewProgressModel("Apply")
	model, _ = model.Update(StepStartedMsg{Label: "uploading changes", Total: 3})
	model, _ = model.Update(FailedMsg{
		Message: "conflict detected",
		Next:    "run remork conflict path/to/file",
	})

	view := model.View()
	for _, want := range []string{"Apply", "error conflict detected", "next run remork conflict path/to/file"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view should contain %q, got:\n%s", want, view)
		}
	}
}

func TestProgressModelUpdatesCounts(t *testing.T) {
	model := NewProgressModel("Sync")
	model, _ = model.Update(StepStartedMsg{Label: "applying remote changes", Total: 4})
	model, _ = model.Update(StepAdvancedMsg{Delta: 2})

	view := model.View()
	if !strings.Contains(view, "2/4") {
		t.Fatalf("view should contain progress count, got:\n%s", view)
	}
}

func TestProgressModelRendersProgressBarAndSummary(t *testing.T) {
	model := NewProgressModel("Sync")
	model, _ = model.Update(StepStartedMsg{Label: "download files", Total: 10})
	model, _ = model.Update(StepAdvancedMsg{Delta: 4})

	view := model.View()
	for _, want := range []string{"Sync", "download files", "[", "4/10"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view should contain %q, got:\n%s", want, view)
		}
	}
}

func TestProgressModelQuitsOnCompleted(t *testing.T) {
	model := NewProgressModel("Sync")
	_, cmd := model.Update(CompletedMsg{Summary: "done"})
	if cmd == nil {
		t.Fatal("completed progress should return a quit command")
	}
}

func TestProgressModelQuitsOnFailed(t *testing.T) {
	model := NewProgressModel("Sync")
	_, cmd := model.Update(FailedMsg{Message: "failed", Next: "retry"})
	if cmd == nil {
		t.Fatal("failed progress should return a quit command")
	}
}

func TestProgressModelCancelsOnControlKeys(t *testing.T) {
	model := NewProgressModel("Sync")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should return a quit command")
	}
	if !model.Canceled() {
		t.Fatal("esc should mark progress as canceled")
	}
}

func TestProgressModelUsesRemorkSpinner(t *testing.T) {
	model := NewProgressModel("Setup")
	frames := model.spin.Spinner.Frames
	if strings.Join(frames, "") != ".oO°Oo." {
		t.Fatalf("spinner frames = %#v", frames)
	}
}
