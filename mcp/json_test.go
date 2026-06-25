package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/paulmooreparks/burnish/judge"
	"github.com/paulmooreparks/burnish/lint"
)

// TestScoreResultJSONPromotion guards against a silent regression: if lint.Result
// ever gains a colliding rule_violations field, Go's embedding promotion would
// drop the outer one from JSON rather than error. Assert the key is present.
func TestScoreResultJSONPromotion(t *testing.T) {
	sr := scoreResult{
		Result:         lint.Result{Distance: 0.5},
		RuleViolations: []judge.RuleViolation{{RuleID: "max-sentence-length", Paragraph: 1}},
	}
	b, err := json.Marshal(sr)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{`"distance"`, `"rule_violations"`, `"rule_id"`} {
		if !strings.Contains(s, key) {
			t.Errorf("missing %s in JSON: %s", key, s)
		}
	}
}
