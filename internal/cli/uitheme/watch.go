package uitheme

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	tsk "github.com/spacelions/j/internal/store/tasks"
)

const watchTickInterval = time.Second

type tickMsg time.Time

type tasksMsg []tsk.Task

type errMsg error

var (
	quitHintStyle = lipgloss.NewStyle().Faint(true)
	errLineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

type watchModel struct {
	taskRows []tsk.Task
	now      time.Time
	width    int
	err      error
	reload   func() ([]tsk.Task, error)
	tick     func() tea.Cmd
}

func defaultTick() tea.Cmd {
	return tea.Tick(watchTickInterval,
		func(t time.Time) tea.Msg { return tickMsg(t) })
}

func reloadCmd(reload func() ([]tsk.Task, error)) tea.Cmd {
	return func() tea.Msg {
		t, err := reload()
		if err != nil {
			return errMsg(err)
		}
		return tasksMsg(t)
	}
}

func (m watchModel) Init() tea.Cmd {
	return tea.Batch(m.tick(), reloadCmd(m.reload))
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tickMsg:
		m.now = time.Time(v)
		return m, tea.Batch(m.tick(), reloadCmd(m.reload))
	case tasksMsg:
		m.taskRows = v
		m.err = nil
		return m, nil
	case errMsg:
		m.err = v
		return m, nil
	case tea.WindowSizeMsg:
		m.width = v.Width
		return m, nil
	case tea.KeyMsg:
		switch v.String() {
		case "q", "Q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m watchModel) View() string {
	var b strings.Builder
	_ = WriteTaskTable(&b, m.taskRows, m.now, m.width)
	if m.err != nil {
		b.WriteString(errLineStyle.Render(fmt.Sprintf("error: %v", m.err)))
		b.WriteByte('\n')
	}
	b.WriteString(quitHintStyle.Render("press q to quit"))
	b.WriteByte('\n')
	return b.String()
}

// RunTasksWatch starts the bubbletea full-screen task table refresh loop.
// reload returns the current task slice from the store on each tick.
// The program exits when the user presses q, Esc, or Ctrl+C.
func RunTasksWatch(
	in io.Reader, out io.Writer, reload func() ([]tsk.Task, error),
) error {
	m := watchModel{
		now:    time.Now(),
		reload: reload,
		tick:   defaultTick,
	}
	_, err := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithInput(in),
		tea.WithOutput(out),
	).Run()
	return err
}
