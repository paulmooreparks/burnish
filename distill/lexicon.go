package distill

import (
	_ "embed"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/paulmooreparks/burnish/internal/text"
)

//go:embed data/baseline_en.txt
var baselineRaw string

// baselineFreq maps a common English word to its corpus count in the embedded
// general-English frequency table; baselineTotal is the sum. Together they give a
// real relative frequency for each word (built from public-domain prose + a modern
// sample, see cmd/mkbaseline), so distinctiveness is measured against genuine
// usage rather than a rank-modelled approximation.
var baselineFreq, baselineTotal = loadBaseline()

func loadBaseline() (map[string]int, int) {
	m := map[string]int{}
	total := 0
	for _, line := range strings.Split(baselineRaw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		word, countStr, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		c, err := strconv.Atoi(strings.TrimSpace(countStr))
		if err != nil || c <= 0 {
			continue
		}
		w := strings.ToLower(strings.TrimSpace(word))
		m[w] = c
		total += c
	}
	return m, total
}

// nonProse is markup/URL/code tokens and apostrophe-stripped contraction
// remnants that leak from imperfect corpus extraction (HTML essays, embedded
// snippets, lost apostrophes). They are never prose vocabulary, so they are
// dropped before distinctiveness mining rather than surfacing as "characteristic"
// terms. A defense against dirty corpora; clean prose corpora are unaffected.
var nonProse = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range []string{
		// markup / URL / code
		"http", "https", "www", "com", "org", "net", "html", "htm", "href", "src",
		"alt", "img", "div", "span", "css", "svg", "png", "jpg", "jpeg", "gif",
		"align", "valign", "width", "height", "margin", "padding", "border", "color",
		"font", "style", "class", "rel", "nofollow", "blank", "noopener", "utf",
		"json", "xml", "url", "uri", "api", "id", "ul", "li", "td", "tr", "th",
		"br", "hr", "px", "em", "rem", "auto", "none", "block", "inline", "flex",
		"max", "min", "var", "func", "int", "str", "args", "true", "false", "null",
		"figcaption", "utm", "nbsp", "amp", "quot", "apos",
		// apostrophe-stripped contraction remnants (never legitimate prose words)
		"ive", "dont", "cant", "wont", "thats", "youre", "theyre", "weve", "didnt",
		"doesnt", "isnt", "wasnt", "arent", "werent", "wouldnt", "couldnt", "shouldnt",
		"havent", "hasnt", "hadnt", "youll", "theyll", "youd", "theyd",
		"whats", "hes", "shes", "aint",
	} {
		m[w] = true
	}
	return m
}()

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
// Distinctiveness is log(corpus_rel / baseline_rel), where baseline_rel is the
// term's real relative frequency in the embedded general-English table (~10k
// words from public-domain prose + a modern sample; out-of-table terms are
// treated as rarer than any listed word). A term is eligible only if it clears
// minCount occurrences and appears in minDocs documents, so a single document's
// idiosyncrasies cannot dominate; non-prose tokens (markup, contraction remnants)
// are dropped. Avoided terms are not mined here: a positive-only corpus cannot
// reveal what the author shuns. Those are set explicitly on the profile (--avoid).
func MineLexicon(docs []string, topN, minCount, minDocs int) []LexiconResult {
	corpusCount := map[string]int{}
	docCount := map[string]int{}
	var total int

	for _, d := range docs {
		seen := map[string]bool{}
		for _, w := range text.Words(d) {
			if len([]rune(w)) < 3 || strings.Contains(w, "'") || nonProse[w] {
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

	var out []LexiconResult
	for term, c := range corpusCount {
		if c < minCount || docCount[term] < minDocs {
			continue
		}
		// Real relative frequencies. A term absent from the baseline is treated as
		// rarer than any listed word (count 1), so genuinely uncommon vocabulary
		// scores as distinctive while common English does not.
		baseCount := 1
		if bc, ok := baselineFreq[term]; ok {
			baseCount = bc
		}
		corpusRel := float64(c) / float64(total)
		baseRel := float64(baseCount) / float64(baselineTotal)
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
