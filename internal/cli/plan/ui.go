package plan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// UI lets the planner ask the user questions. The default implementation
// renders Bubble Tea prompts on the terminal; tests substitute a scripted
// fake to avoid touching stdin.
type UI interface {
	AskTarget(ctx context.Context) (string, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
}

// ErrCancelled is returned by the UI when the user aborts a prompt.
var ErrCancelled = errors.New("plan: cancelled by user")

// bubbleUI is the Bubble Tea backed implementation of UI. The methods
// drive real tea.Program instances and so are not exercised by unit
// tests; orchestration logic is unit-tested through the UI interface
// using a scripted fake (see plan_test.go).
type bubbleUI struct {
	in  io.Reader
	out io.Writer
}

func newBubbleUI(in io.Reader, out io.Writer) *bubbleUI {
	return &bubbleUI{in: in, out: out}
}

func (b *bubbleUI) AskTarget(ctx context.Context) (string, error) {
	final, err := b.run(ctx, newTargetModel())
	if err != nil {
		return "", err
	}
	tm := final.(targetModel)
	if tm.cancelled {
		return "", ErrCancelled
	}
	if tm.value == "" {
		return "", errors.New("plan: no markdown file location provided")
	}
	return tm.value, nil
}

func (b *bubbleUI) SelectTool(ctx context.Context, options []string) (string, error) {
	return b.choose(ctx, "Select planning tool", options)
}

func (b *bubbleUI) SelectModel(ctx context.Context, options []string) (string, error) {
	return b.choose(ctx, "Select Cursor model", options)
}

func (b *bubbleUI) choose(ctx context.Context, title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("%s: no options available", strings.ToLower(title))
	}
	final, err := b.run(ctx, newSelectModel(title, options))
	if err != nil {
		return "", err
	}
	sm := final.(selectModel)
	if sm.cancelled {
		return "", ErrCancelled
	}
	return sm.options[sm.cursor], nil
}

func (b *bubbleUI) run(ctx context.Context, m tea.Model) (tea.Model, error) {
	p := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithInput(b.in),
		tea.WithOutput(b.out),
	)
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("ui: %w", err)
	}
	return final, nil
}

// targetModel asks for a markdown file path.
type targetModel struct {
	input     textinput.Model
	value     string
	cancelled bool
	submitted bool
}

func newTargetModel() targetModel {
	ti := textinput.New()
	ti.Placeholder = "/path/to/feature.md"
	ti.Focus()
	ti.Prompt = "> "
	ti.Width = 60
	return targetModel{input: ti}
}

func (m targetModel) Init() tea.Cmd { return textinput.Blink }

func (m targetModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEnter:
			m.value = strings.TrimSpace(m.input.Value())
			m.submitted = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m targetModel) View() string {
	return fmt.Sprintf("Markdown file location:\n%s\n\nEnter to confirm, Esc to cancel.\n", m.input.View())
}

// selectModel is a single-column list selector.
type selectModel struct {
	title     string
	options   []string
	cursor    int
	cancelled bool
	submitted bool
}

func newSelectModel(title string, options []string) selectModel {
	return selectModel{title: title, options: options}
}

func (m selectModel) Init() tea.Cmd { return nil }

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "enter":
			m.submitted = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectModel) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", m.title)
	for i, opt := range m.options {
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		fmt.Fprintf(&b, "%s%s\n", marker, opt)
	}
	b.WriteString("\nEnter to select, q/Esc to cancel.\n")
	return b.String()
}
