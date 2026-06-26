package lint

import (
	"math"
	"testing"

	"github.com/paulmooreparks/burnish/stylespec"
)

func TestScaleForRateInflation(t *testing.T) {
	f := stylespec.Feature{ID: "fw.the", Mean: 50, Stddev: 10}
	const corpusLen = 640.0

	// At corpus document length, no extra sampling noise: scale == stddev.
	if got := scaleFor(f, int(corpusLen), corpusLen); math.Abs(got-10) > 1e-6 {
		t.Errorf("at corpus length scale = %v, want 10 (== stddev)", got)
	}
	// Far below corpus length, the scale widens (single tokens stop being outliers).
	if got := scaleFor(f, 50, corpusLen); got <= 10 {
		t.Errorf("short draft scale = %v, want > 10", got)
	}
	// Longer than corpus docs: never sharpen below stddev (clamped).
	if got := scaleFor(f, 5000, corpusLen); math.Abs(got-10) > 1e-6 {
		t.Errorf("long draft scale = %v, want 10 (clamped)", got)
	}
	// Shorter draft inflates more than a less-short one (monotone).
	if scaleFor(f, 30, corpusLen) <= scaleFor(f, 120, corpusLen) {
		t.Error("expected shorter draft to inflate scale more")
	}
}

func TestScaleForNonRateUnaffected(t *testing.T) {
	f := stylespec.Feature{ID: "sentence_len_mean", Mean: 20, Stddev: 5}
	if got := scaleFor(f, 30, 640); got != 5 {
		t.Errorf("non-rate feature scale = %v, want 5 (stddev, no inflation)", got)
	}
}

// TestZeroRateFeatureIsSoftAndBounded is the burnish-23 regression: a feature
// absent across the corpus (paren_rate mean=std=0, target [0,0]) must NOT make a
// draft containing that feature a hard violation, and its soft deviation must be
// bounded (about one unit per occurrence), not a near-infinite outlier.
func TestZeroRateFeatureIsSoftAndBounded(t *testing.T) {
	p := &stylespec.Profile{
		Language: "en",
		Corpus:   stylespec.CorpusStats{Documents: 12, Words: 12000},
		Features: []stylespec.Feature{
			{ID: "paren_rate", Mean: 0, Stddev: 0, Weight: 1,
				Target: stylespec.Target{Min: stylespec.Ptr(0.0), Max: stylespec.Ptr(0.0)}},
		},
	}
	// A normal sentence with one parenthetical, ~12 words.
	draft := "The release ships today (after review) and the team is ready."
	res, err := Check(draft, p)
	if err != nil {
		t.Fatal(err)
	}
	if res.HardViolations != 0 {
		t.Errorf("a zero-rate feature must not hard-fail a draft, got %d hard violations", res.HardViolations)
	}
	if len(res.Features) != 1 {
		t.Fatalf("expected the paren_rate feature to register one (soft) violation, got %d", len(res.Features))
	}
	if res.Features[0].Severity != "soft" {
		t.Errorf("feature severity = %q, want soft", res.Features[0].Severity)
	}
	// One occurrence should be a small handful of units, not dozens.
	if d := res.Features[0].Deviation; d > 3 {
		t.Errorf("single-occurrence deviation = %.1f, want bounded (<= 3)", d)
	}
}

func TestCheckShortDraftDampensSingleToken(t *testing.T) {
	// A profile where "fw.an" is rare (mean 4) with modest spread; a long corpus.
	p := &stylespec.Profile{
		Language: "en",
		Corpus:   stylespec.CorpusStats{Documents: 10, Words: 6400},
		Features: []stylespec.Feature{
			{ID: "fw.an", Mean: 4, Stddev: 2, Weight: 1, Target: stylespec.Target{Min: stylespec.Ptr(0.0), Max: stylespec.Ptr(8.0)}},
		},
	}
	// One "an" in ~12 words is 83/1k: far outside [0,8], but from a tiny sample.
	short := "I ate an apple and then walked to the shop nearby today"
	res, err := Check(short, p)
	if err != nil {
		t.Fatal(err)
	}
	// Without sampling correction this would be ~37 stddev out; with it, modest.
	if len(res.Features) == 1 && res.Features[0].Deviation > 5 {
		t.Errorf("single-token deviation not dampened: %.1f stddev", res.Features[0].Deviation)
	}
}
