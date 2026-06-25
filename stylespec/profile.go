// Package stylespec defines the style-profile artifact: the tokenized,
// version-controllable representation of a target writing style, plus its
// on-disk (YAML) form. It is the typed contract distill writes and lint, judge,
// retrieve, and discriminate read. See DESIGN.md section 3.
package stylespec

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// BaseProfileID is the reserved name a profile's Inherits field uses to inherit
// the built-in cross-register base (see BaseProfile). Any other non-empty
// Inherits value is resolved as a file path; this reserved name therefore
// shadows any base file literally named "base" (with no extension).
const BaseProfileID = "base"

// BaseProfile returns the built-in cross-register base: the invariants every
// register inherits regardless of what its corpus shows. Today that is the
// no-em-dash rule, enforced as an avoided-lexicon entry (the hard lexical check).
// It deliberately carries no statistical features, so it never distorts a
// register's distance. New cross-register invariants (e.g. a base judged rule)
// belong here, not baked into the distiller. See DESIGN.md section 4.
func BaseProfile() *Profile {
	return &Profile{
		ID:      BaseProfileID,
		Lexicon: Lexicon{Avoided: []string{"—", "--"}},
	}
}

// Merge overlays base onto child and returns the resolved profile. The base holds
// cross-register invariants, so on any conflict the base WINS: a register cannot
// relax an invariant. Features and rules union by id (base entry replaces a
// same-id child entry); avoided and preferred lexicons union (deduped). The
// child's identity, register, language, corpus, and discriminator are kept.
func Merge(base, child *Profile) *Profile {
	out := *child // shallow copy of scalars; slices/pointers rebuilt below

	out.Features = mergeFeatures(child.Features, base.Features)
	out.Rules = mergeRules(child.Rules, base.Rules)
	out.Lexicon = Lexicon{
		Preferred: dedup(append(append([]string(nil), child.Lexicon.Preferred...), base.Lexicon.Preferred...)),
		Avoided:   dedup(append(append([]string(nil), child.Lexicon.Avoided...), base.Lexicon.Avoided...)),
	}
	// Deep-copy the pointer fields so the merged profile does not alias the
	// child's Discriminator/Exemplars (neither struct has nested slices/pointers).
	if child.Discriminator != nil {
		d := *child.Discriminator
		out.Discriminator = &d
	}
	if child.Exemplars != nil {
		e := *child.Exemplars
		out.Exemplars = &e
	}
	return &out
}

func mergeFeatures(child, base []Feature) []Feature {
	idx := map[string]int{}
	out := append([]Feature(nil), child...)
	for i, f := range out {
		idx[f.ID] = i
	}
	for _, bf := range base { // base wins
		if i, ok := idx[bf.ID]; ok {
			out[i] = bf
		} else {
			idx[bf.ID] = len(out)
			out = append(out, bf)
		}
	}
	return out
}

func mergeRules(child, base []Rule) []Rule {
	idx := map[string]int{}
	out := append([]Rule(nil), child...)
	for i, r := range out {
		idx[r.ID] = i
	}
	for _, br := range base { // base wins
		if i, ok := idx[br.ID]; ok {
			out[i] = br
		} else {
			idx[br.ID] = len(out)
			out = append(out, br)
		}
	}
	return out
}

func dedup(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// Resolve applies a profile's Inherits: it loads the named base (the built-in
// BaseProfileID, or a file path relative to dir) and merges it in. The merge is
// stable, not a literal no-op: Inherits is preserved on the result so Load always
// re-applies the current base (supporting base updates), and re-resolving the
// same profile against the same base yields a deep-equal profile (base-wins +
// dedup are stable). One level deep: a base may not itself inherit. An empty
// Inherits returns p unchanged.
func Resolve(p *Profile, dir string) (*Profile, error) {
	if p.Inherits == "" {
		return p, nil
	}
	var base *Profile
	if p.Inherits == BaseProfileID {
		base = BaseProfile()
	} else {
		path := p.Inherits
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		b, err := loadRaw(path)
		if err != nil {
			return nil, fmt.Errorf("resolve inherits %q: %w", p.Inherits, err)
		}
		if b.Inherits != "" {
			return nil, fmt.Errorf("resolve inherits %q: base profiles may not themselves inherit", p.Inherits)
		}
		base = b
	}
	return Merge(base, p), nil
}

// Profile is one register's complete style signature. A profile targets a
// single genre (register); cross-register invariants live in a base profile
// named by Inherits. See DESIGN.md section 4.
type Profile struct {
	ID       string `yaml:"id"`
	Register string `yaml:"register"`
	// Language is the BCP-47-ish code (e.g. "en", "fr", "zh") of the corpus and
	// of the drafts this profile scores. It selects the language module that
	// segments text and computes features; a draft must be scored with the same
	// module that distilled the profile, or the metrics are not comparable. See
	// DESIGN.md section 11.
	Language string `yaml:"language"`
	Inherits string `yaml:"inherits,omitempty"`

	Features      []Feature      `yaml:"features"`
	Lexicon       Lexicon        `yaml:"lexicon"`
	Rules         []Rule         `yaml:"rules,omitempty"`
	Exemplars     *Exemplars     `yaml:"exemplars,omitempty"`
	Discriminator *Discriminator `yaml:"discriminator,omitempty"`

	// Corpus records provenance: how many documents and words the profile was
	// distilled from. Thin corpora yield untrustworthy ranges; this makes that
	// visible rather than silent.
	Corpus CorpusStats `yaml:"corpus,omitempty"`
}

// Feature is one statistical signal with an acceptance range and a weight.
// Mean and Stddev are retained from distillation so lint can normalize a
// draft's deviation in standard-deviation units.
type Feature struct {
	ID     string  `yaml:"id"`
	Target Target  `yaml:"target"`
	Mean   float64 `yaml:"mean"`
	Stddev float64 `yaml:"stddev"`
	Weight float64 `yaml:"weight"`
}

// Target is an inclusive acceptance range. An absent bound (nil) is open on
// that side.
type Target struct {
	Min *float64 `yaml:"min,omitempty"`
	Max *float64 `yaml:"max,omitempty"`
}

// Lexicon is characteristic vocabulary mined by distinctiveness.
type Lexicon struct {
	Preferred []string `yaml:"preferred,omitempty"`
	Avoided   []string `yaml:"avoided,omitempty"`
}

// Rule is a corpus-validated style rule. Support is the fraction of corpus
// paragraphs in which the rule held at mining time. Deterministic rules carry a
// mined Param (e.g. a length ceiling) and are checked mechanically; judged rules
// carry no Param and are evaluated by the caller's LLM against the Statement.
type Rule struct {
	ID              string   `yaml:"id"`
	Class           string   `yaml:"class"`    // "deterministic" | "judged"
	Severity        string   `yaml:"severity"` // "hard" | "soft"
	Statement       string   `yaml:"statement"`
	Support         float64  `yaml:"support"`
	Param           float64  `yaml:"param,omitempty"` // mined threshold for deterministic rules
	Counterexamples []string `yaml:"counterexamples,omitempty"`
	RequireEvidence bool     `yaml:"require_evidence,omitempty"`
}

// Exemplars points at the embedded exemplar index used for retrieval-augmented
// revision.
type Exemplars struct {
	Index string `yaml:"index"`
}

// Discriminator holds the calibrated acceptance gate: a draft is on-target when
// its deterministic distance is at or below Threshold. AUC, TPR, and FPR record
// how well the calibration separated held-out target text from off-style decoys.
type Discriminator struct {
	Threshold float64 `yaml:"threshold"`
	AUC       float64 `yaml:"auc,omitempty"`
	TPR       float64 `yaml:"tpr,omitempty"`
	FPR       float64 `yaml:"fpr,omitempty"`
	Method    string  `yaml:"method,omitempty"` // e.g. "distance-threshold"
}

// CorpusStats is distillation provenance.
type CorpusStats struct {
	Documents int `yaml:"documents,omitempty"`
	Words     int `yaml:"words,omitempty"`
}

// loadRaw reads and parses a profile from YAML without resolving inheritance.
func loadRaw(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}
	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile %s: %w", path, err)
	}
	return &p, nil
}

// Load reads a profile and resolves its inheritance (merge-at-load-time), so
// every consumer sees one fully-resolved profile with the base invariants
// applied. The base is resolved relative to the profile's directory.
func Load(path string) (*Profile, error) {
	p, err := loadRaw(path)
	if err != nil {
		return nil, err
	}
	return Resolve(p, filepath.Dir(path))
}

// Save writes the profile to a YAML file, with features sorted by id for a
// stable, diffable artifact.
func (p *Profile) Save(path string) error {
	sort.Slice(p.Features, func(i, j int) bool { return p.Features[i].ID < p.Features[j].ID })
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write profile %s: %w", path, err)
	}
	return nil
}

// Ptr is a convenience for building optional Target bounds.
func Ptr(f float64) *float64 { return &f }
