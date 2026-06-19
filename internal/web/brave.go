package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const braveSearchURL = "https://api.search.brave.com/res/v1/web/search"

// BraveSearcher implements Searcher using the Brave Search API.
type BraveSearcher struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

// NewBraveSearcher creates a Brave search backend. The API key should come
// from BRAVE_SEARCH_API_KEY or another secret source owned by the caller.
func NewBraveSearcher(apiKey string) *BraveSearcher {
	return &BraveSearcher{
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: braveSearchURL,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *BraveSearcher) Search(ctx context.Context, query string, max int) ([]Result, error) {
	if s == nil {
		return nil, errors.New("brave searcher is nil")
	}
	if strings.TrimSpace(s.APIKey) == "" {
		return nil, errors.New("BRAVE_SEARCH_API_KEY is required for brave search")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("query is required")
	}
	if max <= 0 {
		max = 5
	}
	if max > 20 {
		max = 20
	}

	baseURL := s.BaseURL
	if baseURL == "" {
		baseURL = braveSearchURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	params := parsed.Query()
	params.Set("q", query)
	params.Set("count", strconv.Itoa(max))
	params.Set("result_filter", "web")
	params.Set("text_decorations", "false")
	parsed.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", strings.TrimSpace(s.APIKey))

	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("brave search failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded braveSearchResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(decoded.Web.Results))
	for _, item := range decoded.Web.Results {
		if strings.TrimSpace(item.URL) == "" {
			continue
		}
		results = append(results, Result{
			Title:   strings.TrimSpace(item.Title),
			URL:     strings.TrimSpace(item.URL),
			Snippet: strings.TrimSpace(item.Description),
		})
	}
	return results, nil
}

type braveSearchResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}
