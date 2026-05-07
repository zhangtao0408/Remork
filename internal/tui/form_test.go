package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
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

func TestFormModelUsesStaticCursorToAvoidIdleBlinkRerenders(t *testing.T) {
	model := NewFormModel("Update daemon token", []Field{
		{Key: "token", Label: "Token"},
	})

	if got := model.inputs[0].CursorMode(); got != textinput.CursorStatic {
		t.Fatalf("cursor mode = %v, want %v", got, textinput.CursorStatic)
	}
	if cmd := model.Init(); cmd != nil {
		t.Fatal("static cursor form init should not schedule blink commands")
	}
}

func TestFormModelMovesBackwardWithArrowKeys(t *testing.T) {
	model := NewFormModel("Setup", []Field{
		{Key: "host", Label: "Host"},
		{Key: "root", Label: "Root"},
	})

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model, _ = model.Update(FieldValueMsg{Key: "host", Value: "lab"})

	if got := model.Values()["host"]; got != "lab" {
		t.Fatalf("up key should return focus to first field, host = %q", got)
	}
}

func TestFormModelViewShowsSectionsAndHelp(t *testing.T) {
	model := NewFormModel("Update server", []Field{
		{Section: "Connection", Key: "host", Label: "Host", Help: "Saved Remork host profile."},
		{Section: "Daemon", Key: "port", Label: "Port", Help: "Port remorkd listens on."},
	})

	view := model.View()
	for _, want := range []string{"Connection", "Saved Remork host profile.", "Daemon", "Port remorkd listens on."} {
		if !strings.Contains(view, want) {
			t.Fatalf("view should contain %q, got:\n%s", want, view)
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
