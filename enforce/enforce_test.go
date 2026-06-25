package enforce

import (
	"context"
	"strings"
	"testing"

	"github.com/paulmooreparks/burnish/stylespec"
)

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
