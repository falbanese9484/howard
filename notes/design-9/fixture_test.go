package design9

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/falbanese9484/agent-harness/internal/domain"
)

// Fixture validation only: there is no service/policy implementation under test.
func TestBoundaryFixtures(t *testing.T) {
	var f struct {
		DesignOnly bool `json:"design_only"`
		Cases      []struct {
			ID       string
			Expected struct {
				HTTP   int
				Launch bool
				Body   json.RawMessage
			}
			Request struct{ Body map[string]any }
		}
		Lifecycle []domain.RunEvent
	}
	b, err := os.ReadFile("fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if !f.DesignOnly {
		t.Fatal("fixture must remain explicitly proposed")
	}
	seen := map[string]bool{}
	for _, c := range f.Cases {
		if seen[c.ID] || c.ID == "" {
			t.Fatalf("duplicate/empty case: %s", c.ID)
		}
		seen[c.ID] = true
		if c.Expected.HTTP >= 400 && c.Expected.Launch {
			t.Fatalf("denial launches: %s", c.ID)
		}
		if c.Expected.Launch && c.Expected.HTTP != 202 {
			t.Fatalf("launch without acceptance: %s", c.ID)
		}
		if c.ID == "cancel" {
			var body struct {
				Terminal  bool
				Requested bool `json:"cancel_requested"`
			}
			if err := json.Unmarshal(c.Expected.Body, &body); err != nil {
				t.Fatal(err)
			}
			if body.Terminal || !body.Requested {
				t.Fatal("cancel ack is not terminal")
			}
		}
	}
	for _, id := range []string{"submit", "status", "events", "cancel", "absent_credentials", "invalid_credentials", "other_owner_submit", "other_owner_status", "other_owner_events", "other_owner_cancel", "forged_prompt", "forged_metadata", "workspace_denied", "profile_denied", "identical_run_id", "conflicting_run_id", "binding_expired", "binding_revoked", "policy_unavailable", "audience_denied", "cross_owner_history", "cross_owner_cache", "cross_owner_artifact", "expired_approval"} {
		if !seen[id] {
			t.Errorf("missing case %s", id)
		}
	}
	var prev *domain.RunEvent
	for i := range f.Lifecycle {
		if err := f.Lifecycle[i].ValidateAfter(prev); err != nil {
			t.Fatal(err)
		}
		prev = &f.Lifecycle[i]
	}
	if prev == nil || prev.Status != domain.RunCanceled {
		t.Fatal("missing stopped terminal example")
	}
}
