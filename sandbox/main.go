package main

import (
	"fmt"

	"github.com/falbanese9484/agent-harness/pkg/config"
)

func main() {
	agentCfg, err := config.LoadAgentConfig()
	if err != nil {
		panic("!")
	}

	fmt.Printf("%v", agentCfg)
}
