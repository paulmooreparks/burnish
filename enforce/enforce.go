// Package enforce is the online massage loop that ties the engine together:
// lint -> judge -> retrieve -> discriminate -> revise, bounded at N revisions,
// then accept or give up by severity (DESIGN.md sections 5, 6, 8).
//
// The deterministic stages (lint features/lexicon, judge rules, retrieve
// exemplars, discriminate verdict) run here; the revise step is GENERATION and
// therefore the caller's, per the inference-in-the-caller decision (#5). Massage
// takes an injected Reviser: in the agentic path it is the calling agent's LLM
// (which can equivalently drive the loop itself via the MCP style_review tool);
// in the headless path it is the model/ adapter; in tests it is a mock. enforce
// never bakes a model.
package enforce

import (
	"context"
	"fmt"
	"strings"

	"github.com/paulmooreparks/burnish/judge"
	"github.com/paulmooreparks/burnish/lint"
	"github.com/paulmooreparks/burnish/retrieve"
	"github.com/paulmooreparks/burnish/stylespec"
)

// Reviser rewrites a draft given a revision brief, returning the revised text. It
// is where inference happens; enforce supplies the brief and owns the loop.
type Reviser func(ctx context.Context, draft string, brief Brief) (string, error)

// Brief is the consolidated, actionable guidance handed to the reviser each
// iteration: every deterministic signal plus the retrieved exemplars.
type Brief struct {
	Distance         float64                 `json:"distance"`
	OnTarget         *bool                   `json:"on_target,omitempty"`
	Threshold        *float64                `json:"threshold,omitempty"`
	Features         []lint.FeatureViolation `json:"features,omitempty"`
	Lexical          []lint.LexicalViolation `json:"lexical,omitempty"`
	Rules            []judge.RuleViolation   `json:"rules,omitempty"`
	Exemplars        []retrieve.Result       `json:"exemplars,omitempty"`
	PreferredLexicon []string                `json:"preferred_lexicon,omitempty"`
	AvoidedLexicon   []string                `json:"avoided_lexicon,omitempty"`
	Guidance         string                  `json:"guidance"`
}

// Step records one iteration's assessment.
type Step struct {
	Revision       int      `json:"revision"` // revisions applied before this check
	Distance       float64  `json:"distance"`
	OnTarget       *bool    `json:"on_target,omitempty"`
	HardViolations int      `json:"hard_violations"`
	RuleViolations int      `json:"rule_violations"`
	Accepted       bool     `json:"accepted"`
}

// Outcome is the result of a massage run.
type Outcome struct {
	Final     string `json:"final"`     // the final (possibly revised) text
	Accepted  bool   `json:"accepted"`  // passed the acceptance gate
	Revisions int    `json:"revisions"` // revise calls made
	Trajectory []Step `json:"trajectory"`
}

// Options tunes the loop.
type Options struct {
	MaxRevisions int // default 3 (DESIGN N = 2-3)
	Exemplars    int // exemplars retrieved per iteration; default 3
}

// Massage drives the loop. bank may be nil (no exemplar retrieval); revise may be
// nil (assess only, no revision). Acceptance gate: no hard violations AND, if the
// profile carries a calibrated discriminator, on-target. Soft feature/rule
// violations inform the brief but do not block acceptance (DESIGN.md section 5:
// hard blocks, soft warns, the discriminator is the gate).
func Massage(ctx context.Context, draft string, p *stylespec.Profile, bank *retrieve.Bank, revise Reviser, opts Options) (*Outcome, error) {
	if opts.MaxRevisions <= 0 {
		opts.MaxRevisions = 3
	}
	if opts.Exemplars <= 0 {
		opts.Exemplars = 3
	}

	cur := draft
	out := &Outcome{}
	for {
		res, err := lint.Check(cur, p)
		if err != nil {
			return nil, err
		}
		rules := judge.CheckRules(cur, p.Rules)
		accepted := res.HardViolations == 0 && (res.OnTarget == nil || *res.OnTarget)
		out.Trajectory = append(out.Trajectory, Step{
			Revision:       out.Revisions,
			Distance:       res.Distance,
			OnTarget:       res.OnTarget,
			HardViolations: res.HardViolations,
			RuleViolations: len(rules),
			Accepted:       accepted,
		})

		if accepted || out.Revisions >= opts.MaxRevisions || revise == nil {
			out.Final, out.Accepted = cur, accepted
			return out, nil
		}

		var exemplars []retrieve.Result
		if bank != nil {
			exemplars = bank.Retrieve(cur, opts.Exemplars)
		}
		revised, err := revise(ctx, cur, buildBrief(res, rules, exemplars, p))
		if err != nil {
			return nil, fmt.Errorf("revise (revision %d): %w", out.Revisions+1, err)
		}
		out.Revisions++
		if strings.TrimSpace(revised) == "" || revised == cur {
			// No progress (empty or identical): stop with the current state rather
			// than spin the remaining budget.
			out.Final, out.Accepted = cur, accepted
			return out, nil
		}
		cur = revised
	}
}

func buildBrief(res lint.Result, rules []judge.RuleViolation, exemplars []retrieve.Result, p *stylespec.Profile) Brief {
	return Brief{
		Distance:         res.Distance,
		OnTarget:         res.OnTarget,
		Threshold:        res.Threshold,
		Features:         res.Features,
		Lexical:          res.Lexical,
		Rules:            rules,
		Exemplars:        exemplars,
		PreferredLexicon: p.Lexicon.Preferred,
		AvoidedLexicon:   p.Lexicon.Avoided,
		Guidance: "Rewrite the draft to move it toward the target style WITHOUT changing its meaning: " +
			"bring the off-target features into range, remove every avoided term, fix the listed rule " +
			"violations, favor the preferred lexicon, and match the voice and rhythm of the exemplars " +
			"(authentic target-style passages on the same topic). It will be re-checked; the goal is to " +
			"cross the discriminator threshold (on-target) with no hard violations.",
	}
}
