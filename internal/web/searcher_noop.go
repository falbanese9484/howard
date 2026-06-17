package web

import (
	"context"
	"errors"
)

// NoopSearcher is the default Searcher used when no backend is configured.
// It always returns an explanatory error so the user knows how to enable
// search.
type NoopSearcher struct{}

func (NoopSearcher) Search(_ context.Context, _ string, _ int) ([]Result, error) {
	return nil, errors.New("no search backend wired up; future versions will support brave/tavily/duckduckgo")
}
