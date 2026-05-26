package main

import (
	"fmt"

	"github.com/falbanese9484/agent-harness/internal/chat"
	"github.com/falbanese9484/agent-harness/internal/types"
)

func main() {
	executor := chat.Executor{Tools: []types.Tool{}}
	res := executor.Execute()
	fmt.Printf("Result: %s", string(res))
}
