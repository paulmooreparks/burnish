package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/paulmooreparks/burnish/distill"
)

// corpusDocs returns a small single-register corpus with a consistent trait
// (every sentence short and declarative) so rule mining has something to find.
func corpusDocs(t *testing.T) []distill.DocInput {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Short, regular paragraphs (two short sentences each, blank-line separated)
	// so the miner finds a consistent trait across paragraphs, as judge.Mine
	// expects. One body repeated across docs is enough to clear 0.9 support.
	body := "The cat sat. The dog ran.\n\nBirds fly south. Fish swim east.\n\nWe walk home. They drive far."
	write("a.md", body)
	write("b.md", body)
	write("c.md", body)
	write("d.md", body)
	docs, err := distill.ReadCorpusDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

// TestDistillMinesRules is the parity guard: the shared Distill path must
// populate the deterministic rule layer. A profile with zero rules would make a
// later check report no violations because there is nothing to check, the exact
// silent degradation burnish-21 fixes.
func TestDistillMinesRules(t *testing.T) {
	docs := corpusDocs(t)
	outcome, err := Distill(docs, DistillOptions{Register: "test-register", Avoid: []string{"—"}})
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	if outcome.Profile.ID != "test-register" {
		t.Errorf("id should default to register, got %q", outcome.Profile.ID)
	}
	if outcome.DeterministicRules == 0 || len(outcome.Profile.Rules) == 0 {
		t.Fatal("Distill produced no deterministic rules; the rule layer was skipped")
	}
	if outcome.DeterministicRules != len(outcome.Profile.Rules) {
		t.Errorf("rule count mismatch: reported %d, profile has %d", outcome.DeterministicRules, len(outcome.Profile.Rules))
	}
	// The avoided term must reach the profile's lexicon.
	found := false
	for _, a := range outcome.Profile.Lexicon.Avoided {
		if a == "—" {
			found = true
		}
	}
	if !found {
		t.Errorf("avoided term not applied: %v", outcome.Profile.Lexicon.Avoided)
	}
}

// TestDistillRequiresRegister keeps the contract explicit.
func TestDistillRequiresRegister(t *testing.T) {
	if _, err := Distill(corpusDocs(t), DistillOptions{}); err == nil {
		t.Error("Distill with no register should error")
	}
}

// TestDistillMergesJudgedRules covers the most subtle path: judged rules from a
// rules-file are merged AFTER base resolution and deduped by id against the
// mined rules. A judged rule with a fresh id is added; one whose id collides
// with a mined rule is skipped and accounted for.
func TestDistillMergesJudgedRules(t *testing.T) {
	docs := corpusDocs(t)
	// Find a mined rule id to force a collision against.
	mined, err := Distill(docs, DistillOptions{Register: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if len(mined.Profile.Rules) == 0 {
		t.Fatal("need at least one mined rule to test collision")
	}
	collidingID := mined.Profile.Rules[0].ID

	// A rules-file profile carrying two judged rules: one fresh, one colliding.
	rulesFile := filepath.Join(t.TempDir(), "rules.profile.yaml")
	body := "" +
		"id: carrier\nregister: r\nlanguage: en\nrules:\n" +
		"  - id: prefer-active-voice\n    class: judged\n    severity: soft\n    statement: Prefer the active voice.\n" +
		"  - id: " + collidingID + "\n    class: judged\n    severity: soft\n    statement: This should be skipped.\n"
	if err := os.WriteFile(rulesFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := Distill(docs, DistillOptions{Register: "r", RulesFile: rulesFile})
	if err != nil {
		t.Fatalf("Distill with rules-file: %v", err)
	}
	if out.JudgedRules != 1 {
		t.Errorf("JudgedRules = %d, want 1 (one fresh, one skipped)", out.JudgedRules)
	}
	if len(out.SkippedJudged) != 1 || out.SkippedJudged[0] != collidingID {
		t.Errorf("SkippedJudged = %v, want [%s]", out.SkippedJudged, collidingID)
	}
	// The fresh judged rule must be present in the final profile.
	var sawFresh bool
	for _, r := range out.Profile.Rules {
		if r.ID == "prefer-active-voice" && r.Class == "judged" {
			sawFresh = true
		}
	}
	if !sawFresh {
		t.Error("fresh judged rule not merged into the profile")
	}
}
