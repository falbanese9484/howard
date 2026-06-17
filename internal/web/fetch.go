package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// FetchResult is the converted body of a fetched URL.
type FetchResult struct {
	URL         string
	Status      int
	ContentType string
	Bytes       int64
	Body        string // text or markdown depending on requested format
}

// FetchOptions configures a single Fetch call.
type FetchOptions struct {
	Format    string        // "markdown" or "text"; defaults to "markdown"
	MaxBytes  int64         // hard cap on response size; 0 means no cap
	Timeout   time.Duration // overall request timeout; 0 means use the context
	UserAgent string        // overrides the default User-Agent
}

const defaultUserAgent = "howard/0.1 (+https://github.com/falbanese9484/agent-harness)"

// Fetch performs an HTTP GET and converts the response body to text or
// markdown. It enforces a size cap and accepts only text-shaped content
// types.
func Fetch(ctx context.Context, rawURL string, opts FetchOptions) (*FetchResult, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, errors.New("url is required")
	}

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,text/plain,application/xhtml+xml;q=0.9,*/*;q=0.5")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if !isTextContentType(contentType) {
		return nil, fmt.Errorf("unsupported content type %q; only text/html and text/plain are allowed", contentType)
	}

	var reader io.Reader = resp.Body
	if opts.MaxBytes > 0 {
		// Read one extra byte so we can detect truncation.
		reader = io.LimitReader(resp.Body, opts.MaxBytes+1)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	truncated := opts.MaxBytes > 0 && int64(len(raw)) > opts.MaxBytes
	if truncated {
		raw = raw[:opts.MaxBytes]
	}

	format := opts.Format
	if format == "" {
		format = "markdown"
	}

	var body string
	switch {
	case strings.HasPrefix(strings.ToLower(contentType), "text/plain"):
		body = string(raw)
	default:
		body, err = convertHTML(string(raw), format)
		if err != nil {
			return nil, err
		}
	}

	if truncated {
		body += fmt.Sprintf("\n\n[truncated to %d bytes]", opts.MaxBytes)
	}

	return &FetchResult{
		URL:         rawURL,
		Status:      resp.StatusCode,
		ContentType: contentType,
		Bytes:       int64(len(raw)),
		Body:        body,
	}, nil
}

func isTextContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		// Be permissive when servers omit the header.
		return true
	}
	if i := strings.Index(ct, ";"); i != -1 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "text/html", "text/plain", "application/xhtml+xml", "text/markdown":
		return true
	}
	return false
}

// convertHTML walks the HTML tree and emits either plain text or a thin
// markdown rendering. The goal is to produce model-friendly content; we do
// not aim for perfect fidelity.
func convertHTML(src, format string) (string, error) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", err
	}

	w := &htmlWriter{markdown: format == "markdown"}
	w.walk(doc)
	return strings.TrimSpace(collapseBlankLines(w.b.String())), nil
}

type htmlWriter struct {
	b        strings.Builder
	markdown bool
	skip     int // depth of <script>/<style>/<head> we are inside
}

func (w *htmlWriter) walk(n *html.Node) {
	switch n.Type {
	case html.ElementNode:
		switch n.Data {
		case "script", "style", "noscript", "head":
			w.skip++
			defer func() { w.skip-- }()
		}
	case html.TextNode:
		if w.skip == 0 {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				w.b.WriteString(text)
				w.b.WriteByte(' ')
			}
		}
		return
	case html.CommentNode:
		return
	}

	if n.Type == html.ElementNode && w.skip == 0 {
		w.openTag(n)
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c)
	}

	if n.Type == html.ElementNode && w.skip == 0 {
		w.closeTag(n)
	}
}

func (w *htmlWriter) openTag(n *html.Node) {
	switch n.Data {
	case "p", "div", "section", "article", "header", "footer", "tr", "br":
		w.newline()
	case "h1":
		w.newline()
		if w.markdown {
			w.b.WriteString("# ")
		}
	case "h2":
		w.newline()
		if w.markdown {
			w.b.WriteString("## ")
		}
	case "h3":
		w.newline()
		if w.markdown {
			w.b.WriteString("### ")
		}
	case "h4", "h5", "h6":
		w.newline()
		if w.markdown {
			w.b.WriteString("#### ")
		}
	case "li":
		w.newline()
		if w.markdown {
			w.b.WriteString("- ")
		}
	case "code":
		if w.markdown {
			w.b.WriteByte('`')
		}
	case "pre":
		w.newline()
		if w.markdown {
			w.b.WriteString("```\n")
		}
	case "strong", "b":
		if w.markdown {
			w.b.WriteString("**")
		}
	case "em", "i":
		if w.markdown {
			w.b.WriteByte('*')
		}
	}
}

func (w *htmlWriter) closeTag(n *html.Node) {
	switch n.Data {
	case "p", "div", "section", "article", "header", "footer", "h1", "h2", "h3", "h4", "h5", "h6":
		w.newline()
	case "code":
		if w.markdown {
			w.b.WriteByte('`')
		}
	case "pre":
		if w.markdown {
			w.b.WriteString("\n```\n")
		}
		w.newline()
	case "strong", "b":
		if w.markdown {
			w.b.WriteString("**")
		}
	case "em", "i":
		if w.markdown {
			w.b.WriteByte('*')
		}
	case "a":
		if w.markdown {
			href := attr(n, "href")
			if href != "" {
				fmt.Fprintf(&w.b, " (%s)", href)
			}
		}
	}
}

func (w *htmlWriter) newline() {
	if w.b.Len() == 0 {
		return
	}
	if !strings.HasSuffix(w.b.String(), "\n") {
		w.b.WriteByte('\n')
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func collapseBlankLines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}
