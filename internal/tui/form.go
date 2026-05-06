package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	ErrCanceled   = errors.New("form canceled")
	ErrIncomplete = errors.New("form incomplete")
)

type Field struct {
	Key         string
	Label       string
	Placeholder string
	Initial     string
}

type FieldValueMsg struct {
	Key   string
	Value string
}

type FormModel struct {
	title     string
	fields    []Field
	inputs    []textinput.Model
	current   int
	submitted bool
	canceled  bool
}

func NewFormModel(title string, fields []Field) FormModel {
	inputs := make([]textinput.Model, len(fields))
	for i, field := range fields {
		input := textinput.New()
		input.Placeholder = field.Placeholder
		input.Prompt = field.Label + ": "
		input.Width = 60
		if field.Initial != "" {
			input.SetValue(field.Initial)
		}
		if i == 0 {
			_ = input.Focus()
		}
		inputs[i] = input
	}
	return FormModel{title: title, fields: append([]Field(nil), fields...), inputs: inputs}
}

func (m FormModel) Init() tea.Cmd {
	if len(m.inputs) == 0 {
		return nil
	}
	return m.inputs[m.current].Focus()
}

func (m FormModel) Update(msg tea.Msg) (FormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case FieldValueMsg:
		for i, field := range m.fields {
			if field.Key == msg.Key {
				m.inputs[i].SetValue(msg.Value)
				return m, nil
			}
		}
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.canceled = true
			return m, tea.Quit
		case tea.KeyEnter:
			if len(m.inputs) == 0 || m.current == len(m.inputs)-1 {
				m.submitted = true
				return m, tea.Quit
			}
			m.move(1)
			return m, nil
		case tea.KeyTab:
			m.move(1)
			return m, nil
		case tea.KeyShiftTab:
			m.move(-1)
			return m, nil
		}
	}
	if len(m.inputs) == 0 {
		return m, nil
	}
	var cmd tea.Cmd
	m.inputs[m.current], cmd = m.inputs[m.current].Update(msg)
	return m, cmd
}

func (m *FormModel) move(delta int) {
	if len(m.inputs) == 0 {
		return
	}
	m.inputs[m.current].Blur()
	m.current = (m.current + delta + len(m.inputs)) % len(m.inputs)
	_ = m.inputs[m.current].Focus()
}

func (m FormModel) View() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Render(m.title)
	fmt.Fprintf(&b, "%s\n\n", title)
	for i, input := range m.inputs {
		prefix := "  "
		if i == m.current {
			prefix = "> "
		}
		fmt.Fprintf(&b, "%s%s\n", prefix, input.View())
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "enter next/submit  tab switch  esc cancel")
	return b.String()
}

func (m FormModel) Values() map[string]string {
	values := make(map[string]string, len(m.fields))
	for i, field := range m.fields {
		values[field.Key] = strings.TrimSpace(m.inputs[i].Value())
	}
	return values
}

func (m FormModel) Submitted() bool {
	return m.submitted
}

func (m FormModel) Canceled() bool {
	return m.canceled
}

func formResult(m FormModel) (map[string]string, error) {
	if m.Canceled() {
		return nil, ErrCanceled
	}
	if !m.Submitted() {
		return nil, ErrIncomplete
	}
	return m.Values(), nil
}

type teaFormModel struct {
	model FormModel
}

func (m teaFormModel) Init() tea.Cmd {
	return m.model.Init()
}

func (m teaFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.model.Update(msg)
	m.model = next
	return m, cmd
}

func (m teaFormModel) View() string {
	return m.model.View()
}

func RunForm(model FormModel, opts ...tea.ProgramOption) (map[string]string, error) {
	final, err := tea.NewProgram(teaFormModel{model: model}, opts...).Run()
	if err != nil {
		return nil, err
	}
	wrapped, ok := final.(teaFormModel)
	if !ok {
		return nil, fmt.Errorf("unexpected form model result %T", final)
	}
	return formResult(wrapped.model)
}

var _ tea.Model = teaFormModel{}
