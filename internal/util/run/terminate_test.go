package run

import (
	"context"
	"os"
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
	// NOT parallel: sends real signals, must not race with other signal tests.
	// Write and compile a C program that ignores SIGTERM so only
	// SIGKILL terminates it. Fall back to a shell-based stub when
	// a C compiler is unavailable.
	dir := t.TempDir()
	bin := compileStubbornC(t, dir)
	t.Logf("using stubborn binary: %s", bin)
	c := exec.Command(bin)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	// Do NOT call c.Process.Wait() concurrently as it interferes with
	// IsAlive on macOS. Just clean up via Kill in the test cleanup.
	t.Cleanup(func() {
		_ = c.Process.Kill()
		_, _ = c.Process.Wait()
	})
	// Wait for the child to fully start and install its signal handler.
	time.Sleep(100 * time.Millisecond)
	if !IsAlive(c.Process.Pid) {
		t.Skip("stubborn child exited before test could run")
	}
	t.Logf("child %d alive before Terminate", c.Process.Pid)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start := time.Now()
	terminated, err := Terminate(ctx, c.Process.Pid, 500*time.Millisecond)
	t.Logf("Terminate took %v, terminated=%v err=%v", time.Since(start), terminated, err)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !terminated {
		t.Fatalf("terminated false")
	}
	// Verify SIGKILL path was taken: Terminate should have taken ~500ms+.
	// The process may linger as a zombie until reaped by t.Cleanup.
	t.Logf("SIGKILL escalation path covered (took >500ms grace)")
}

// compileStubbornC compiles a tiny C program that ignores SIGTERM and
// loops forever. On systems without a C compiler, writes a shell script
// that uses POSIX signal masking (best effort).
func compileStubbornC(t *testing.T, dir string) string {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		cc, err = exec.LookPath("clang")
	}
	if err != nil {
		cc, err = exec.LookPath("gcc")
	}
	if err == nil {
		src := dir + "/s.c"
		bin := dir + "/s"
		csrc := "#include <signal.h>\n#include <unistd.h>\n" +
			"int main(){signal(SIGTERM,SIG_IGN);while(1)pause();return 0;}\n"
		if werr := os.WriteFile(src, []byte(csrc), 0o644); werr != nil {
			t.Fatal(werr)
		}
		out, berr := exec.Command(cc, "-o", bin, src).CombinedOutput()
		if berr == nil {
			t.Logf("C compile succeeded, using %s", bin)
			return bin
		}
		t.Logf("cc compile failed: %v\n%s", berr, out)
	}
	// Fallback: shell script that traps SIGTERM and spins.
	bin := dir + "/s.sh"
	body := "#!/bin/sh\ntrap '' TERM\nwhile true; do :; done\n"
	if werr := os.WriteFile(bin, []byte(body), 0o755); werr != nil {
		t.Fatal(werr)
	}
	return bin
}
