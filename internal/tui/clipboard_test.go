package tui

import "testing"

func TestClipboardNavigatesPromptHistory(t *testing.T) {
	clipboard := historyInit(3)
	clipboard.Push("first")
	clipboard.Push("second")
	clipboard.Push("third")

	if got := clipboard.Prev(); got != "third" {
		t.Fatalf("Prev() = %q, want %q", got, "third")
	}
	if got := clipboard.Prev(); got != "second" {
		t.Fatalf("Prev() = %q, want %q", got, "second")
	}
	if got := clipboard.Prev(); got != "first" {
		t.Fatalf("Prev() = %q, want %q", got, "first")
	}
	if got := clipboard.Prev(); got != "first" {
		t.Fatalf("Prev() at oldest = %q, want %q", got, "first")
	}

	if got := clipboard.Next(); got != "second" {
		t.Fatalf("Next() = %q, want %q", got, "second")
	}
	if got := clipboard.Next(); got != "third" {
		t.Fatalf("Next() = %q, want %q", got, "third")
	}
	if got := clipboard.Next(); got != "" {
		t.Fatalf("Next() past newest = %q, want empty", got)
	}
}

func TestClipboardEvictsOldestPrompt(t *testing.T) {
	clipboard := historyInit(2)
	clipboard.Push("first")
	clipboard.Push("second")
	clipboard.Push("third")

	if got := clipboard.Prev(); got != "third" {
		t.Fatalf("Prev() = %q, want %q", got, "third")
	}
	if got := clipboard.Prev(); got != "second" {
		t.Fatalf("Prev() = %q, want %q", got, "second")
	}
	if got := clipboard.Prev(); got != "second" {
		t.Fatalf("Prev() at oldest retained prompt = %q, want %q", got, "second")
	}
}

func TestClipboardZeroLimit(t *testing.T) {
	clipboard := historyInit(0)
	clipboard.Push("first")

	if got := clipboard.Prev(); got != "" {
		t.Fatalf("Prev() = %q, want empty", got)
	}
}
