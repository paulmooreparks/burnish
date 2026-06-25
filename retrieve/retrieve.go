package retrieve

import (
	"math"
	"sort"
	"strings"

	"github.com/paulmooreparks/burnish/internal/text"
)

// This is the first-cut exemplar bank: TF-IDF cosine retrieval over corpus
// chunks, with no embedding model or vector-store dependency. It returns
// authentic target-style passages (every chunk is corpus text) that are
// topically relevant to a draft (term overlap), to seed the revision step as
// targeted few-shot. Dense semantic embeddings are the documented upgrade: they
// match by meaning rather than shared terms, reusing this same chunk + retrieve
// shape. See DESIGN.md section 3.

// Document is one corpus document to chunk and index.
type Document struct {
	Name string
	Text string
}

// Chunk is a retrievable passage of target-style text.
type Chunk struct {
	Source string `json:"source"`
	// Index is the chunk's ordinal within its source after filtering (a stable id
	// for display and tie-breaking), not the raw paragraph number.
	Index int    `json:"index"`
	Text  string `json:"text"`
	vec   map[string]float64
}

// Result is one retrieved exemplar with its similarity score.
type Result struct {
	Chunk Chunk   `json:"chunk"`
	Score float64 `json:"score"` // cosine similarity in [0,1]
}

// Bank is an in-memory TF-IDF index over corpus chunks.
type Bank struct {
	Chunks []Chunk
	idf    map[string]float64
}

// Options tunes chunking.
type Options struct {
	// MinWords drops chunks shorter than this (too little to exemplify). Default 20.
	MinWords int
}

// DefaultOptions returns sensible chunking defaults.
func DefaultOptions() Options { return Options{MinWords: 20} }

// Build chunks the documents by paragraph and indexes them by TF-IDF.
func Build(docs []Document, opts Options) *Bank {
	if opts.MinWords <= 0 {
		opts.MinWords = DefaultOptions().MinWords
	}
	b := &Bank{idf: map[string]float64{}}

	type raw struct {
		chunk  Chunk
		counts map[string]float64
	}
	var raws []raw
	df := map[string]float64{}
	seen := map[string]bool{} // dedup identical passages (boilerplate) for diverse few-shot
	for _, d := range docs {
		for i, p := range text.Segment(d.Text).Paragraphs {
			toks := terms(p.Text)
			if len(toks) < opts.MinWords {
				continue
			}
			key := strings.TrimSpace(p.Text)
			if seen[key] {
				continue
			}
			seen[key] = true
			counts := map[string]float64{}
			for _, t := range toks {
				counts[t]++
			}
			for t := range counts {
				df[t]++
			}
			raws = append(raws, raw{Chunk{Source: d.Name, Index: i, Text: p.Text}, counts})
		}
	}

	n := float64(len(raws))
	// Smoothed IDF, sklearn-style: 1 + ln((n+1)/(df+1)). The +1 floor keeps every
	// observed term strictly positive, so terms that appear in every chunk (common
	// in a small, single-register corpus, and the only term in a 1-chunk corpus)
	// still carry weight rather than being zeroed out of all vectors.
	for t, d := range df {
		b.idf[t] = 1 + math.Log((n+1)/(d+1))
	}
	for _, r := range raws {
		r.chunk.vec = normalize(tfidf(r.counts, b.idf))
		b.Chunks = append(b.Chunks, r.chunk)
	}
	return b
}

// Retrieve returns the k chunks most similar to the query, highest score first.
// k <= 0 returns all scored chunks. The Bank is read-only here, so concurrent
// Retrieve calls are safe.
func (b *Bank) Retrieve(query string, k int) []Result {
	if len(b.Chunks) == 0 {
		return nil
	}
	counts := map[string]float64{}
	for _, t := range terms(query) {
		counts[t]++
	}
	qvec := normalize(tfidf(counts, b.idf))
	if len(qvec) == 0 {
		return nil // query shares no indexed terms with the corpus
	}

	results := make([]Result, 0, len(b.Chunks))
	for _, c := range b.Chunks {
		if s := dot(qvec, c.vec); s > 0 {
			results = append(results, Result{Chunk: c, Score: s})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		// Total-order tie-break for determinism, robust to duplicate source names.
		if results[i].Chunk.Source != results[j].Chunk.Source {
			return results[i].Chunk.Source < results[j].Chunk.Source
		}
		if results[i].Chunk.Index != results[j].Chunk.Index {
			return results[i].Chunk.Index < results[j].Chunk.Index
		}
		return results[i].Chunk.Text < results[j].Chunk.Text
	})
	if k > 0 && len(results) > k {
		results = results[:k]
	}
	return results
}

// terms tokenizes text into lowercase content words (length >= 3, no
// contractions), matching the lexicon miner's filtering so the vocabulary is
// consistent across the engine.
func terms(s string) []string {
	var out []string
	for _, w := range text.Words(s) {
		if len([]rune(w)) >= 3 && !containsApostrophe(w) {
			out = append(out, w)
		}
	}
	return out
}

func containsApostrophe(w string) bool {
	for _, r := range w {
		if r == '\'' || r == '’' {
			return true
		}
	}
	return false
}

func tfidf(counts, idf map[string]float64) map[string]float64 {
	vec := map[string]float64{}
	for t, c := range counts {
		if w, ok := idf[t]; ok && w > 0 {
			vec[t] = c * w
		}
	}
	return vec
}

// normalize scales vec to unit L2 length in place and returns it.
func normalize(vec map[string]float64) map[string]float64 {
	var sum float64
	for _, v := range vec {
		sum += v * v
	}
	if sum == 0 {
		return map[string]float64{}
	}
	norm := math.Sqrt(sum)
	for t := range vec {
		vec[t] /= norm
	}
	return vec
}

// dot is the cosine similarity of two already-normalized sparse vectors. Iterates
// the smaller map for efficiency.
func dot(a, b map[string]float64) float64 {
	if len(b) < len(a) {
		a, b = b, a
	}
	var s float64
	for t, va := range a {
		s += va * b[t]
	}
	return s
}
