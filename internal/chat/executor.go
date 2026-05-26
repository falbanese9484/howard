package chat

import (
	"os/exec"

	"github.com/falbanese9484/agent-harness/internal/types"
)

type Executor struct {
	Tools []types.Tool
}

func (e *Executor) Execute() []byte {
	cmd := exec.Command("ls", "-la")
	output, err := cmd.Output()
	if err != nil {
		return []byte(err.Error())
	}
	return output
}
