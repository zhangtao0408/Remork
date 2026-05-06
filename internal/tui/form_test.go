package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFormModelCollectsValuesAndSubmits(t *testing.T) {
	model := NewFormModel("Init workspace", []Field{
		{Key: "host", Label: "Host"},
		{Key: "root", Label: "Remote root"},
	})

	model, _ = model.Update(FieldValueMsg{Key: "host", Value: "lab"})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = model.Update(FieldValueMsg{Key: "root", Value: "/home/me/project"})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !model.Submitted() {
		t.Fatal("form should be submitted")
	}
	values := model.Values()
	if values["host"] != "lab" || values["root"] != "/home/me/project" {
		t.Fatalf("values = %#v", values)
	}
}

func TestFormModelUsesInitialValues(t *testing.T) {
	model := NewFormModel("Daemon upgrade", []Field{
		{Key: "host", Label: "Host", Initial: "lab"},
		{Key: "roots", Label: "Allowed roots", Initial: "/home/me"},
	})

	values := model.Values()
	if values["host"] != "lab" || values["roots"] != "/home/me" {
		t.Fatalf("initial values = %#v", values)
	}
	view := model.View()
	for _, want := range []string{"lab", "/home/me"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view should contain initial value %q, got:\n%s", want, view)
		}
	}
}

func TestFormModelCancel(t *testing.T) {
	model := NewFormModel("Install daemon", []Field{{Key: "host", Label: "Host"}})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !model.Canceled() {
		t.Fatal("form should be canceled")
	}
	if _, err := formResult(model); !errors.Is(err, ErrCanceled) {
		t.Fatalf("formResult err = %v, want ErrCanceled", err)
	}
}

func TestFormResultRequiresSubmit(t *testing.T) {
	model := NewFormModel("Init workspace", []Field{{Key: "host", Label: "Host"}})
	model, _ = model.Update(FieldValueMsg{Key: "host", Value: "lab"})

	if _, err := formResult(model); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("formResult err = %v, want ErrIncomplete", err)
	}
}

func TestFormModelViewShowsTitleAndFields(t *testing.T) {
	model := NewFormModel("Init workspace", []Field{
		{Key: "host", Label: "Host", Placeholder: "my-lab"},
	})

	view := model.View()
	for _, want := range []string{"Init workspace", "Host", "my-lab"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view should contain %q, got:\n%s", want, view)
		}
	}
}
