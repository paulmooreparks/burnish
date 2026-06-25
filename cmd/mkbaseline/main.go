// Command mkbaseline builds the general-English frequency table embedded as the
// distinctiveness baseline (distill/data/baseline_en.txt). It reads public-domain
// prose (Project Gutenberg) and a modern sample (Wikipedia API JSON), strips
// boilerplate, tokenizes the same way the lexicon miner does, and emits the
// top-N words as "word<TAB>count" sorted by frequency. Only the derived counts
// are committed, never the source text. Regenerate via scripts/fetch-baseline-corpus.sh.
//
//	go run ./cmd/mkbaseline <gutenberg-dir> <wiki-json-dir> <topN> > distill/data/baseline_en.txt
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/paulmooreparks/burnish/internal/text"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: mkbaseline <gutenberg-dir> <wiki-json-dir> <topN>")
		os.Exit(2)
	}
	gutDir, wikiDir := os.Args[1], os.Args[2]
	topN, _ := strconv.Atoi(os.Args[3])

	counts := map[string]int{}
	var total, gutWords, wikiWords, gutFilesUsed int

	tally := func(s string) int {
		n := 0
		for _, w := range text.Words(s) {
			counts[w]++
			n++
		}
		total += n
		return n
	}

	// Gutenberg: strip the *** START/END OF THE PROJECT GUTENBERG EBOOK *** banners.
	// Skip files too small to be a real book (e.g. an error/blocked page).
	gutFiles, _ := filepath.Glob(filepath.Join(gutDir, "*.txt"))
	for _, f := range gutFiles {
		b, err := os.ReadFile(f)
		if err != nil || len(b) < 20000 {
			continue
		}
		gutWords += tally(stripGutenberg(string(b)))
		gutFilesUsed++
	}

	// Wikipedia API JSON: concatenate the plaintext "extract" of each page.
	wikiFiles, _ := filepath.Glob(filepath.Join(wikiDir, "*.json"))
	for _, f := range wikiFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var resp struct {
			Query struct {
				Pages map[string]struct {
					Extract string `json:"extract"`
				} `json:"pages"`
			} `json:"query"`
		}
		if json.Unmarshal(b, &resp) != nil {
			continue
		}
		for _, p := range resp.Query.Pages {
			wikiWords += tally(p.Extract)
		}
	}

	type wc struct {
		w string
		c int
	}
	ranked := make([]wc, 0, len(counts))
	for w, c := range counts {
		ranked = append(ranked, wc{w, c})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].c != ranked[j].c {
			return ranked[i].c > ranked[j].c
		}
		return ranked[i].w < ranked[j].w
	})
	if topN > 0 && len(ranked) > topN {
		ranked = ranked[:topN]
	}

	fmt.Printf("# General-English word-frequency baseline for distinctiveness mining.\n")
	fmt.Printf("# word<TAB>count, sorted by frequency. Derived counts only (not the text)\n")
	fmt.Printf("# from %d Project Gutenberg public-domain works (%d words) + a Wikipedia modern\n", gutFilesUsed, gutWords)
	fmt.Printf("# sample (%d words); %d total tokens, %d distinct, top %d kept. Regenerate via\n", wikiWords, total, len(counts), len(ranked))
	fmt.Printf("# scripts/fetch-baseline-corpus.sh. Blank lines and #-comments are ignored.\n")
	for _, r := range ranked {
		fmt.Printf("%s\t%d\n", r.w, r.c)
	}
}

func stripGutenberg(s string) string {
	if i := strings.Index(s, "*** START OF THE PROJECT GUTENBERG"); i >= 0 {
		if nl := strings.IndexByte(s[i:], '\n'); nl >= 0 {
			s = s[i+nl+1:]
		}
	}
	if i := strings.Index(s, "*** END OF THE PROJECT GUTENBERG"); i >= 0 {
		s = s[:i]
	}
	return s
}
