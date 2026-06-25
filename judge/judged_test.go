package judge

import (
	"strings"
	"testing"

	"github.com/paulmooreparks/burnish/stylespec"
)

func TestInductionPrompt(t *testing.T) {
	p := InductionPrompt([]string{"First sample text.", "Second sample text."}, 5)
	for _, want := range []string{"up to 5", "JSON", "statement", "First sample text.", "Second sample text."} {
		if !strings.Contains(p, want) {
			t.Errorf("induction prompt missing %q", want)
		}
	}
}

func TestJudgingPrompt(t *testing.T) {
	rules := []stylespec.Rule{
		{ID: "max-sentence-length", Class: "deterministic", Statement: "Short sentences."},
		{ID: "open-with-anecdote", Class: "judged", Statement: "Open with an anecdote.", Support: 0.8},
	}
	p := JudgingPrompt("This is the draft text.", rules)
	if !strings.Contains(p, "open-with-anecdote") || !strings.Contains(p, "Open with an anecdote.") {
		t.Errorf("judging prompt missing the judged rule: %s", p)
	}
	if strings.Contains(p, "max-sentence-length") {
		t.Error("judging prompt should not include deterministic rules")
	}
	if !strings.Contains(p, "This is the draft text.") || !strings.Contains(p, "evidence") {
		t.Errorf("judging prompt missing draft or evidence requirement")
	}
	if !strings.Contains(p, "80%") {
		t.Errorf("judging prompt should surface support: %s", p)
	}
}

func TestJudgingPromptNoJudgedRules(t *testing.T) {
	rules := []stylespec.Rule{{ID: "x", Class: "deterministic", Statement: "y"}}
	if p := JudgingPrompt("draft", rules); !strings.Contains(p, "nothing to judge") {
		t.Errorf("expected no-judged-rules message, got %q", p)
	}
}

func TestJudgedRulesFilter(t *testing.T) {
	rules := []stylespec.Rule{
		{ID: "a", Class: "deterministic"},
		{ID: "b", Class: "judged"},
		{ID: "c", Class: "judged"},
	}
	if got := JudgedRules(rules); len(got) != 2 {
		t.Errorf("JudgedRules = %d, want 2", len(got))
	}
}
