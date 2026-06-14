package main

import (
	"fmt"
	"os"

	"github.com/falbanese9484/agent-harness/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], cli.IO{Stdout: os.Stdout, Stderr: os.Stderr}); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
