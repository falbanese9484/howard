package main

import (
	"log"

	"github.com/falbanese9484/agent-harness/internal/tuiapp"
)

func main() {
	if err := tuiapp.Run(); err != nil {
		log.Fatal(err)
	}
}
