// Package api is the stable, importable entry point for Go applications (Tela,
// planning.fit) that want burnish style enforcement around their own LLM calls.
// It composes the engine packages so callers depend on one surface. The same
// binary also runs as the `serve` HTTP/subprocess sidecar for .NET (later).
package api

import (
	"context"

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
