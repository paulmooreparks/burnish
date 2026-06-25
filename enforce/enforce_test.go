package enforce

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/paulmooreparks/burnish/judge"
	"github.com/paulmooreparks/burnish/stylespec"
)

// judgedProfile carries one judged rule at the given severity and no discriminator,
// so acceptance turns purely on hard violations (lint + rules).
func judgedProfile(severity string) *stylespec.Profile {
	return &stylespec.Profile{
		Language: "en",
		Rules: []stylespec.Rule{
			{ID: "open-anecdote", Class: "judged", Severity: severity, Statement: "Open with a concrete anecdote."},
		},
	}
}

func TestMassageHardJudgedRuleBlocksThenFixes(t *testing.T) {
	p := judgedProfile("hard")
	// Judge fails the rule while the draft contains "abstract", passes once it does not.
	judgeHook := func(_ context.Context, draft string, _ []stylespec.Rule) ([]judge.RuleVerdict, error) {
		holds := !strings.Contains(draft, "abstract")
		return []judge.RuleVerdict{{ID: "open-anecdote", Holds: holds, Evidence: "opens with an abstract thesis"}}, nil
	}
	var sawJudged bool
	revise := func(_ context.Context, draft string, b Brief) (string, error) {
		for _, rv := range b.Rules {
			if rv.RuleID == "open-anecdote" && rv.Evidence != "" {
				sawJudged = true
			}
		}
		return strings.ReplaceAll(draft, "abstract", "anecdote"), nil
	}
	out, err := Massage(context.Background(), "an abstract opening", p, nil, revise, Options{Judge: judgeHook})
	if err != nil {
		t.Fatal(err)
	}
	if !sawJudged {
		t.Error("expected the judged violation (with evidence) to reach the reviser's brief")
	}
	if !out.Accepted || out.Revisions != 1 {
		t.Errorf("hard judged rule should block once then accept after the fix: %+v", out)
	}
	if out.Trajectory[0].Accepted {
		t.Errorf("iteration 0 should be rejected by the hard judged violation: %+v", out.Trajectory[0])
	}
}

func TestMassageSoftJudgedRuleDoesNotBlock(t *testing.T) {
	p := judgedProfile("soft")
	// Always fails the rule; soft, so it must not block acceptance.
	judgeHook := func(_ context.Context, _ string, _ []stylespec.Rule) ([]judge.RuleVerdict, error) {
		return []judge.RuleVerdict{{ID: "open-anecdote", Holds: false, Evidence: "x"}}, nil
	}
	called := 0
	revise := func(_ context.Context, d string, _ Brief) (string, error) { called++; return d, nil }
	out, err := Massage(context.Background(), "clean text", p, nil, revise, Options{Judge: judgeHook})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Accepted || out.Revisions != 0 || called != 0 {
		t.Errorf("soft judged failure should inform but not block: %+v (revise called %d)", out, called)
	}
}

func TestMassageSoftJudgedRuleInformsBrief(t *testing.T) {
	// A soft judged rule that always fails, plus a hard lexical violation ("zzz")
	// that forces a revision. The soft judged failure must ride along in the brief
	// (inform) even though it never blocks acceptance.
	p := judgedProfile("soft")
	p.Lexicon = stylespec.Lexicon{Avoided: []string{"zzz"}}
	judgeHook := func(_ context.Context, _ string, _ []stylespec.Rule) ([]judge.RuleVerdict, error) {
		return []judge.RuleVerdict{{ID: "open-anecdote", Holds: false, Evidence: "no anecdote"}}, nil
	}
	var sawSoftJudged bool
	revise := func(_ context.Context, draft string, b Brief) (string, error) {
		for _, rv := range b.Rules {
			if rv.RuleID == "open-anecdote" {
				sawSoftJudged = true
			}
		}
		return strings.ReplaceAll(draft, "zzz", "ok"), nil
	}
	out, err := Massage(context.Background(), "has zzz", p, nil, revise, Options{Judge: judgeHook})
	if err != nil {
		t.Fatal(err)
	}
	if !sawSoftJudged {
		t.Error("soft judged failure should appear in the brief that drives revision")
	}
	if !out.Accepted || out.Revisions != 1 {
		t.Errorf("hard lexical fix should accept after one revision despite the soft judged failure: %+v", out)
	}
}

func TestMassageNilJudgeSkipsJudgedRules(t *testing.T) {
	p := judgedProfile("hard") // a hard judged rule, but no Judge supplied
	called := 0
	revise := func(_ context.Context, d string, _ Brief) (string, error) { called++; return d, nil }
	out, err := Massage(context.Background(), "clean text", p, nil, revise, Options{}) // Judge nil
	if err != nil {
		t.Fatal(err)
	}
	if !out.Accepted || out.Revisions != 0 || called != 0 {
		t.Errorf("nil Judge must skip judged rules entirely (current behavior): %+v (revise called %d)", out, called)
	}
}

func TestMassageJudgeErrorPropagates(t *testing.T) {
	p := judgedProfile("hard")
	judgeHook := func(_ context.Context, _ string, _ []stylespec.Rule) ([]judge.RuleVerdict, error) {
		return nil, errors.New("judge LLM failed")
	}
	revise := func(_ context.Context, d string, _ Brief) (string, error) { return d, nil }
	_, err := Massage(context.Background(), "draft", p, nil, revise, Options{Judge: judgeHook})
	if err == nil || !strings.Contains(err.Error(), "judge LLM failed") {
		t.Errorf("expected judge error to propagate, got %v", err)
	}
}

// profile with one avoided term ("zzz") and no discriminator: acceptance == no
// hard violations. Keeps the loop test independent of a distilled profile.
func avoidProfile() *stylespec.Profile {
	return &stylespec.Profile{Language: "en", Lexicon: stylespec.Lexicon{Avoided: []string{"zzz"}}}
}

func TestMassageConverges(t *testing.T) {
	p := avoidProfile()
	// Reviser removes the offending avoided term, making the draft acceptable.
	revise := func(_ context.Context, draft string, _ Brief) (string, error) {
		return strings.ReplaceAll(draft, "zzz", "fix"), nil
	}
	out, err := Massage(context.Background(), "this has zzz in it", p, nil, revise, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Accepted {
		t.Errorf("expected acceptance, got %+v", out)
	}
	if out.Revisions != 1 {
		t.Errorf("revisions = %d, want 1", out.Revisions)
	}
	if strings.Contains(out.Final, "zzz") {
		t.Errorf("final still contains avoided term: %q", out.Final)
	}
	// Trajectory: off-target check, then accepted check.
	if len(out.Trajectory) != 2 || out.Trajectory[0].Accepted || !out.Trajectory[1].Accepted {
		t.Errorf("unexpected trajectory: %+v", out.Trajectory)
	}
}

func TestMassageAlreadyClean(t *testing.T) {
	p := avoidProfile()
	called := 0
	revise := func(_ context.Context, d string, _ Brief) (string, error) { called++; return d, nil }
	out, err := Massage(context.Background(), "this is clean text", p, nil, revise, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Accepted || out.Revisions != 0 || called != 0 {
		t.Errorf("clean draft should accept with no revisions: %+v (revise called %d)", out, called)
	}
}

func TestMassageExhaustsBudget(t *testing.T) {
	p := avoidProfile()
	// Reviser changes the text but never removes the violation -> never accepts.
	n := 0
	revise := func(_ context.Context, d string, _ Brief) (string, error) {
		n++
		return d + " zzz", nil // still contains zzz, and text changes so no "no progress" stop
	}
	out, err := Massage(context.Background(), "start zzz", p, nil, revise, Options{MaxRevisions: 2})
	if err != nil {
		t.Fatal(err)
	}
	if out.Accepted {
		t.Errorf("should not accept, got %+v", out)
	}
	if out.Revisions != 2 {
		t.Errorf("revisions = %d, want 2 (budget)", out.Revisions)
	}
}

func TestMassageStopsOnNoProgress(t *testing.T) {
	p := avoidProfile()
	revise := func(_ context.Context, d string, _ Brief) (string, error) { return d, nil } // identity
	out, err := Massage(context.Background(), "has zzz", p, nil, revise, Options{MaxRevisions: 5})
	if err != nil {
		t.Fatal(err)
	}
	if out.Accepted {
		t.Error("identity reviser cannot fix; should not accept")
	}
	if out.Revisions != 1 {
		t.Errorf("should stop after 1 no-progress revision, got %d", out.Revisions)
	}
}

func TestMassageNilReviserAssessesOnly(t *testing.T) {
	p := avoidProfile()
	out, err := Massage(context.Background(), "has zzz", p, nil, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Revisions != 0 || out.Accepted {
		t.Errorf("nil reviser should assess-only and report not-accepted: %+v", out)
	}
	if len(out.Trajectory) != 1 {
		t.Errorf("nil reviser should produce one assessment, got %d", len(out.Trajectory))
	}
}
