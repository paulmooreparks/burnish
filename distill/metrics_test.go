package distill

import (
	"math"
	"testing"
)

func TestMetricsBasic(t *testing.T) {
	text := "The cat sat. The dog ran fast! Did it work?\n\nWe can't be sure; perhaps not."
	m := Metrics(text)

	// Three sentences in para 1, one in para 2: 4 sentences total.
	if m[MExclaimRate] <= 0 {
		t.Errorf("expected nonzero exclaim rate, got %v", m[MExclaimRate])
	}
	if m[MQuestionRate] <= 0 {
		t.Errorf("expected nonzero question rate, got %v", m[MQuestionRate])
	}
	if m[MSemicolonRate] <= 0 {
		t.Errorf("expected nonzero semicolon rate, got %v", m[MSemicolonRate])
	}
	if m[MContractRate] <= 0 {
		t.Errorf("expected to detect the contraction can't, got %v", m[MContractRate])
	}
	if m[MHedgeRate] <= 0 {
		t.Errorf("expected to detect hedge word perhaps, got %v", m[MHedgeRate])
	}
	if m[MFirstPersRate] <= 0 {
		t.Errorf("expected first-person pronoun we, got %v", m[MFirstPersRate])
	}
}

func TestEmDashDetection(t *testing.T) {
	withDash := Metrics("This is a sentence — with an em dash in it here.")
	if withDash[MEmDashRate] <= 0 {
		t.Errorf("em-dash not detected: %v", withDash[MEmDashRate])
	}
	withDoubleHyphen := Metrics("This is a sentence -- with a double hyphen here.")
	if withDoubleHyphen[MEmDashRate] <= 0 {
		t.Errorf("double-hyphen stand-in not detected: %v", withDoubleHyphen[MEmDashRate])
	}
	clean := Metrics("This sentence has no em dash at all in it.")
	if clean[MEmDashRate] != 0 {
		t.Errorf("false-positive em-dash: %v", clean[MEmDashRate])
	}
}

func TestSyllablesSane(t *testing.T) {
	// Aggregate sanity: a longer-word document should read at a higher grade.
	simple := Metrics("The cat sat on the mat. The dog ran to the log.")
	complex := Metrics("Architectural decomposition necessitates rigorous deterministic verification procedures.")
	if complex[MReadingGrade] <= simple[MReadingGrade] {
		t.Errorf("expected higher reading grade for complex text: simple=%v complex=%v",
			simple[MReadingGrade], complex[MReadingGrade])
	}
}

func TestMeanStddev(t *testing.T) {
	mean, std := meanStddev([]float64{2, 4, 4, 4, 5, 5, 7, 9})
	if mean != 5 {
		t.Errorf("mean = %v, want 5", mean)
	}
	if math.Abs(std-2) > 1e-9 {
		t.Errorf("stddev = %v, want 2", std)
	}
}

func TestEmptyText(t *testing.T) {
	if len(Metrics("")) != 0 {
		t.Error("empty text should yield no metrics")
	}
}
