//go:build !linux

package provider

import (
	"context"
	"errors"
	"os/exec"
)

func runBash(ctx context.Context, root, command string, worker bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if worker {
		return nil, errors.New("worker bash requires Linux process-group cleanup")
	}
	// Preserve legacy CLI/TUI behavior outside Linux. This does not promise
	// descendant cleanup, so the in-process worker executor cannot use it.
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = root
	return cmd.CombinedOutput()
}
