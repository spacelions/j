package tasks

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/spacelions/j/internal/store/tasks"
)

// noopTick returns a tea.Cmd that does nothing when invoked. It keeps
// the model deterministic in tests by preventing any real timers from
// firing while we drive Update directly.
func noopTick() tea.Cmd {
	return func() tea.Msg { return nil }
}

func newTestModel(tasks []tasks.Task, now time.Time) model {
	return model{
		tasks:  tasks,
		now:    now,
		reload: func() ([]tasks.Task, error) { return tasks, nil },
		tick:   noopTick,
	}
}

func TestModel_Init_ReturnsCmd(t *testing.T) {
	m := newTestModel(nil, time.Now())
	if m.Init() == nil {
		t.Fatal("Init must return a tea.Cmd batch (tick + reload)")
	}
}

func TestModel_TickAdvancesNowAndReturnsCmd(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := newTestModel(nil, start)
	next := start.Add(time.Second)
	updated, cmd := m.Update(tickMsg(next))
	mm := updated.(model)
	if !mm.now.Equal(next) {
		t.Fatalf("now = %v, want %v", mm.now, next)
	}
	if cmd == nil {
		t.Fatal("tickMsg should return a follow-up tea.Cmd")
	}
}

func TestModel_TasksMsgReplacesAndClearsErr(t *testing.T) {
	m := newTestModel(nil, time.Now())
	m.err = errors.New("stale")
	fresh := []tasks.Task{{ID: "abc", Status: tasks.StatusPlanDone}}
	updated, cmd := m.Update(tasksMsg(fresh))
	mm := updated.(model)
	if !reflect.DeepEqual(mm.tasks, fresh) {
		t.Fatalf("tasks = %#v, want %#v", mm.tasks, fresh)
	}
	if mm.err != nil {
		t.Fatalf("err should clear, got %v", mm.err)
	}
	if cmd != nil {
		t.Fatalf("tasksMsg should not schedule a follow-up cmd, got %T", cmd)
	}
}

func TestModel_ErrMsgDoesNotQuit(t *testing.T) {
	m := newTestModel(nil, time.Now())
	want := errors.New("reload broke")
	updated, cmd := m.Update(errMsg(want))
	mm := updated.(model)
	if !errors.Is(mm.err, want) {
		t.Fatalf("err = %v, want %v", mm.err, want)
	}
	if cmd != nil {
		t.Fatal("errMsg must not return tea.Quit")
	}
}

func TestModel_QuitKeys(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyRunes, Runes: []rune{'Q'}},
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyEsc},
	}
	for _, k := range keys {
		t.Run(k.String(), func(t *testing.T) {
			m := newTestModel(nil, time.Now())
			_, cmd := m.Update(k)
			if cmd == nil {
				t.Fatalf("key %q should return tea.Quit", k.String())
			}
			if msg := cmd(); msg == nil {
				t.Fatalf("quit cmd produced nil msg for key %q", k.String())
			} else if _, ok := msg.(tea.QuitMsg); !ok {
				t.Fatalf("key %q produced %T, want tea.QuitMsg", k.String(), msg)
			}
		})
	}
}

func TestModel_OtherKeyIgnored(t *testing.T) {
	m := newTestModel(nil, time.Now())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Fatal("non-quit key should be a no-op")
	}
}

// TestModel_WindowSizeMsgUpdatesWidth pins the resize behaviour: the
// model captures the new terminal width so the next View call passes
// it into renderTable and the table redraws to fit.
func TestModel_WindowSizeMsgUpdatesWidth(t *testing.T) {
	m := newTestModel(nil, time.Now())
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	mm := updated.(model)
	if mm.width != 60 {
		t.Fatalf("width = %d, want 60", mm.width)
	}
	if cmd != nil {
		t.Fatalf("WindowSizeMsg should not schedule a follow-up cmd, got %T", cmd)
	}
}

func TestModel_View_WithTasksAndQuitHint(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)
	begin := now.Add(-80 * time.Second)
	tasks := []tasks.Task{{
		ID:           "active-1",
		Status:       tasks.StatusPlanning,
		InvokedTool:  "cursor",
		InvokedModel: "sonnet-4",
		Summary:      "draft idea",
		PlanBeginAt:  &begin,
	}}
	m := newTestModel(tasks, now)
	out := m.View()
	if !strings.Contains(out, "planning(1m:20s)") {
		t.Fatalf("expected ticking status row: %q", out)
	}
	if !strings.Contains(out, "press q to quit") {
		t.Fatalf("expected quit hint: %q", out)
	}
}

func TestModel_View_WithErrFooter(t *testing.T) {
	m := newTestModel(nil, time.Now())
	m.err = errors.New("kapow")
	out := m.View()
	if !strings.Contains(out, "error: kapow") {
		t.Fatalf("expected error footer: %q", out)
	}
	if !strings.Contains(out, "press q to quit") {
		t.Fatalf("error state should still show quit hint: %q", out)
	}
}

func TestReloadCmd_TasksMsgOnSuccess(t *testing.T) {
	want := []tasks.Task{{ID: "abc", Status: tasks.StatusPlanning}}
	cmd := reloadCmd(func() ([]tasks.Task, error) { return want, nil })
	msg := cmd()
	got, ok := msg.(tasksMsg)
	if !ok {
		t.Fatalf("expected tasksMsg, got %T", msg)
	}
	if !reflect.DeepEqual([]tasks.Task(got), want) {
		t.Fatalf("tasks = %#v, want %#v", got, want)
	}
}

func TestReloadCmd_ErrMsgOnFailure(t *testing.T) {
	want := errors.New("boom")
	cmd := reloadCmd(func() ([]tasks.Task, error) { return nil, want })
	msg := cmd()
	got, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
	if !errors.Is(error(got), want) {
		t.Fatalf("err = %v, want %v", error(got), want)
	}
}

func TestDefaultTick_ProducesTickMsg(t *testing.T) {
	cmd := defaultTick()
	if cmd == nil {
		t.Fatal("defaultTick returned nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(tickMsg); !ok {
		t.Fatalf("default tick produced %T, want tickMsg", msg)
	}
}

// TestRunWatch_QuitsOnInput drives the runWatch entrypoint end-to-
// end against a pre-loaded reader and a discard-style output. The
// 'q' byte parses as KeyRunes('q') so the model returns tea.Quit and
// runWatch exits cleanly. A guard timeout fails the test rather than
// hanging if bubbletea ever stops handling the byte.
func TestRunWatch_QuitsOnInput(t *testing.T) {
	out := &bytes.Buffer{}
	reload := func() ([]tasks.Task, error) { return nil, nil }
	done := make(chan error, 1)
	go func() {
		done <- runWatch(strings.NewReader("q"), out, reload)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runWatch: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runWatch did not quit within 5s")
	}
}
