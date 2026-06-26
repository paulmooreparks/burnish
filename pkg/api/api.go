// Package api is the stable, importable entry point for Go applications (Tela,
// planning.fit) that want burnish style enforcement around their own LLM calls.
// It composes the engine packages so callers depend on one surface. The same
// binary also runs as the `serve` HTTP/subprocess sidecar for .NET (later).
package api

import (
	"context"
	"fmt"

	"github.com/paulmooreparks/burnish/distill"
	"github.com/paulmooreparks/burnish/enforce"
	"github.com/paulmooreparks/burnish/judge"
	"github.com/paulmooreparks/burnish/lint"
	"github.com/paulmooreparks/burnish/retrieve"
	"github.com/paulmooreparks/burnish/stylespec"
)

// Assessment is the deterministic style assessment of a draft: the feature/lexicon
// result plus any deterministic rule violations.
type Assessment struct {
	lint.Result
	RuleViolations []judge.RuleViolation `json:"rule_violations,omitempty"`
}

// LoadProfile loads and resolves a profile from a YAML path.
func LoadProfile(path string) (*stylespec.Profile, error) { return stylespec.Load(path) }

// DistillOptions configures Distill: a complete corpus-to-profile build.
type DistillOptions struct {
	ID        string   // profile id; defaults to Register when empty
	Register  string   // register/genre name (required)
	Language  string   // language code; "" defaults to distill.DefaultLanguage
	Avoid     []string // terms this author avoids (hard violations)
	BasePath  string   // base profile to inherit (cross-register invariants); "" = none
	RulesFile string   // YAML profile whose judged rules to merge; "" = none
}

// DistillOutcome reports a completed distill. Profile is held in memory; the
// caller writes it to disk. The counts mirror what the CLI prints.
type DistillOutcome struct {
	Profile            *stylespec.Profile
	DeterministicRules int      // mined + base deterministic rules
	JudgedRules        int      // judged rules merged from RulesFile
	SkippedJudged      []string // judged rule ids skipped on an id collision
}

// Distill builds a complete style profile from corpus documents: stylometric
// features and lexicon, mined deterministic rules, resolved base inheritance,
// and merged judged rules. It does not touch the filesystem (the caller reads
// the corpus and saves the result). This is the single distill-and-finish path
// shared by the CLI and the MCP server so the two front ends cannot drift; any
// reduction here is a reduction everywhere, by construction.
func Distill(docs []distill.DocInput, o DistillOptions) (*DistillOutcome, error) {
	if o.Register == "" {
		return nil, fmt.Errorf("distill requires a register")
	}
	id := o.ID
	if id == "" {
		id = o.Register
	}

	opts := distill.DefaultOptions()
	opts.Avoid = o.Avoid
	prof, err := distill.Distill(id, o.Register, o.Language, docs, opts)
	if err != nil {
		return nil, err
	}

	// Mine the deterministic rule layer, then resolve base inheritance after the
	// rules are set so the saved profile carries the author's base invariants and
	// matches what Load reconstructs.
	prof.Rules = judge.Mine(docs, judge.DefaultMinSupport)
	prof.Inherits = o.BasePath
	if prof, err = stylespec.Resolve(prof, ""); err != nil {
		return nil, err
	}

	out := &DistillOutcome{Profile: prof, DeterministicRules: len(prof.Rules)}

	// Merge author-induced judged (subjective) rules AFTER Resolve so the base
	// merge cannot clobber them, deduped by id so they never collide with a mined
	// or base rule. The carrier file is read raw (any stray inherits ignored).
	if o.RulesFile != "" {
		rf, err := stylespec.LoadRaw(o.RulesFile)
		if err != nil {
			return nil, fmt.Errorf("rules-file: %w", err)
		}
		have := map[string]bool{}
		for _, r := range prof.Rules {
			have[r.ID] = true
		}
		for _, r := range judge.JudgedRules(rf.Rules) {
			if have[r.ID] {
				out.SkippedJudged = append(out.SkippedJudged, r.ID)
				continue
			}
			have[r.ID] = true
			prof.Rules = append(prof.Rules, r)
			out.JudgedRules++
		}
	}
	return out, nil
}

// Check runs the deterministic checks (features, lexicon, discriminator gate, and
// rules) against a draft. No model is involved.
func Check(text string, p *stylespec.Profile) (Assessment, error) {
	res, err := lint.Check(text, p)
	if err != nil {
		return Assessment{}, err
	}
	return Assessment{Result: res, RuleViolations: judge.CheckRules(text, p.Rules)}, nil
}

// BuildBank builds an exemplar retrieval bank from corpus documents (for Massage).
func BuildBank(docs []retrieve.Document) *retrieve.Bank {
	return retrieve.Build(docs, retrieve.DefaultOptions())
}

// Massage runs the bounded massage loop, driving a draft toward the profile's
// target style. The revise step is the caller's: pass your LLM as the Reviser.
// bank may be nil to skip exemplar retrieval.
func Massage(ctx context.Context, draft string, p *stylespec.Profile, bank *retrieve.Bank, revise enforce.Reviser, opts enforce.Options) (*enforce.Outcome, error) {
	return enforce.Massage(ctx, draft, p, bank, revise, opts)
}
