//go:build linux

package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// runBash owns a distinct process group. The direct shell is deliberately not
// reaped until group cleanup finishes: its reserved PID prevents cleanup from
// signaling a recycled, unrelated process group. No process-global subreaper or
// signal handler is installed. The host must not externally reap these children
// or configure SA_NOCLDWAIT. Descendants that deliberately change groups or
// sessions are outside this mechanism; it is not a security sandbox.
func runBash(ctx context.Context, root, command string, _ bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// This worker must exclusively own waits for its children. Ignored SIGCHLD
	// auto-reaps children and would invalidate the reserved-group-ID guarantee.
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return nil, fmt.Errorf("child-wait verification unavailable: %w", err)
	}
	if err := checkChildWaitStatus(string(status)); err != nil {
		return nil, err
	}
	// Fail before launch when this platform's cleanup verification is unavailable.
	if _, err := liveProcessGroup(syscall.Getpgrp()); err != nil {
		return nil, fmt.Errorf("process-group verification unavailable: %w", err)
	}
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pid := cmd.Process.Pid
	exited := make(chan error, 1)
	go func() {
		var info unix.Siginfo
		var err error
		for {
			err = unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
			if !errors.Is(err, syscall.EINTR) {
				break
			}
		}
		exited <- err
	}()

	var observeErr error
	observed := false
	select {
	case observeErr = <-exited:
		observed = true
	case <-ctx.Done():
	}
	var killErr error
	signalGroup := func() {
		// Observe a wait failure before using the numeric group ID. ECHILD means
		// an unsupported external reaper may have released its identity. In that
		// case only passive verification is safe; completion can remain pending.
		if !observed {
			select {
			case observeErr = <-exited:
				observed = true
			default:
			}
		}
		if observed && observeErr != nil {
			return
		}
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			killErr = err
		}
	}
	// Also stop same-group background children after natural shell exit. Otherwise
	// a successful tool could leave execution running beyond its terminal event.
	signalGroup()
	// SIGKILL delivery is asynchronous. Wait until every observed group member
	// has stopped before returning; zombies cannot execute. The direct shell is
	// reaped below, while orphaned descendant zombies belong to their OS reaper.
	for {
		live, err := liveProcessGroup(pid)
		if err == nil && !live {
			break
		}
		// A descendant can fork while the first signal is being delivered. The
		// unreaped leader still reserves this group ID, so repeat group cleanup
		// until no member can execute or fork again.
		signalGroup()
		// If verification becomes unavailable after launch, keep ownership rather
		// than falsely report stopped. Preflight rejects unavailable /proc before
		// side effects; a transient enumeration failure is safe to retry here.
		time.Sleep(5 * time.Millisecond)
	}
	if !observed {
		observeErr = <-exited
	}
	waitErr := cmd.Wait() // Reap the owned shell and join output-copy goroutines.
	if err := errors.Join(killErr, observeErr); err != nil {
		return output.Bytes(), fmt.Errorf("process-group cleanup failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return output.Bytes(), err
	}
	return output.Bytes(), waitErr
}

func liveProcessGroup(group int) (bool, error) {
	return liveProcessGroupAt("/proc", group)
}

func liveProcessGroupAt(proc string, group int) (bool, error) {
	entries, err := os.ReadDir(proc)
	if err != nil {
		return false, err
	}
	live := false
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		path := filepath.Join(proc, entry.Name())
		_, pgid, err := processStat(filepath.Join(path, "stat"))
		if processGone(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if pgid != group {
			continue
		}
		// A zombie thread-group leader can still have executing sibling threads
		// (for example after pthread_exit). Check every task, not just its leader.
		tasks, err := os.ReadDir(filepath.Join(path, "task"))
		if processGone(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		for _, task := range tasks {
			state, _, err := processStat(filepath.Join(path, "task", task.Name(), "stat"))
			if processGone(err) {
				continue
			}
			if err != nil {
				return false, err
			}
			if state != "Z" && state != "X" {
				live = true
			}
		}
	}
	return live, nil
}

func processGone(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

func processStat(path string) (state string, pgid int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	// comm is parenthesized and may itself contain spaces or parentheses.
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", 0, fmt.Errorf("invalid process stat: %s", path)
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 3 {
		return "", 0, fmt.Errorf("short process stat: %s", path)
	}
	pgid, err = strconv.Atoi(fields[2])
	return fields[0], pgid, err
}

func checkChildWaitStatus(status string) error {
	for _, line := range strings.Split(status, "\n") {
		if !strings.HasPrefix(line, "SigIgn:") {
			continue
		}
		mask, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "SigIgn:")), 16, 64)
		if err != nil {
			return fmt.Errorf("invalid ignored-signal mask: %w", err)
		}
		if mask&(uint64(1)<<uint(syscall.SIGCHLD-1)) != 0 {
			return errors.New("bash cleanup requires exclusive child wait ownership; SIGCHLD is ignored")
		}
		return nil
	}
	return errors.New("child-wait verification unavailable: missing SigIgn")
}
