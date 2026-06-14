package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/falbanese9484/agent-harness/internal/app"
	"github.com/falbanese9484/agent-harness/internal/chat"
	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/tuiapp"
	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

type IO struct {
	Stdout io.Writer
	Stderr io.Writer
}

func Run(args []string, ioStreams IO) error {
	if ioStreams.Stdout == nil {
		return errors.New("stdout is required")
	}
	if ioStreams.Stderr == nil {
		return errors.New("stderr is required")
	}

	if len(args) == 0 {
		return tuiapp.Run()
	}

	switch args[0] {
	case "tui":
		return tuiapp.Run()
	case "run":
		return runPrompt(args[1:], ioStreams)
	case "models":
		return listModels(args[1:], ioStreams)
	case "help", "-h", "--help":
		printUsage(ioStreams.Stdout)
		return nil
	default:
		printUsage(ioStreams.Stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runPrompt(args []string, ioStreams IO) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(ioStreams.Stderr)
	model := flags.String("model", "", "override the configured model")
	if err := flags.Parse(args); err != nil {
		return err
	}

	prompt := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if prompt == "" {
		return errors.New("prompt is required")
	}

	log, prov, err := loadRuntime()
	if err != nil {
		return err
	}
	if *model != "" {
		prov.SetModel(*model)
	}

	bus := chat.NewBus(log, prov)
	responses := make(chan *types.ChatResponse, 100)
	go bus.Start(responses)

	req := prov.GenerateRequest(prompt)
	req.Context = nil
	go bus.Send(req)

	for response := range responses {
		if response == nil {
			continue
		}
		if response.Done {
			fmt.Fprintln(ioStreams.Stdout)
			return nil
		}
		if response.Kind == types.ResponseKindToolStatus {
			fmt.Fprintln(ioStreams.Stderr, strings.TrimSpace(response.Response))
			continue
		}
		fmt.Fprint(ioStreams.Stdout, response.Response)
	}

	return errors.New("response channel closed")
}

func listModels(args []string, ioStreams IO) error {
	flags := flag.NewFlagSet("models", flag.ContinueOnError)
	flags.SetOutput(ioStreams.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}

	_, prov, err := loadRuntime()
	if err != nil {
		return err
	}

	models, err := prov.RetrieveModels()
	if err != nil {
		return err
	}
	for _, model := range models {
		fmt.Fprintln(ioStreams.Stdout, model.Name)
	}
	return nil
}

func loadRuntime() (*logger.Logger, types.Provider, error) {
	cfg, err := config.LoadAgentConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	log, err := app.NewLogger()
	if err != nil {
		return nil, nil, fmt.Errorf("initialize logger: %w", err)
	}

	prov, err := app.NewProvider(cfg, log)
	if err != nil {
		return nil, nil, err
	}
	return log, prov, nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  howard                 Launch the TUI
  howard tui             Launch the TUI
  howard run [flags] PROMPT
  howard models
  howard help

Run flags:
  -model string          Override the configured model
`)
}
