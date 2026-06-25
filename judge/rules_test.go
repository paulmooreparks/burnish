package judge

import (
	"strings"
	"testing"

	"github.com/paulmooreparks/burnish/distill"
	"github.com/paulmooreparks/burnish/stylespec"
)

func TestQuantile(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if q := quantile(xs, 0.9); q != 9 {
		t.Errorf("quantile(0.9) = %v, want 9", q)
	}
	if q := quantile(xs, 1.0); q != 10 {
		t.Errorf("quantile(1.0) = %v, want 10", q)
	}
	// q at 0 exercises the idx<0 clamp -> first element.
	if q := quantile(xs, 0.0); q != 1 {
		t.Errorf("quantile(0.0) = %v, want 1", q)
	}
	// Empty input returns the documented 0 sentinel (Mine guards against this).
	if q := quantile(nil, 0.9); q != 0 {
		t.Errorf("quantile(nil) = %v, want 0", q)
	}
	// Does not mutate the caller's slice.
	src := []float64{3, 1, 2}
	_ = quantile(src, 0.5)
	if src[0] != 3 || src[1] != 1 || src[2] != 2 {
		t.Errorf("quantile mutated caller slice: %v", src)
	}
}

func TestMineProducesRules(t *testing.T) {
	// A corpus of short, regular paragraphs: every sentence is short, every
	// paragraph has two sentences.
	docs := make([]distill.DocInput, 6)
	for i := range docs {
		docs[i] = distill.DocInput{
			Name: "d",
			Text: "The cat sat. The dog ran.\n\nBirds fly south. Fish swim east.\n\nWe walk home. They drive far.",
		}
	}
	rules := Mine(docs, DefaultMinSupport)
	if len(rules) == 0 {
		t.Fatal("expected mined rules")
	}
	var sawSent bool
	for _, r := range rules {
		if r.Class != "deterministic" {
			t.Errorf("rule %s: class = %q, want deterministic", r.ID, r.Class)
		}
		if r.Support < DefaultMinSupport-1e-9 {
			t.Errorf("rule %s: support %v below minSupport", r.ID, r.Support)
		}
		if r.ID == "max-sentence-length" {
			sawSent = true
			if r.Param < 3 || r.Param > 5 {
				t.Errorf("max sentence length param = %v, want ~3-4 words", r.Param)
			}
		}
	}
	if !sawSent {
		t.Error("expected a max-sentence-length rule")
	}
}

func TestCheckRulesFlagsRunOn(t *testing.T) {
	rule := stylespec.Rule{
		ID: "max-sentence-length", Class: "deterministic", Severity: "soft",
		Statement: "Sentences stay at or under 8 words.", Param: 8,
	}
	short := "A short clean sentence here. Another tidy one follows."
	if v := CheckRules(short, []stylespec.Rule{rule}); len(v) != 0 {
		t.Errorf("clean text flagged: %+v", v)
	}
	runOn := "This single sentence goes on and on and on well past the modest ceiling that the corpus established for sentence length here."
	v := CheckRules(runOn, []stylespec.Rule{rule})
	if len(v) != 1 {
		t.Fatalf("expected 1 run-on violation, got %d", len(v))
	}
	if v[0].Paragraph != 1 || !strings.Contains(v[0].Statement, "8 words") {
		t.Errorf("unexpected violation: %+v", v[0])
	}
}

func TestCheckRulesSkipsJudged(t *testing.T) {
	judged := stylespec.Rule{ID: "leads-with-anecdote", Class: "judged", Statement: "Opens with a scene."}
	if v := CheckRules("Anything at all goes here.", []stylespec.Rule{judged}); len(v) != 0 {
		t.Errorf("judged rule should not be checked deterministically: %+v", v)
	}
}
