package tasks

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/spacelions/j/internal/store/tasks"
)

// watchTickInterval drives the elapsed-time clock on active rows. A
// 1-second cadence keeps the SECONDS column fresh without flooding
// the renderer.
const watchTickInterval = time.Second

// tickMsg fires once per second and pushes `now` forward so the
// active-row durations rerender.
type tickMsg time.Time

// tasksMsg carries a fresh, reaped, sorted slice from the bbolt
// store back into the model.
type tasksMsg []tasks.Task

// errMsg surfaces a reload failure into the View footer without
// quitting the program — the next tick retries.
type errMsg error

// quitHintStyle dims the bottom hint so the eye lands on the table
// first; errLineStyle paints reload failures red.
var (
	quitHintStyle = lipgloss.NewStyle().Faint(true)
	errLineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// model is the bubbletea state for `j tasks` watch mode. reload and
// tick are real fields with default factories rather than test-only
// seams: the production caller uses defaultTick + a closure over the
// store, and tests inject deterministic fakes through the same
// fields. now is bumped by tickMsg so the renderer reflects the
// current second on every paint. width is bumped by tea.WindowSizeMsg
// so the table redraws to fit the user's terminal on resize.
type model struct {
	tasks  []tasks.Task
	now    time.Time
	width  int
	err    error
	reload func() ([]tasks.Task, error)
	tick   func() tea.Cmd
}

// defaultTick returns the production tea.Cmd that converts the next
// timer tick into a tickMsg. Each call mints a fresh timer so the
// caller can re-arm by re-invoking the function inside Update.
func defaultTick() tea.Cmd {
	return tea.Tick(watchTickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// reloadCmd wraps reload in a tea.Cmd so it runs off the Update
// goroutine. A nil error path produces tasksMsg; an error becomes
// errMsg without quitting so the user can recover.
func reloadCmd(reload func() ([]tasks.Task, error)) tea.Cmd {
	return func() tea.Msg {
		t, err := reload()
		if err != nil {
			return errMsg(err)
		}
		return tasksMsg(t)
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.tick(), reloadCmd(m.reload))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tickMsg:
		m.now = time.Time(v)
		return m, tea.Batch(m.tick(), reloadCmd(m.reload))
	case tasksMsg:
		m.tasks = v
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

func (m model) View() string {
	var b strings.Builder
	// strings.Builder.Write never errors so renderTable cannot fail
	// against it.
	_ = renderTable(&b, m.tasks, m.now, m.width)
	if m.err != nil {
		b.WriteString(errLineStyle.Render(fmt.Sprintf("error: %v", m.err)))
		b.WriteByte('\n')
	}
	b.WriteString(quitHintStyle.Render("press q to quit"))
	b.WriteByte('\n')
	return b.String()
}

// runWatch starts the bubbletea program with reload as the source of
// task data. The program owns the alt screen until the user presses
// q / esc / ctrl+c; on exit it returns whatever tea.Program.Run
// surfaced (nil on a clean quit). in/out are real parameters: the
// production caller wires them to os.Stdin/os.Stdout, tests pass a
// pre-loaded reader so the loop quits deterministically.
func runWatch(in io.Reader, out io.Writer, reload func() ([]tasks.Task, error)) error {
	m := model{
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
