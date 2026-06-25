package distill

import (
	"math"
	"strings"

	"github.com/paulmooreparks/burnish/internal/text"
)

// Metric ids. These strings are the stable contract between distill (which
// computes target ranges over a corpus) and lint (which scores a draft against
// them). Function-word frequencies use the "fw." prefix; everything else is a
// named stylometric signal. Rates are per 1000 words unless noted.
const (
	MSentLenMean   = "sentence_len_mean"   // mean words per sentence
	MSentLenStddev = "sentence_len_stddev" // within-doc sentence-length variation
	MParaLenMean   = "paragraph_len_mean"  // mean sentences per paragraph
	MWordLenMean   = "word_len_mean"       // mean characters per word
	MReadingGrade  = "reading_grade"       // Flesch-Kincaid grade level

	MEmDashRate     = "em_dash_rate"     // per 1000 words (includes "--")
	MSemicolonRate  = "semicolon_rate"   // per 1000 words
	MColonRate      = "colon_rate"       // per 1000 words
	MParenRate      = "paren_rate"       // per 1000 words (open parens)
	MExclaimRate    = "exclaim_rate"     // per 1000 words
	MQuestionRate   = "question_rate"    // per 1000 words
	MCommaRate      = "comma_rate"       // per 1000 words
	MContractRate   = "contraction_rate" // per 1000 words
	MHedgeRate      = "hedge_rate"       // per 1000 words
	MFirstPersRate  = "first_person_rate"
	MSecondPersRate = "second_person_rate"
	MThirdPersRate  = "third_person_rate"

	MHeadingCadence = "heading_cadence" // markdown headings per 100 lines
	MListCadence    = "list_cadence"    // markdown list items per 100 lines
)

// fwPrefix marks function-word frequency metrics, the classic
// authorship-attribution signal.
const fwPrefix = "fw."

// IsPer1kRate reports whether a metric id is a per-1000-word count rate. Such
// estimates have high sampling variance on short drafts (one token in 50 words is
// 20/1k), so lint widens their deviation scale by the draft's sampling error to
// keep a single token from reading as a huge outlier. The cadence metrics are
// per-100-lines, not per-1k words, so they are excluded.
func IsPer1kRate(id string) bool {
	return strings.HasPrefix(id, fwPrefix) || strings.HasSuffix(id, "_rate")
}

// functionWords is a fixed, ordered set of high-frequency function words whose
// relative usage rates form a stylometric fingerprint largely independent of
// topic. Kept modest; expand once the signal proves out.
var functionWords = []string{
	"the", "of", "and", "to", "a", "in", "that", "is", "it", "for",
	"as", "with", "but", "not", "this", "be", "by", "are", "or", "an",
	"from", "at", "which", "you", "we", "they", "its", "into", "than", "then",
}

var hedgeWords = map[string]bool{
	"maybe": true, "perhaps": true, "possibly": true, "might": true,
	"could": true, "somewhat": true, "fairly": true, "relatively": true,
	"generally": true, "usually": true, "likely": true, "probably": true,
	"seems": true, "appears": true, "arguably": true, "presumably": true,
	"sort": true, "roughly": true, "approximately": true, "essentially": true,
}

var firstPerson = map[string]bool{
	"i": true, "me": true, "my": true, "mine": true, "we": true, "us": true, "our": true, "ours": true,
}
var secondPerson = map[string]bool{
	"you": true, "your": true, "yours": true,
}
var thirdPerson = map[string]bool{
	"he": true, "him": true, "his": true, "she": true, "her": true, "hers": true,
	"it": true, "its": true, "they": true, "them": true, "their": true, "theirs": true,
}

var contractionSuffixes = []string{"n't", "'re", "'ve", "'ll", "'d", "'m", "'s"}

// Metrics computes the full stylometric signature of a document as a flat
// map keyed by metric id. distill aggregates these across a corpus into target
// ranges; lint compares a single draft's map against those ranges.
func Metrics(raw string) map[string]float64 {
	doc := text.Segment(raw)
	m := map[string]float64{}

	sents := doc.AllSentences()
	words := doc.AllWords()
	nWords := float64(len(words))
	if nWords == 0 {
		return m
	}
	per1k := 1000.0 / nWords

	// Sentence-length distribution.
	if len(sents) > 0 {
		lens := make([]float64, len(sents))
		var totalSyll float64
		for i, s := range sents {
			lens[i] = float64(len(s.Words))
		}
		for _, w := range words {
			totalSyll += float64(text.Syllables(w))
		}
		mean, std := meanStddev(lens)
		m[MSentLenMean] = mean
		m[MSentLenStddev] = std
		// Flesch-Kincaid grade level.
		wps := nWords / float64(len(sents))
		spw := totalSyll / nWords
		m[MReadingGrade] = 0.39*wps + 11.8*spw - 15.59
	}

	// Paragraph length in sentences.
	if len(doc.Paragraphs) > 0 {
		plens := make([]float64, len(doc.Paragraphs))
		for i, p := range doc.Paragraphs {
			plens[i] = float64(len(p.Sentences))
		}
		mean, _ := meanStddev(plens)
		m[MParaLenMean] = mean
	}

	// Word length.
	var totalChars float64
	for _, w := range words {
		totalChars += float64(len([]rune(w)))
	}
	m[MWordLenMean] = totalChars / nWords

	// Punctuation rates, counted over the raw text.
	emDashes := strings.Count(raw, "—") + strings.Count(raw, "--")
	m[MEmDashRate] = float64(emDashes) * per1k
	m[MSemicolonRate] = float64(strings.Count(raw, ";")) * per1k
	m[MColonRate] = float64(strings.Count(raw, ":")) * per1k
	m[MParenRate] = float64(strings.Count(raw, "(")) * per1k
	m[MExclaimRate] = float64(strings.Count(raw, "!")) * per1k
	m[MQuestionRate] = float64(strings.Count(raw, "?")) * per1k
	m[MCommaRate] = float64(strings.Count(raw, ",")) * per1k

	// Lexical-class rates over word tokens.
	var contractions, hedges, p1, p2, p3 float64
	for _, w := range words {
		if strings.Contains(w, "'") {
			for _, suf := range contractionSuffixes {
				if strings.HasSuffix(w, suf) {
					contractions++
					break
				}
			}
		}
		if hedgeWords[w] {
			hedges++
		}
		switch {
		case firstPerson[w]:
			p1++
		case secondPerson[w]:
			p2++
		case thirdPerson[w]:
			p3++
		}
	}
	m[MContractRate] = contractions * per1k
	m[MHedgeRate] = hedges * per1k
	m[MFirstPersRate] = p1 * per1k
	m[MSecondPersRate] = p2 * per1k
	m[MThirdPersRate] = p3 * per1k

	// Function-word frequencies (fingerprint), as rate per 1000 words.
	counts := map[string]float64{}
	for _, w := range words {
		counts[w]++
	}
	for _, fw := range functionWords {
		m[fwPrefix+fw] = counts[fw] * per1k
	}

	// Structural cadence over raw lines.
	var headings, listItems float64
	for _, ln := range doc.Lines {
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "#"):
			headings++
		case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "), strings.HasPrefix(t, "+ "):
			listItems++
		}
	}
	if n := float64(len(doc.Lines)); n > 0 {
		m[MHeadingCadence] = headings * 100.0 / n
		m[MListCadence] = listItems * 100.0 / n
	}

	return m
}

func meanStddev(xs []float64) (mean, std float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean = sum / float64(len(xs))
	var sq float64
	for _, x := range xs {
		d := x - mean
		sq += d * d
	}
	std = math.Sqrt(sq / float64(len(xs)))
	return mean, std
}
