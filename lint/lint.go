// Package lint runs the deterministic half of the massage loop: it scores a
// draft against a Profile's statistical features and lexicon, with zero LLM
// calls. Deterministic checks run first and free; only what survives reaches
// the judge (DESIGN.md sections 5 and 6). The headline output is a single
// distance-to-style number plus the specific features that are off.
package lint

import (
	"math"
	"sort"
	"strings"

	"github.com/paulmooreparks/burnish/distill"
	"github.com/paulmooreparks/burnish/internal/num"
	"github.com/paulmooreparks/burnish/internal/text"
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
	// no calibrated discriminator. Note: for drafts much shorter than the corpus
	// documents the distance is scored on a widened scale (sampling correction), so
	// the verdict is a softer gate than the calibrated TPR/FPR, which describe
	// document-scale text.
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
	// Use the same token count the rates were normalized by (segmented words, as
	// distill.Metrics does), so the sampling-variance correction matches the n the
	// per-1k rates were actually computed at.
	nWords := len(text.Segment(draft).AllWords())
	var corpusDocWords float64
	if p.Corpus.Documents > 0 {
		corpusDocWords = float64(p.Corpus.Words) / float64(p.Corpus.Documents)
	}

	var weightedSum, weightTotal float64
	for _, f := range p.Features {
		v, ok := metrics[f.ID]
		if !ok {
			continue
		}
		weightTotal += f.Weight
		dev := deviation(v, f.Target, scaleFor(f, nWords, corpusDocWords))
		if dev <= 0 {
			continue
		}
		weightedSum += f.Weight * dev
		// Feature breaches are always SOFT: they measure distance from the corpus
		// distribution, not a gate. Hard invariants are opt-in and come only from the
		// avoided lexicon (below) and the deterministic rule layer (judge). A feature
		// that merely happened to be absent in the corpus must not become a hard
		// "must be zero" rule (burnish-23).
		res.Features = append(res.Features, FeatureViolation{
			ID:        f.ID,
			Value:     num.Round(v, 4),
			Min:       f.Target.Min,
			Max:       f.Target.Max,
			Deviation: num.Round(dev, 3),
			Weight:    f.Weight,
			Severity:  "soft",
		})
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

// scaleFor returns the deviation scale for a feature: the corpus stddev, widened
// for per-1000-word rate features when the draft is shorter than the corpus
// documents. A per-1k rate over n words has sampling variance ~ 1000*mean/n. The
// corpus stddev already embeds that variance at the corpus's own document length,
// so only the EXTRA variance from a shorter draft is added (in quadrature, and
// never negative). corpusDocWords is the mean corpus document length, an
// approximation of the per-document n the stddev embeds (exact only for
// equal-length docs). At or above corpus-document length the scale is unchanged,
// so a threshold calibrated on full documents stays valid; far below it, a single
// token no longer reads as a large outlier.
func scaleFor(f stylespec.Feature, nWords int, corpusDocWords float64) float64 {
	if nWords <= 0 || !distill.IsPer1kRate(f.ID) {
		return f.Stddev
	}
	draftVar := 1000.0 * f.Mean / float64(nWords)
	var corpusVar float64
	if corpusDocWords > 0 {
		corpusVar = 1000.0 * f.Mean / corpusDocWords
	}
	extra := draftVar - corpusVar
	if extra < 0 {
		extra = 0
	}
	s := math.Sqrt(f.Stddev*f.Stddev + extra)
	// Floor the scale at the rate of a single occurrence in the draft (1000/nWords
	// per 1k). A feature absent from the corpus (mean=std=0, e.g. a punctuation the
	// author happened not to use) would otherwise give a ~0 scale, so a lone
	// occurrence reads as a near-infinite outlier. The floor makes each occurrence
	// worth about one unit of deviation: a bounded soft signal that still rises with
	// frequency, rather than a hard or 84-sigma failure on the first one (burnish-23).
	if floor := 1000.0 / float64(nWords); s < floor {
		s = floor
	}
	return s
}

// deviation returns how far value lies outside the target range, measured in
// scale units. Returns 0 when in range. A near-zero scale falls back to epsilon
// only as a divide-by-zero guard (scaleFor floors per-1k rates well above it).
func deviation(value float64, t stylespec.Target, scale float64) float64 {
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
