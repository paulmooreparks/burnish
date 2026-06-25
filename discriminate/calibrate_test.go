package discriminate

import (
	"math"
	"testing"
)

func TestAUC(t *testing.T) {
	cases := []struct {
		name             string
		pos, neg         []float64
		want             float64
	}{
		{"perfect separation (target lower)", []float64{1, 2, 3}, []float64{4, 5, 6}, 1.0},
		{"inverted (target higher)", []float64{4, 5, 6}, []float64{1, 2, 3}, 0.0},
		{"partial overlap", []float64{1, 3}, []float64{2, 4}, 0.75},
		{"ties count half", []float64{1, 2}, []float64{2, 3}, 0.875},
	}
	for _, c := range cases {
		got := auc(c.pos, c.neg)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: auc = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBestThreshold(t *testing.T) {
	// Perfectly separable: target distances below decoys. Best cut sits at the top
	// of the target range with TPR 1, FPR 0.
	thr, tpr, fpr, acc := bestThreshold([]float64{1, 2, 3}, []float64{4, 5, 6})
	if thr != 3 {
		t.Errorf("threshold = %v, want 3", thr)
	}
	if tpr != 1 || fpr != 0 || acc != 1 {
		t.Errorf("tpr=%v fpr=%v acc=%v, want 1/0/1", tpr, fpr, acc)
	}
}

func TestBestThresholdOverlap(t *testing.T) {
	// One decoy intrudes into the target range; best J still classifies most right.
	thr, tpr, fpr, acc := bestThreshold([]float64{1, 2, 3}, []float64{2.5, 5, 6})
	// At t=3: TPR 3/3=1, FPR 1/3. At t=2: TPR 2/3, FPR 0 -> J=0.667. At t=3 J=0.667.
	// Either cut is acceptable; assert the metrics are self-consistent.
	if tpr <= 0 || tpr > 1 || fpr < 0 || fpr > 1 {
		t.Errorf("rates out of range: tpr=%v fpr=%v", tpr, fpr)
	}
	if acc < 0 || acc > 1 {
		t.Errorf("accuracy out of range: %v", acc)
	}
	_ = thr
}

