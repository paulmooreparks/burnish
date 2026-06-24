package distill

import (
	_ "embed"
	"math"
	"sort"
	"strings"

	"github.com/paulmooreparks/bluepencil/internal/text"
)

//go:embed data/baseline_en.txt
var baselineRaw string

// baselineRank maps a common English word to its frequency rank (1 = most
// common). Words absent from the list are treated as rare (see tailRank).
var baselineRank = loadBaseline()

func loadBaseline() map[string]int {
	m := map[string]int{}
	rank := 0
	for _, line := range strings.Split(baselineRaw, "\n") {
		w := strings.TrimSpace(line)
		if w == "" || strings.HasPrefix(w, "#") {
			continue
		}
		rank++
		m[strings.ToLower(w)] = rank
	}
	return m
}

// LexiconResult is a scored distinctiveness ranking.
type LexiconResult struct {
	Term  string
	Score float64 // log(corpus_rel / baseline_rel); higher = more distinctive
	Count int     // corpus occurrences
	Docs  int     // documents the term appears in
}

// MineLexicon ranks corpus vocabulary by distinctiveness against the embedded
// general-English baseline, returning the top preferred terms.
//
// Distinctiveness is log(corpus_rel / baseline_rel), where baseline_rel is
// modeled Zipfian from each word's rank (freq proportional to 1/rank) and
// out-of-list words are assigned a rare tail rank. A term is eligible only if
// it clears minCount occurrences and appears in minDocs documents, so a single
// document's idiosyncrasies cannot dominate.
//
// Known limit: the baseline is a small seed list, so mid-frequency English
// words absent from it score as distinctive. The minDocs/minCount floors and
// the short-word filter blunt this; a larger baseline is the real fix and is
// tracked as a later upgrade. Avoided terms are not mined here: a positive-only
// corpus cannot reveal what the author shuns. Those come from the base profile
// and, later, the LLM-voice decoy corpus.
func MineLexicon(docs []string, topN, minCount, minDocs int) []LexiconResult {
	corpusCount := map[string]int{}
	docCount := map[string]int{}
	var total int

	for _, d := range docs {
		seen := map[string]bool{}
		for _, w := range text.Words(d) {
			if len([]rune(w)) < 3 || strings.Contains(w, "'") {
				continue
			}
			corpusCount[w]++
			total++
			if !seen[w] {
				docCount[w]++
				seen[w] = true
			}
		}
	}
	if total == 0 {
		return nil
	}

	tailRank := len(baselineRank) + 1
	// Zipf normalizer over the modeled baseline distribution (1/rank terms).
	var z float64
	for r := 1; r <= tailRank; r++ {
		z += 1.0 / float64(r)
	}

	var out []LexiconResult
	for term, c := range corpusCount {
		if c < minCount || docCount[term] < minDocs {
			continue
		}
		rank := tailRank
		if r, ok := baselineRank[term]; ok {
			rank = r
		}
		corpusRel := float64(c) / float64(total)
		baseRel := (1.0 / float64(rank)) / z
		score := math.Log(corpusRel / baseRel)
		if score <= 0 {
			continue
		}
		out = append(out, LexiconResult{Term: term, Score: score, Count: c, Docs: docCount[term]})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Term < out[j].Term
	})
	if topN > 0 && len(out) > topN {
		out = out[:topN]
	}
	return out
}
