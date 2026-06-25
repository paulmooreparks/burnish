package judge

import (
	"fmt"
	"strings"

	"github.com/paulmooreparks/burnish/stylespec"
)

// Subjective, LLM-judged rules: the upgrade over the deterministic structural
// rules in rules.go. burnish bakes no model (decision #5), so judge builds the
// PAYLOADS and a caller's LLM renders them:
//
//   - InductionPrompt asks an LLM to propose candidate style rules from corpus
//     samples (run offline; each candidate is then validated against the corpus
//     and kept only with high support, with require_evidence set).
//   - JudgingPrompt asks an LLM to judge a draft against the kept rules, quoting
//     the offending span for every violation.
//
// Both must be rendered in a FRESH, ISOLATED context (never the one that wrote
// the draft). The kept rules are stored on the profile as class="judged".

// InductionPrompt builds the prompt a caller LLM answers to induce candidate
// subjective style rules from corpus samples. maxRules caps how many it proposes.
func InductionPrompt(samples []string, maxRules int) string {
	if maxRules <= 0 {
		maxRules = 8
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are inducing the STYLE rules of a target author from samples of their writing. ")
	fmt.Fprintf(&b, "Propose up to %d candidate rules that capture HOW they write (voice, rhythm, sentence "+
		"and paragraph shape, stance, how they open and close, argument moves, what they avoid), NOT what "+
		"they write about. Each rule must be checkable against a new draft by quoting evidence.\n\n", maxRules)
	b.WriteString("Return ONLY JSON: a list of objects with fields:\n")
	b.WriteString(`  "id": short kebab-case slug; "statement": one imperative sentence; "rationale": why it characterizes this author.` + "\n\n")
	b.WriteString("Avoid rules that merely restate topic or vocabulary. Prefer rules another writer could follow.\n\n")
	for i, s := range samples {
		fmt.Fprintf(&b, "\n=== SAMPLE %d START ===\n%s\n=== SAMPLE %d END ===\n", i+1, strings.TrimSpace(s), i+1)
	}
	return b.String()
}

// JudgingPrompt builds the prompt a caller LLM answers to judge a draft against
// the profile's judged rules. It requires a quoted evidence span for every
// violation (require_evidence), which kills plausible-but-unfounded findings and
// makes the revision actionable.
func JudgingPrompt(draft string, rules []stylespec.Rule) string {
	var judged []stylespec.Rule
	for _, r := range rules {
		if r.Class == "judged" {
			judged = append(judged, r)
		}
	}
	var b strings.Builder
	if len(judged) == 0 {
		b.WriteString("No judged style rules on this profile; nothing to judge.\n")
		return b.String()
	}
	b.WriteString("Judge whether the DRAFT below follows each of the target author's style rules. ")
	b.WriteString("This is a fresh, isolated judgement: rely only on the rules and the draft, not on any prior context.\n\n")
	b.WriteString("Return ONLY JSON: a list of objects with fields:\n")
	b.WriteString(`  "id": the rule id; "holds": true|false; "evidence": a quoted span from the draft (required when holds=false, showing the violation).` + "\n\n")
	b.WriteString("=== RULES ===\n")
	for _, r := range judged {
		fmt.Fprintf(&b, "- [%s] %s", r.ID, r.Statement)
		if r.Support > 0 {
			fmt.Fprintf(&b, " (holds in %.0f%% of the corpus)", r.Support*100)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\n=== DRAFT START ===\n%s\n=== DRAFT END ===\n", strings.TrimSpace(draft))
	return b.String()
}

// RuleVerdict is one judged-rule judgement returned by whoever renders the
// JudgingPrompt (the caller's LLM in the agentic path, the model/ adapter in the
// headless path). It mirrors the JSON shape the prompt asks for: the rule id,
// whether the rule holds on the draft, and (when it does not) the quoted offending
// span. A Holds==false verdict is a violation; require_evidence binds the judge to
// quote Evidence so the revision is actionable.
type RuleVerdict struct {
	ID       string `json:"id"`
	Holds    bool   `json:"holds"`
	Evidence string `json:"evidence,omitempty"`
}

// JudgedRules returns the judged (subjective) rules from a rule set.
func JudgedRules(rules []stylespec.Rule) []stylespec.Rule {
	var out []stylespec.Rule
	for _, r := range rules {
		if r.Class == "judged" {
			out = append(out, r)
		}
	}
	return out
}
