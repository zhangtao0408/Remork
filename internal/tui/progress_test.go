package tui

import (
	"strings"
	"testing"
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

func TestProgressModelUsesRemorkSpinner(t *testing.T) {
	model := NewProgressModel("Setup")
	frames := model.spin.Spinner.Frames
	if strings.Join(frames, "") != ".oO°Oo." {
		t.Fatalf("spinner frames = %#v", frames)
	}
}
