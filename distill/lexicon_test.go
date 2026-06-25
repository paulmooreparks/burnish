package distill

import "testing"

func TestBaselineLoaded(t *testing.T) {
	if baselineTotal < 100000 {
		t.Errorf("baseline total tokens = %d, want a large corpus", baselineTotal)
	}
	if baselineFreq["the"] < baselineFreq["because"] || baselineFreq["the"] == 0 {
		t.Errorf("baseline frequencies look wrong: the=%d because=%d", baselineFreq["the"], baselineFreq["because"])
	}
}

func TestMineLexiconFlagsDistinctive(t *testing.T) {
	// "frobnicate"/"foobaz" are not general English, so they score as distinctive;
	// "https"/"ive"/"thats" are non-prose and must be dropped before scoring.
	doc := "We frobnicate the system to foobaz it. The foobaz must frobnicate cleanly. https ive thats."
	docs := []string{doc, doc, doc}
	res := MineLexicon(docs, 40, 2, 2)

	got := map[string]bool{}
	for _, r := range res {
		got[r.Term] = true
	}
	if !got["frobnicate"] || !got["foobaz"] {
		t.Errorf("expected distinctive coinages flagged, got %v", got)
	}
	for _, noise := range []string{"https", "ive", "thats"} {
		if got[noise] {
			t.Errorf("non-prose token %q should be dropped", noise)
		}
	}
	// A higher-than-baseline coinage outscores a common word that survives scoring.
	score := map[string]float64{}
	for _, r := range res {
		score[r.Term] = r.Score
	}
	if score["foobaz"] == 0 {
		t.Error("foobaz should have a positive distinctiveness score")
	}
}
