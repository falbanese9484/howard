package design10

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/falbanese9484/agent-harness/internal/domain"
)

// These are data/lifecycle checks, not a store, recovery engine or SQL test.
func TestRecoveryFixtures(t *testing.T) {
	var f struct {
		DesignOnly bool `json:"design_only"`
		Record     struct {
			Events   []domain.RunEvent
			Messages []struct {
				ID      string
				Ordinal int
				Parts   []struct {
					Type   string
					CallID string `json:"call_id"`
				}
			}
			Intent struct {
				CallID    string `json:"call_id"`
				MessageID string `json:"assistant_message_id"`
			} `json:"tool_intent"`
			Result struct {
				CallID string `json:"call_id"`
			} `json:"tool_result"`
		}
		Crashes []struct {
			ID      string
			Events  []domain.RunEvent
			Replay  bool `json:"automatic_replay"`
			Stop    bool `json:"stop_proven"`
			Blocked bool `json:"session_blocked"`
		}
		Submission []struct{ ID string } `json:"submission_cases"`
		Delivery   []struct{ ID string } `json:"delivery_cases"`
	}
	b, err := os.ReadFile("fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if !f.DesignOnly {
		t.Fatal("fixture must remain proposed")
	}
	validate := func(events []domain.RunEvent) {
		t.Helper()
		var prev *domain.RunEvent
		for i := range events {
			if err := events[i].ValidateAfter(prev); err != nil {
				t.Fatal(err)
			}
			prev = &events[i]
		}
	}
	validate(f.Record.Events)
	matched := false
	for i, m := range f.Record.Messages {
		if m.Ordinal != i+1 {
			t.Fatal("message ordering")
		}
		for _, p := range m.Parts {
			if p.Type == "tool_call" && p.CallID == f.Record.Intent.CallID && m.ID == f.Record.Intent.MessageID {
				matched = true
			}
		}
	}
	if !matched || f.Record.Result.CallID != f.Record.Intent.CallID {
		t.Fatal("tool correlation")
	}
	seen := map[string]bool{}
	for _, c := range f.Crashes {
		if seen[c.ID] {
			t.Fatal("duplicate case")
		}
		seen[c.ID] = true
		validate(c.Events)
		if c.Replay {
			t.Fatalf("automatic replay: %s", c.ID)
		}
		if c.ID == "lease_expired_old_alive" && (!c.Blocked || c.Stop) {
			t.Fatal("lease is not stop proof")
		}
		if len(c.Events) > 0 && c.Events[len(c.Events)-1].Status.Terminal() && !c.Stop {
			t.Fatalf("terminal without stop proof: %s", c.ID)
		}
	}
	for _, id := range []string{"before_acceptance", "acceptance_ack_lost", "queued_before_start", "running_before_intent", "intent_before_dispatch", "dispatch_before_effect", "effect_before_result", "result_before_terminal", "terminal_commit_unknown", "terminal_ack_lost", "lease_expired_old_alive", "stopped_interrupted", "missing_object", "corrupt_sequence", "unpaired_tool_result", "unknown_schema"} {
		if !seen[id] {
			t.Errorf("missing %s", id)
		}
	}
	for _, group := range [][]struct{ ID string }{f.Submission, f.Delivery} {
		for _, c := range group {
			if seen[c.ID] {
				t.Fatal("duplicate case")
			}
			seen[c.ID] = true
		}
	}
	for _, id := range []string{"identical", "conflicting", "same_session_race", "independent_sessions", "retry_other_instance", "deleted_id", "duplicate_equal", "out_of_order", "duplicate_conflicting"} {
		if !seen[id] {
			t.Errorf("missing %s", id)
		}
	}
}

// Negative examples exercise the existing validator; no delivery adapter is simulated.
func TestNegativeEventExamples(t *testing.T) {
	var f struct {
		Crashes []struct {
			ID       string
			Observed []domain.RunEvent `json:"observed_events"`
			Record   map[string]any    `json:"observed_record"`
		}
		Delivery []struct {
			ID     string
			Events []domain.RunEvent `json:"received_events"`
		} `json:"delivery_cases"`
		Completed struct{ Events []domain.RunEvent } `json:"completed_replacement"`
	}
	b, err := os.ReadFile("fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Crashes {
		switch c.ID {
		case "corrupt_sequence":
			if len(c.Observed) != 3 || c.Observed[1].ValidateAfter(&c.Observed[0]) == nil {
				t.Fatal("missing invalid sequence input")
			}
		case "unpaired_tool_result":
			if c.Record["intent_call_id"] == c.Record["result_call_id"] {
				t.Fatal("missing broken correlation")
			}
		case "unknown_schema":
			if c.Record["schema_version"] == float64(1) {
				t.Fatal("missing unsupported version")
			}
		case "missing_object":
			if c.Record["fetch_result"] != "not_found" {
				t.Fatal("missing object failure")
			}
		}
	}
	for _, c := range f.Delivery {
		if len(c.Events) < 2 {
			t.Fatal("missing delivery inputs")
		}
		if c.Events[1].ValidateAfter(&c.Events[0]) == nil {
			t.Fatalf("expected duplicate or gap: %s", c.ID)
		}
		switch c.ID {
		case "duplicate_equal":
			if c.Events[0] != c.Events[1] {
				t.Fatal("duplicate differs")
			}
		case "duplicate_conflicting":
			if c.Events[0] == c.Events[1] || c.Events[0].Sequence != c.Events[1].Sequence {
				t.Fatal("missing conflicting duplicate")
			}
		}
	}
	var prev *domain.RunEvent
	for i := range f.Completed.Events {
		if err := f.Completed.Events[i].ValidateAfter(prev); err != nil {
			t.Fatal(err)
		}
		prev = &f.Completed.Events[i]
	}
	if prev == nil || prev.Status != domain.RunSucceeded {
		t.Fatal("missing durable completed example")
	}
}
