package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"remork/internal/output"
)

type CommandItem struct {
	Group       string
	Name        string
	Description string
	Args        []string
	HelpOnly    bool
}

type CommandMenu struct {
	title     string
	items     []CommandItem
	current   int
	selected  int
	submitted bool
	canceled  bool
	Color     output.ColorMode
}

func NewCommandMenu(title string, items []CommandItem) CommandMenu {
	copied := append([]CommandItem(nil), items...)
	return CommandMenu{title: title, items: copied, selected: -1}
}

func (m CommandMenu) Init() tea.Cmd {
	return nil
}

func (m CommandMenu) Update(msg tea.Msg) (CommandMenu, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.canceled = true
			return m, tea.Quit
		case tea.KeyUp:
			m.move(-1)
		case tea.KeyDown:
			m.move(1)
		case tea.KeyEnter:
			if len(m.items) == 0 {
				m.canceled = true
				return m, tea.Quit
			}
			m.selected = m.current
			m.submitted = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *CommandMenu) move(delta int) {
	if len(m.items) == 0 {
		return
	}
	m.current = (m.current + delta + len(m.items)) % len(m.items)
}

func (m CommandMenu) View() string {
	var b strings.Builder
	title := m.bold(m.title)
	fmt.Fprintf(&b, "%s\n\n", title)
	if len(m.items) == 0 {
		fmt.Fprintln(&b, "  no commands available")
		return b.String()
	}
	lastGroup := ""
	for i, item := range m.items {
		if item.Group != "" && item.Group != lastGroup {
			if lastGroup != "" {
				fmt.Fprintln(&b)
			}
			fmt.Fprintf(&b, "%s\n", m.bold(item.Group))
			lastGroup = item.Group
		}
		cursor := "  "
		name := item.Name
		if i == m.current {
			cursor = ">> "
			name += " [selected]"
			name = m.bold(name)
		}
		description := item.Description
		if item.HelpOnly {
			description += " (opens help)"
		}
		fmt.Fprintf(&b, "%s%-24s %s\n", cursor, name, description)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "up/down choose  enter open selected  esc cancel")
	return b.String()
}

func (m CommandMenu) Selected() string {
	if m.selected < 0 || m.selected >= len(m.items) {
		return ""
	}
	return m.items[m.selected].Name
}

func (m CommandMenu) SelectedArgs() []string {
	if m.selected < 0 || m.selected >= len(m.items) {
		return nil
	}
	if len(m.items[m.selected].Args) == 0 {
		args := []string{m.items[m.selected].Name}
		if m.items[m.selected].HelpOnly {
			args = append(args, "--help")
		}
		return args
	}
	args := append([]string(nil), m.items[m.selected].Args...)
	if m.items[m.selected].HelpOnly {
		args = append(args, "--help")
	}
	return args
}

func (m CommandMenu) bold(text string) string {
	if !m.allowColor() {
		return text
	}
	return lipgloss.NewStyle().Bold(true).Render(text)
}

func (m CommandMenu) allowColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	switch m.Color {
	case output.ColorNever:
		return false
	default:
		return true
	}
}

func (m CommandMenu) Submitted() bool {
	return m.submitted
}

func (m CommandMenu) Canceled() bool {
	return m.canceled
}

type teaCommandMenu struct {
	model CommandMenu
}

func (m teaCommandMenu) Init() tea.Cmd {
	return m.model.Init()
}

func (m teaCommandMenu) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.model.Update(msg)
	m.model = next
	return m, cmd
}

func (m teaCommandMenu) View() string {
	return m.model.View()
}

func RunCommandMenu(model CommandMenu, opts ...tea.ProgramOption) (CommandMenu, error) {
	final, err := tea.NewProgram(teaCommandMenu{model: model}, opts...).Run()
	if err != nil {
		return model, err
	}
	wrapped, ok := final.(teaCommandMenu)
	if !ok {
		return model, fmt.Errorf("unexpected menu model result %T", final)
	}
	return wrapped.model, nil
}

var _ tea.Model = teaCommandMenu{}
