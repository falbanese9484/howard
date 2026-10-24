//go:build linux

package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for PID in %s", path)
	return 0
}

func assertProcessStopped(t *testing.T, pid int) {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))
	if fields[0] != "Z" && fields[0] != "X" {
		t.Fatalf("process %d still executing: %s", pid, data)
	}
}

func TestBashCancellationStopsGroupBeforeReturn(t *testing.T) {
	state := setupToolTestProject(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := state.executeBash(ctx, map[string]any{"command": "echo $$ > parent.pid; sleep 300 & echo $! > child.pid; wait"})
		finished <- err
	}()
	parent := waitForPIDFile(t, filepath.Join(state.root, "parent.pid"))
	child := waitForPIDFile(t, filepath.Join(state.root, "child.pid"))
	cancel()
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v; want canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not stop shell group")
	}
	if err := syscall.Kill(parent, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("direct shell was not reaped: %v", err)
	}
	assertProcessStopped(t, child)
	if live, err := liveProcessGroup(parent); err != nil || live {
		t.Fatalf("group still live: %v, %v", live, err)
	}
}

func TestBashCompletionCleansBackgroundChild(t *testing.T) {
	state := setupToolTestProject(t)
	finished := make(chan error, 1)
	go func() {
		_, err := state.executeBash(context.Background(), map[string]any{"command": "sleep 300 >/dev/null 2>&1 & echo $! > child.pid; printf success"})
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("background child stranded completion")
	}
	assertProcessStopped(t, waitForPIDFile(t, filepath.Join(state.root, "child.pid")))
}

func TestBashCancellationIsRunLocal(t *testing.T) {
	a, b := setupToolTestProject(t), setupToolTestProject(t)
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	finishedA, finishedB := make(chan error, 1), make(chan error, 1)
	for _, run := range []struct {
		state *ExecutionState
		ctx   context.Context
		done  chan error
	}{{a, ctxA, finishedA}, {b, ctxB, finishedB}} {
		go func() {
			_, err := run.state.executeBash(run.ctx, map[string]any{"command": "echo $$ > parent.pid; sleep 300 & wait"})
			run.done <- err
		}()
	}
	waitForPIDFile(t, filepath.Join(a.root, "parent.pid"))
	pidB := waitForPIDFile(t, filepath.Join(b.root, "parent.pid"))
	cancelA()
	select {
	case err := <-finishedA:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run A did not stop")
	}
	select {
	case err := <-finishedB:
		t.Fatalf("run B ended with A: %v", err)
	default:
	}
	if err := syscall.Kill(pidB, 0); err != nil {
		t.Fatalf("run B shell stopped: %v", err)
	}
	cancelB()
	select {
	case err := <-finishedB:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run B did not stop")
	}
}

func TestBashDeadlineStopsGroup(t *testing.T) {
	state := setupToolTestProject(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := state.executeBash(ctx, map[string]any{"command": "sleep 300 & wait"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("deadline error = %v", err)
	}
}

func TestLiveProcessGroupIncludesThreadsOfZombieLeader(t *testing.T) {
	proc := t.TempDir()
	writeStat := func(path, state string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		// Include parentheses and spaces to exercise Linux comm parsing too.
		if err := os.WriteFile(path, []byte("123 (thread (name)) "+state+" 1 123"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeStat(filepath.Join(proc, "123", "stat"), "Z")
	writeStat(filepath.Join(proc, "123", "task", "123", "stat"), "Z")
	thread := filepath.Join(proc, "123", "task", "124", "stat")
	writeStat(thread, "S")
	if live, err := liveProcessGroupAt(proc, 123); err != nil || !live {
		t.Fatalf("live thread hidden by zombie leader: %v, %v", live, err)
	}
	writeStat(thread, "Z")
	if live, err := liveProcessGroupAt(proc, 123); err != nil || live {
		t.Fatalf("all threads stopped: %v, %v", live, err)
	}
}

func TestBashRejectsIgnoredSIGCHLDBeforeLaunch(t *testing.T) {
	mask := uint64(1) << uint(syscall.SIGCHLD-1)
	if err := checkChildWaitStatus(fmt.Sprintf("Name:\ttest\nSigIgn:\t%016x\n", mask)); err == nil {
		t.Fatal("ignored SIGCHLD accepted")
	}
	if err := checkChildWaitStatus("SigIgn:\t0000000000000000\n"); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", "SigIgn:\tinvalid"} {
		if err := checkChildWaitStatus(invalid); err == nil {
			t.Fatal("unverifiable signal status accepted")
		}
	}
}
