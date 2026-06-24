// Package distill turns a genre-homogeneous corpus into a style Profile: the
// offline half of bluepencil (DESIGN.md section 5). It computes a stylometric
// feature signature with per-feature acceptance ranges and mines a
// distinctiveness lexicon. Rule mining, exemplar embedding, and discriminator
// calibration are later increments; this is the "measurement before
// articulation" walking skeleton (DESIGN.md section 9).
//
// One distill run produces one register's profile. The caller is responsible
// for feeding a single-register corpus; a mixed-register corpus averages into
// mush (DESIGN.md section 4).
package distill

import (
	"math"

	"github.com/paulmooreparks/bluepencil/internal/text"
	"github.com/paulmooreparks/bluepencil/stylespec"
)

// DocInput is one corpus document.
type DocInput struct {
	Name string
	Text string
}

// Options tunes distillation.
type Options struct {
	// RangeK sets the half-width of each feature's acceptance range in standard
	// deviations: target = mean +/- RangeK*stddev. Default 1.5.
	RangeK float64
	// LexiconTopN, LexiconMinCount, LexiconMinDocs bound the preferred lexicon.
	LexiconTopN     int
	LexiconMinCount int
	LexiconMinDocs  int
}

// DefaultOptions returns sensible distillation defaults.
func DefaultOptions() Options {
	return Options{RangeK: 1.5, LexiconTopN: 40, LexiconMinCount: 3, LexiconMinDocs: 2}
}

// Distill builds a Profile from a single-register corpus in the given language.
// An empty language defaults to DefaultLanguage. It returns an error if no
// feature module exists for the language, rather than emitting a profile whose
// features were computed by the wrong (English) module.
func Distill(id, register, language string, docs []DocInput, opts Options) (*stylespec.Profile, error) {
	if language == "" {
		language = DefaultLanguage
	}
	if !LanguageImplemented(language) {
		return nil, ErrUnsupportedLanguage(language)
	}
	if opts.RangeK == 0 {
		opts = DefaultOptions()
	}

	// Per-metric samples, one value per document.
	samples := map[string][]float64{}
	var totalWords int
	var texts []string
	for _, d := range docs {
		texts = append(texts, d.Text)
		m := Metrics(d.Text)
		for id, v := range m {
			samples[id] = append(samples[id], v)
		}
		totalWords += len(text.Words(d.Text))
	}

	prof := &stylespec.Profile{
		ID:       id,
		Register: register,
		Language: language,
		Corpus:   stylespec.CorpusStats{Documents: len(docs), Words: totalWords},
	}

	for id, vals := range samples {
		mean, std := meanStddev(vals)
		f := stylespec.Feature{
			ID:     id,
			Mean:   round(mean, 4),
			Stddev: round(std, 4),
			Weight: weightFor(id, mean, std),
		}
		half := opts.RangeK * std
		min := mean - half
		if min < 0 && isNonNegative(id) {
			min = 0
		}
		max := mean + half
		f.Target = stylespec.Target{Min: stylespec.Ptr(round(min, 4)), Max: stylespec.Ptr(round(max, 4))}
		prof.Features = append(prof.Features, f)
	}

	// Bake the cross-register em-dash invariant (DESIGN.md section 4): regardless
	// of what the corpus shows, em-dashes are a hard zero. This belongs in a base
	// profile once inheritance is implemented; for now it is enforced inline.
	setHardInvariant(prof, MEmDashRate)

	lex := MineLexicon(texts, opts.LexiconTopN, opts.LexiconMinCount, opts.LexiconMinDocs)
	for _, l := range lex {
		prof.Lexicon.Preferred = append(prof.Lexicon.Preferred, l.Term)
	}
	prof.Lexicon.Avoided = []string{"—", "--"}

	return prof, nil
}

// weightFor assigns a feature weight. Stable features (low coefficient of
// variation across the corpus) describe the style more reliably and are
// weighted higher; noisy ones are downweighted. Bounded to [0.1, 1.0].
func weightFor(id string, mean, std float64) float64 {
	if mean == 0 {
		// A consistent zero (e.g. no exclamation points) is a strong signal.
		if std == 0 {
			return 0.8
		}
		return 0.4
	}
	cv := std / math.Abs(mean)
	w := 1.0 / (1.0 + cv)
	switch {
	case w < 0.1:
		return 0.1
	case w > 1.0:
		return 1.0
	default:
		return round(w, 3)
	}
}

func setHardInvariant(p *stylespec.Profile, id string) {
	for i := range p.Features {
		if p.Features[i].ID == id {
			p.Features[i].Target = stylespec.Target{Max: stylespec.Ptr(0)}
			p.Features[i].Weight = 1.0
			return
		}
	}
	p.Features = append(p.Features, stylespec.Feature{
		ID: id, Target: stylespec.Target{Max: stylespec.Ptr(0)}, Weight: 1.0,
	})
}

// isNonNegative reports whether a metric is a rate/count that cannot go below
// zero, so its lower target bound should clamp at 0 rather than go negative.
func isNonNegative(id string) bool {
	switch id {
	case MSentLenStddev:
		return true
	}
	// All "*_rate", cadence, and length metrics are non-negative; reading_grade
	// and the function-word rates likewise. The only signed metric in practice
	// is reading_grade, which can be negative for very simple text, so leave it
	// unclamped.
	return id != MReadingGrade
}

func round(f float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(f*p) / p
}
