// Package stylespec defines the style-profile artifact: the tokenized,
// version-controllable representation of a target writing style, plus its
// on-disk (YAML) form. It is the typed contract distill writes and lint, judge,
// retrieve, and discriminate read. See DESIGN.md section 3.
package stylespec

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

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

// Rule is an LLM-induced, corpus-validated style rule. Support is the fraction
// of corpus paragraphs in which the rule held at distillation time.
type Rule struct {
	ID              string  `yaml:"id"`
	Class           string  `yaml:"class"`    // "judged" | "deterministic"
	Severity        string  `yaml:"severity"` // "hard" | "soft"
	Statement       string  `yaml:"statement"`
	Support         float64 `yaml:"support"`
	RequireEvidence bool    `yaml:"require_evidence"`
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

// Load reads and parses a profile from a YAML file.
func Load(path string) (*Profile, error) {
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
