package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"remork/internal/output"
)

func TestMenuModelSelectsCommand(t *testing.T) {
	model := NewCommandMenu("Remork", []CommandItem{
		{Name: "sync", Description: "Sync remote files", Args: []string{"sync"}},
		{Name: "status", Description: "Show workspace state", Args: []string{"status"}},
	})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := model.Selected(); got != "status" {
		t.Fatalf("selected = %q, want status", got)
	}
	if got := model.SelectedArgs(); strings.Join(got, " ") != "status" {
		t.Fatalf("selected args = %#v, want status", got)
	}
}

func TestMenuModelRoutesHelpOnlyItemsToHelp(t *testing.T) {
	model := NewCommandMenu("Remork", []CommandItem{
		{Name: "run", Description: "Run command", Args: []string{"run"}, HelpOnly: true},
	})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := strings.Join(model.SelectedArgs(), " "); got != "run --help" {
		t.Fatalf("selected args = %q, want run --help", got)
	}
}

func TestMenuModelViewLabelsHelpOnlyItems(t *testing.T) {
	model := NewCommandMenu("Remork", []CommandItem{
		{Name: "run", Description: "Run command", Args: []string{"run"}, HelpOnly: true},
	})

	view := model.View()
	if !strings.Contains(view, "opens help") {
		t.Fatalf("help-only item should be labeled in view, got:\n%s", view)
	}
	if strings.Contains(view, "enter run") {
		t.Fatalf("footer should not imply every item runs directly, got:\n%s", view)
	}
}

func TestMenuModelViewMarksCurrentItemClearly(t *testing.T) {
	model := NewCommandMenu("Remork", []CommandItem{
		{Name: "sync", Description: "Sync remote files", Args: []string{"sync"}},
		{Name: "status", Description: "Show workspace state", Args: []string{"status"}},
	})
	model.Color = output.ColorNever

	view := model.View()
	if !strings.Contains(view, ">> sync [selected]") {
		t.Fatalf("current item should have an explicit selected marker, got:\n%s", view)
	}
	if strings.Contains(view, "> sync") && !strings.Contains(view, ">> sync [selected]") {
		t.Fatalf("current item should not rely only on a bare > cursor, got:\n%s", view)
	}
}

func TestMenuModelCancel(t *testing.T) {
	model := NewCommandMenu("Remork", []CommandItem{{Name: "sync", Args: []string{"sync"}}})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !model.Canceled() {
		t.Fatal("menu should be canceled")
	}
}

func TestMenuModelViewShowsCommandDiscovery(t *testing.T) {
	model := NewCommandMenu("Remork", []CommandItem{
		{Group: "Daily", Name: "sync", Description: "Sync remote files"},
		{Group: "Inspect", Name: "doctor", Description: "Check readiness"},
	})

	view := model.View()
	for _, want := range []string{"Remork", "Daily", "sync", "Sync remote files", "doctor"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view should contain %q, got:\n%s", want, view)
		}
	}
}

func TestMenuModelHonorsColorNever(t *testing.T) {
	model := NewCommandMenu("Remork", []CommandItem{
		{Group: "Daily", Name: "sync", Description: "Sync remote files"},
	})
	model.Color = output.ColorNever

	if view := model.View(); strings.Contains(view, "\x1b[") {
		t.Fatalf("ColorNever menu should not contain ANSI, got:\n%s", view)
	}
}
