package judge

import (
	"fmt"
	"math"
	"sort"

	"github.com/paulmooreparks/burnish/distill"
	"github.com/paulmooreparks/burnish/internal/num"
	"github.com/paulmooreparks/burnish/internal/text"
	"github.com/paulmooreparks/burnish/stylespec"
)

// This is the first-cut rule layer: deterministic, corpus-validated structural
// rules with no model in the loop. They capture per-instance constraints the
// aggregate feature distance cannot see, for example a single run-on sentence in
// an otherwise typical paragraph: the distance averages it away, a max-length rule
// catches it. Each rule's threshold is mined from the corpus at a support level
// (the fraction of corpus paragraphs that comply), per DESIGN.md section 3.
//
// LLM-induced *subjective* rules (e.g. "leads with a concrete anecdote, not an
// abstract thesis") are the upgrade: judge/ will build the induction and judging
// payloads and the caller's LLM renders them (decision #5), reusing this same
// support-validation discipline.

// DefaultMinSupport is the fraction of corpus paragraphs a deterministic rule's
// threshold must accommodate; the threshold lands at that quantile.
const DefaultMinSupport = 0.9

// RuleViolation is one place a draft breaks a deterministic rule.
type RuleViolation struct {
	RuleID    string `json:"rule_id"`
	Statement string `json:"statement"`
	Severity  string `json:"severity"`
	Paragraph int    `json:"paragraph"` // 1-based paragraph index in the draft
	Evidence  string `json:"evidence"`  // the offending text (truncated)
}

// candidate is a built-in deterministic rule: how to mine its threshold from a
// corpus and how to check a draft against it.
type candidate struct {
	id       string
	severity string
	statement func(param float64) string
	// perParagraph maps each corpus paragraph to the scalar the rule bounds
	// (e.g. its longest sentence in words). The mined threshold is the
	// minSupport-quantile of these values.
	perParagraph func(text.Paragraph) float64
	// violationsIn returns the offending spans in a draft paragraph given the
	// mined threshold.
	violationsIn func(p text.Paragraph, param float64) []string
}

// candidateByID indexes the built-in candidates for CheckRules; built once since
// the set is immutable and CheckRules is on the per-draft hot path.
var candidateByID = func() map[string]candidate {
	m := make(map[string]candidate, len(candidates))
	for _, c := range candidates {
		m[c.id] = c
	}
	return m
}()

var candidates = []candidate{
	{
		id:       "max-sentence-length",
		severity: "soft",
		statement: func(p float64) string {
			return fmt.Sprintf("Sentences stay at or under %.0f words; the corpus rarely runs longer.", p)
		},
		perParagraph: func(p text.Paragraph) float64 {
			var max float64
			for _, s := range p.Sentences {
				if w := float64(len(s.Words)); w > max {
					max = w
				}
			}
			return max
		},
		violationsIn: func(p text.Paragraph, param float64) []string {
			var out []string
			for _, s := range p.Sentences {
				if float64(len(s.Words)) > param {
					out = append(out, truncate(s.Text, 120))
				}
			}
			return out
		},
	},
	{
		id:       "max-paragraph-length",
		severity: "soft",
		statement: func(p float64) string {
			return fmt.Sprintf("Paragraphs stay at or under %.0f sentences.", p)
		},
		perParagraph: func(p text.Paragraph) float64 {
			return float64(len(p.Sentences))
		},
		violationsIn: func(p text.Paragraph, param float64) []string {
			if float64(len(p.Sentences)) > param {
				return []string{fmt.Sprintf("%d-sentence paragraph: %s", len(p.Sentences), truncate(p.Text, 120))}
			}
			return nil
		},
	},
}

// Mine validates each built-in deterministic rule against the corpus and returns
// the kept rules with their mined threshold, support, and counterexamples.
func Mine(docs []distill.DocInput, minSupport float64) []stylespec.Rule {
	if minSupport <= 0 || minSupport >= 1 {
		minSupport = DefaultMinSupport
	}
	var rules []stylespec.Rule
	for _, c := range candidates {
		var vals []float64
		type para struct {
			p text.Paragraph
			v float64
		}
		var paras []para
		for _, d := range docs {
			for _, p := range text.Segment(d.Text).Paragraphs {
				v := c.perParagraph(p)
				vals = append(vals, v)
				paras = append(paras, para{p, v})
			}
		}
		if len(vals) < 5 {
			continue // too little structure to mine a meaningful threshold
		}
		param := quantile(vals, minSupport)
		var comply int
		for _, v := range vals {
			if v <= param {
				comply++
			}
		}
		support := float64(comply) / float64(len(vals))

		// Counterexamples: the corpus paragraphs that exceed the threshold. Skip the
		// sort entirely when every paragraph complies (nothing to show).
		var cex []string
		if comply < len(vals) {
			sort.Slice(paras, func(i, j int) bool { return paras[i].v > paras[j].v })
			for _, pr := range paras {
				if pr.v <= param || len(cex) >= 3 {
					break
				}
				cex = append(cex, truncate(firstSentence(pr.p), 100))
			}
		}

		rules = append(rules, stylespec.Rule{
			ID:              c.id,
			Class:           "deterministic",
			Severity:        c.severity,
			Statement:       c.statement(param),
			Support:         num.Round(support, 3),
			Param:           param,
			Counterexamples: cex,
		})
	}
	return rules
}

// CheckRules evaluates a draft against the profile's deterministic rules. Judged
// rules are skipped here; they are handed to the caller's LLM via style_review.
func CheckRules(draft string, rules []stylespec.Rule) []RuleViolation {
	var out []RuleViolation
	doc := text.Segment(draft)
	for _, r := range rules {
		if r.Class != "deterministic" {
			continue
		}
		c, ok := candidateByID[r.ID]
		if !ok {
			continue
		}
		for i, p := range doc.Paragraphs {
			for _, ev := range c.violationsIn(p, r.Param) {
				out = append(out, RuleViolation{
					RuleID:    r.ID,
					Statement: r.Statement,
					Severity:  r.Severity,
					Paragraph: i + 1,
					Evidence:  ev,
				})
			}
		}
	}
	return out
}

// quantile returns the value at fraction q of the sorted data (nearest-rank),
// without mutating xs. Returns 0 for empty input; callers (Mine) must guard
// against empty, since 0 is also a legitimate quantile value.
func quantile(xs []float64, q float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	if len(s) == 0 {
		return 0
	}
	idx := int(math.Ceil(q*float64(len(s)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}

func firstSentence(p text.Paragraph) string {
	if len(p.Sentences) > 0 {
		return p.Sentences[0].Text
	}
	return p.Text
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
