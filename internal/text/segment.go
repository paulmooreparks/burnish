// Package text provides deterministic, dependency-free segmentation of prose
// into paragraphs, sentences, words, and syllables. It is the shared substrate
// for every stylometric measurement in burnish: feature extraction and
// linting both segment identically, so a profile distilled from a corpus is
// directly comparable to a draft scored against it.
//
// The segmentation is intentionally lightweight (regex + rules, no NLP model).
// Stylometric signals are aggregate rates, so small per-sentence boundary errors
// wash out across a document; what matters is that distill and lint segment the
// same way.
package text

import (
	"regexp"
	"strings"
)

// Doc is a segmented document.
type Doc struct {
	Raw        string
	Paragraphs []Paragraph
	// Lines is the raw line set, retained for heading/list cadence metrics.
	Lines []string
}

// Paragraph is a block of prose separated from others by a blank line.
type Paragraph struct {
	Text      string
	Sentences []Sentence
}

// Sentence is a single sentence within a paragraph.
type Sentence struct {
	Text  string
	Words []string
}

var (
	// wordRe matches word tokens using Unicode letter (\p{L}) and combining-mark
	// (\p{M}) classes, so accented Latin and non-Latin scripts are not silently
	// dropped, allowing internal apostrophes (straight or curly) for contractions
	// and possessives. Note: this still splits on whitespace, so for scripts that
	// do not delimit words with spaces (Chinese, Japanese, Thai) a run of
	// characters is captured as one token. Correct word segmentation there is a
	// per-language-module concern (DESIGN.md section 11); this regex only ensures
	// the foundation is Unicode-clean, not that every language is handled.
	wordRe = regexp.MustCompile(`[\p{L}\p{M}]+(?:['’][\p{L}\p{M}]+)*`)
	// sentenceSplitRe splits on terminal punctuation. ASCII terminators must be
	// followed by whitespace or end of string (so "3.14" and "U.S." do not split);
	// fullwidth/CJK and other-script terminators split unconditionally because
	// they are not generally followed by a space. Deliberately simple; abbreviation
	// handling is out of scope because aggregate rates tolerate the noise, and
	// finer per-language rules belong in the language module.
	sentenceSplitRe = regexp.MustCompile(`[.!?]+(?:\s+|$)|[。！？؟।]+`)
)

// Segment parses raw text into a Doc.
func Segment(raw string) *Doc {
	d := &Doc{Raw: raw, Lines: strings.Split(raw, "\n")}
	for _, block := range splitParagraphs(raw) {
		p := Paragraph{Text: block}
		for _, s := range splitSentences(block) {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			p.Sentences = append(p.Sentences, Sentence{Text: s, Words: Words(s)})
		}
		if len(p.Sentences) > 0 {
			d.Paragraphs = append(d.Paragraphs, p)
		}
	}
	return d
}

// Words extracts lowercase word tokens from a fragment.
func Words(s string) []string {
	matches := wordRe.FindAllString(s, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, strings.ToLower(m))
	}
	return out
}

// AllWords returns every word token across the whole document.
func (d *Doc) AllWords() []string {
	var out []string
	for _, p := range d.Paragraphs {
		for _, s := range p.Sentences {
			out = append(out, s.Words...)
		}
	}
	return out
}

// AllSentences returns every sentence across the whole document.
func (d *Doc) AllSentences() []Sentence {
	var out []Sentence
	for _, p := range d.Paragraphs {
		out = append(out, p.Sentences...)
	}
	return out
}

func splitParagraphs(raw string) []string {
	// A blank line (possibly with whitespace) separates paragraphs.
	blank := regexp.MustCompile(`\n\s*\n`)
	parts := blank.Split(raw, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitSentences(block string) []string {
	// Flatten internal newlines so a wrapped sentence is one sentence.
	flat := strings.Join(strings.Fields(block), " ")
	if flat == "" {
		return nil
	}
	idx := sentenceSplitRe.FindAllStringIndex(flat, -1)
	if len(idx) == 0 {
		return []string{flat}
	}
	var out []string
	prev := 0
	for _, loc := range idx {
		out = append(out, flat[prev:loc[1]])
		prev = loc[1]
	}
	if prev < len(flat) {
		out = append(out, flat[prev:])
	}
	return out
}

// Syllables estimates the syllable count of a word using a vowel-group
// heuristic with a silent-trailing-e correction. It is the standard
// approximation used by readability scores; exact accuracy is unnecessary
// because Flesch-Kincaid is itself an aggregate estimate.
func Syllables(word string) int {
	word = strings.ToLower(strings.Trim(word, "'"))
	if word == "" {
		return 0
	}
	count := 0
	prevVowel := false
	for _, r := range word {
		isVowel := strings.ContainsRune("aeiouy", r)
		if isVowel && !prevVowel {
			count++
		}
		prevVowel = isVowel
	}
	// Silent trailing 'e' (but never reduce below one).
	if strings.HasSuffix(word, "e") && count > 1 {
		count--
	}
	if count < 1 {
		count = 1
	}
	return count
}
