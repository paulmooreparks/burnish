package stylespec

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestMergeBaseWins(t *testing.T) {
	child := &Profile{
		ID:      "child",
		Register: "r",
		Features: []Feature{
			{ID: "em_dash_rate", Target: Target{Max: Ptr(100)}, Weight: 0.3}, // child would allow em-dashes
			{ID: "sentence_len_mean", Target: Target{Min: Ptr(10), Max: Ptr(20)}, Weight: 0.8},
		},
		Lexicon: Lexicon{Preferred: []string{"foo"}, Avoided: []string{"bar"}},
		Rules:   []Rule{{ID: "max-sentence-length", Param: 45}},
	}
	base := &Profile{
		ID: "base",
		Features: []Feature{{ID: "em_dash_rate", Target: Target{Max: Ptr(0)}, Weight: 1}},
		Lexicon: Lexicon{Avoided: []string{"—", "--", "bar"}}, // overlapping "bar" dedupes
		Rules:   []Rule{{ID: "no-survey", Class: "judged"}},
	}

	m := Merge(base, child)

	// Base wins on the conflicting feature.
	var em Feature
	for _, f := range m.Features {
		if f.ID == "em_dash_rate" {
			em = f
		}
	}
	if em.Target.Max == nil || *em.Target.Max != 0 || em.Weight != 1 {
		t.Errorf("base did not win em_dash_rate: %+v", em)
	}
	// Child-only feature preserved.
	if len(m.Features) != 2 {
		t.Errorf("feature count = %d, want 2 (union)", len(m.Features))
	}
	// Avoided union, deduped.
	if got := len(m.Lexicon.Avoided); got != 3 {
		t.Errorf("avoided = %v, want 3 deduped", m.Lexicon.Avoided)
	}
	// Rules union by id.
	if len(m.Rules) != 2 {
		t.Errorf("rules = %d, want 2", len(m.Rules))
	}
	// Child identity preserved.
	if m.ID != "child" || m.Register != "r" {
		t.Errorf("identity not preserved: %s/%s", m.ID, m.Register)
	}
}

func TestResolveBuiltinBase(t *testing.T) {
	p := &Profile{ID: "x", Inherits: BaseProfileID}
	r, err := Resolve(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(r.Lexicon.Avoided, "—") || !contains(r.Lexicon.Avoided, "--") {
		t.Errorf("builtin base avoided not applied: %v", r.Lexicon.Avoided)
	}
}

func TestResolveNoInherits(t *testing.T) {
	p := &Profile{ID: "x"}
	r, err := Resolve(p, "")
	if err != nil || r != p {
		t.Errorf("empty inherits should return p unchanged (err=%v)", err)
	}
}

func TestResolveStable(t *testing.T) {
	// Re-resolving a resolved profile against the same base yields a deep-equal
	// profile (stable merge), and Inherits is preserved so Load keeps re-applying.
	p := &Profile{ID: "x", Inherits: BaseProfileID, Discriminator: &Discriminator{Threshold: 0.3}}
	r1, _ := Resolve(p, "")
	r2, _ := Resolve(r1, "")
	if !reflect.DeepEqual(r1, r2) {
		t.Errorf("resolve not stable:\n%+v\n%+v", r1, r2)
	}
	if r1.Inherits != BaseProfileID {
		t.Errorf("Inherits not preserved: %q", r1.Inherits)
	}
}

func TestLoadResolvesFileBase(t *testing.T) {
	dir := t.TempDir()
	base := &Profile{ID: "mybase", Lexicon: Lexicon{Avoided: []string{"zzz"}}}
	if err := base.Save(filepath.Join(dir, "mybase.profile.yaml")); err != nil {
		t.Fatal(err)
	}
	child := &Profile{ID: "c", Register: "r", Language: "en", Inherits: "mybase.profile.yaml"}
	if err := child.Save(filepath.Join(dir, "c.profile.yaml")); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filepath.Join(dir, "c.profile.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(loaded.Lexicon.Avoided, "zzz") {
		t.Errorf("file base not merged on Load: %v", loaded.Lexicon.Avoided)
	}
}

func TestResolveRejectsNestedBase(t *testing.T) {
	dir := t.TempDir()
	base := &Profile{ID: "b", Inherits: "other"} // a base that itself inherits
	if err := base.Save(filepath.Join(dir, "b.profile.yaml")); err != nil {
		t.Fatal(err)
	}
	child := &Profile{ID: "c", Inherits: "b.profile.yaml"}
	if _, err := Resolve(child, dir); err == nil {
		t.Error("expected error for nested base inheritance")
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
