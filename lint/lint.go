// Package lint runs the deterministic half of the massage loop: it scores a
// draft against a Profile's statistical features and lexicon, with zero LLM
// calls. Deterministic checks run first and free; only what survives reaches
// the judge (DESIGN.md sections 5 and 6). The headline output is a single
// distance-to-style number plus the specific features that are off.
package lint

import (
	"sort"
	"strings"

	"github.com/paulmooreparks/burnish/distill"
	"github.com/paulmooreparks/burnish/internal/num"
	"github.com/paulmooreparks/burnish/stylespec"
)

// Result is the deterministic style assessment of a draft.
type Result struct {
	// Distance is the weighted mean deviation, in standard-deviation units, of
	// out-of-range features. 0 means every feature sits inside its target range.
	Distance float64 `json:"distance"`
	// Features lists every feature that fell outside its target range, worst
	// (highest weighted contribution) first.
	Features []FeatureViolation `json:"features,omitempty"`
	// Lexical lists each occurrence of an avoided term, with its span.
	Lexical []LexicalViolation `json:"lexical,omitempty"`
	// HardViolations counts violations that must block (e.g. em-dash present).
	HardViolations int `json:"hard_violations"`
	// OnTarget is the calibrated discriminator verdict: true when Distance is at
	// or below the profile's discriminator threshold. Nil when the profile carries
	// no calibrated discriminator.
	OnTarget *bool `json:"on_target,omitempty"`
	// Threshold is the discriminator's acceptance threshold, when present.
	Threshold *float64 `json:"threshold,omitempty"`
}

// FeatureViolation is one out-of-range statistical feature.
type FeatureViolation struct {
	ID        string   `json:"id"`
	Value     float64  `json:"value"`
	Min       *float64 `json:"min,omitempty"`
	Max       *float64 `json:"max,omitempty"`
	Deviation float64  `json:"deviation"` // in stddev units, outside the range
	Weight    float64  `json:"weight"`
	Severity  string   `json:"severity"` // "hard" | "soft"
}

// LexicalViolation is one occurrence of an avoided term.
type LexicalViolation struct {
	Term  string `json:"term"`
	Start int    `json:"start"` // byte offset in the draft
	End   int    `json:"end"`
}

const epsilon = 1e-9

// Check scores draft text against a profile. It returns an error if no feature
// module exists for the profile's language, since scoring a draft with a
// different module than distilled the profile yields incomparable metrics.
func Check(draft string, p *stylespec.Profile) (Result, error) {
	if !distill.LanguageImplemented(p.Language) {
		return Result{}, distill.ErrUnsupportedLanguage(p.Language)
	}
	var res Result
	metrics := distill.Metrics(draft)

	var weightedSum, weightTotal float64
	for _, f := range p.Features {
		v, ok := metrics[f.ID]
		if !ok {
			continue
		}
		weightTotal += f.Weight
		dev := deviation(v, f.Target, f.Stddev)
		if dev <= 0 {
			continue
		}
		weightedSum += f.Weight * dev
		fv := FeatureViolation{
			ID:        f.ID,
			Value:     num.Round(v, 4),
			Min:       f.Target.Min,
			Max:       f.Target.Max,
			Deviation: num.Round(dev, 3),
			Weight:    f.Weight,
			Severity:  severity(f, v),
		}
		if fv.Severity == "hard" {
			res.HardViolations++
		}
		res.Features = append(res.Features, fv)
	}
	if weightTotal > 0 {
		res.Distance = num.Round(weightedSum/weightTotal, 4)
	}

	// Worst contributors first.
	sort.Slice(res.Features, func(i, j int) bool {
		ci := res.Features[i].Weight * res.Features[i].Deviation
		cj := res.Features[j].Weight * res.Features[j].Deviation
		return ci > cj
	})

	res.Lexical = findAvoided(draft, p.Lexicon.Avoided)
	res.HardViolations += len(res.Lexical)

	if p.Discriminator != nil {
		t := p.Discriminator.Threshold
		onTarget := res.Distance <= t
		res.OnTarget = &onTarget
		res.Threshold = &t
	}
	return res, nil
}

// deviation returns how far value lies outside the target range, measured in
// standard deviations. Returns 0 when in range. A zero stddev falls back to an
// absolute deviation against epsilon so hard invariants (max 0) still register.
func deviation(value float64, t stylespec.Target, stddev float64) float64 {
	scale := stddev
	if scale < epsilon {
		scale = epsilon
	}
	if t.Min != nil && value < *t.Min {
		return (*t.Min - value) / scale
	}
	if t.Max != nil && value > *t.Max {
		return (value - *t.Max) / scale
	}
	return 0
}

// severity classifies a feature violation. A feature whose target caps at zero
// (the em-dash invariant, and any future hard zero) is a hard violation when
// breached; everything else is soft.
func severity(f stylespec.Feature, value float64) string {
	if f.Target.Max != nil && *f.Target.Max == 0 && value > 0 {
		return "hard"
	}
	return "soft"
}

func findAvoided(draft string, avoided []string) []LexicalViolation {
	var out []LexicalViolation
	for _, term := range avoided {
		if term == "" {
			continue
		}
		from := 0
		for {
			i := strings.Index(draft[from:], term)
			if i < 0 {
				break
			}
			start := from + i
			out = append(out, LexicalViolation{Term: term, Start: start, End: start + len(term)})
			from = start + len(term)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}
