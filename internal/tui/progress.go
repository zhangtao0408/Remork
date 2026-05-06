package tui

import (
	"bytes"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"remork/internal/output"
)

type StepState string

const (
	StepLoading StepState = "loading"
	StepOK      StepState = "ok"
	StepFailed  StepState = "failed"
)

type StepStartedMsg struct {
	Label string
	Total int64
}

type StepAdvancedMsg struct {
	Delta int64
}

type StepDoneMsg struct{}

type CompletedMsg struct {
	Summary string
}

type FailedMsg struct {
	Message string
	Next    string
}

type ProgressModel struct {
	Title    string
	Step     string
	State    StepState
	Current  int64
	Total    int64
	Summary  string
	Error    string
	Next     string
	canceled bool
	spin     spinner.Model
}

func NewProgressModel(title string) ProgressModel {
	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: RemorkSpinnerFrames(),
		FPS:    time.Second / 8,
	}
	return ProgressModel{Title: title, State: StepLoading, spin: s}
}

func (m ProgressModel) Init() tea.Cmd {
	return m.spin.Tick
}

func (m ProgressModel) Update(msg tea.Msg) (ProgressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case StepStartedMsg:
		m.Step = msg.Label
		m.Total = msg.Total
		m.Current = 0
		m.State = StepLoading
		m.Error = ""
		m.Next = ""
	case StepAdvancedMsg:
		m.Current += msg.Delta
		if m.Total > 0 && m.Current > m.Total {
			m.Current = m.Total
		}
	case StepDoneMsg:
		m.State = StepOK
		if m.Total > 0 {
			m.Current = m.Total
		}
	case CompletedMsg:
		m.State = StepOK
		m.Summary = msg.Summary
		return m, tea.Quit
	case FailedMsg:
		m.State = StepFailed
		m.Error = msg.Message
		m.Next = msg.Next
		return m, tea.Quit
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.canceled = true
			m.State = StepFailed
			m.Error = "cancelled"
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m ProgressModel) View() string {
	var buf bytes.Buffer
	r := output.NewPlainRenderer(&buf, output.PlainOptions{Color: output.ColorNever})
	r.Section(m.Title)
	switch m.State {
	case StepFailed:
		r.Error(m.Error, m.Next)
	case StepOK:
		if m.Step != "" {
			if m.Total > 1 {
				r.ProgressBar(m.Step, m.Current, m.Total)
			}
			r.Success(progressLabel(m.Step, m.Current, m.Total))
		}
	default:
		if m.Step != "" {
			if m.Total > 1 {
				r.ProgressBar(m.Step, m.Current, m.Total)
			} else {
				r.Step(progressLabel(m.Step, m.Current, m.Total))
			}
		}
	}
	if m.Summary != "" {
		r.Success(m.Summary)
	}
	return buf.String()
}

func progressLabel(label string, current, total int64) string {
	if total <= 1 {
		return label
	}
	return fmt.Sprintf("%s %d/%d", label, current, total)
}

func (m ProgressModel) Canceled() bool {
	return m.canceled
}

type teaProgressModel struct {
	model ProgressModel
}

func (m teaProgressModel) Init() tea.Cmd {
	return m.model.Init()
}

func (m teaProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.model.Update(msg)
	m.model = next
	return m, cmd
}

func (m teaProgressModel) View() string {
	return m.model.View()
}

func RunProgress(model ProgressModel, opts ...tea.ProgramOption) (ProgressModel, error) {
	final, err := tea.NewProgram(teaProgressModel{model: model}, opts...).Run()
	if err != nil {
		return model, err
	}
	if wrapped, ok := final.(teaProgressModel); ok {
		return wrapped.model, nil
	}
	return model, nil
}

var _ tea.Model = teaProgressModel{}
