package run

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestTerminate_PidZero(t *testing.T) {
	t.Parallel()
	terminated, err := Terminate(t.Context(), 0, time.Second)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if terminated {
		t.Fatalf("terminated true for pid 0")
	}
}

func TestTerminate_AlreadyDead(t *testing.T) {
	t.Parallel()
	c := exec.Command("true")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	terminated, err := Terminate(t.Context(), c.Process.Pid, time.Second)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if terminated {
		t.Fatalf("terminated true for already-dead pid")
	}
}

func TestTerminate_LiveSleep(t *testing.T) {
	t.Parallel()
	c := exec.Command("sleep", "60")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = c.Wait() }()
	t.Cleanup(func() {
		_ = c.Process.Kill()
	})
	terminated, err := Terminate(t.Context(), c.Process.Pid, 2*time.Second)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !terminated {
		t.Fatalf("terminated false")
	}
	if IsAlive(c.Process.Pid) {
		t.Fatalf("still alive")
	}
}

func TestTerminate_StubbornChildEscalates(t *testing.T) {
	t.Parallel()
	c := exec.Command("bash", "-c", `trap "" TERM; sleep 60`)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = c.Wait() }()
	t.Cleanup(func() {
		_ = c.Process.Kill()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	terminated, err := Terminate(ctx, c.Process.Pid, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !terminated {
		t.Fatalf("terminated false")
	}
	deadline := time.Now().Add(2 * time.Second)
	for IsAlive(c.Process.Pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if IsAlive(c.Process.Pid) {
		t.Fatalf("still alive after escalation")
	}
}
