package discriminate

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/paulmooreparks/burnish/distill"
	"github.com/paulmooreparks/burnish/lint"
	"github.com/paulmooreparks/burnish/stylespec"
)

// splitSeed makes the train/holdout shuffle deterministic and reproducible while
// removing any coupling to corpus file order.
const splitSeed = 1

// This is the first-cut discriminator: a calibrated threshold over the
// deterministic distance that lint already computes, rather than an LLM judge.
// It needs no model, keeps the runtime hot path model-free, and empirically tests
// whether the distance separates authentic target writing from off-style decoys.
// The calibrated LLM judge (DESIGN.md decision #4) is the documented upgrade; it
// would replace or augment the distance statistic with a richer score, reusing
// the same calibration protocol (held-out target vs decoys -> threshold).

// Scored is one document's distance and true label.
type Scored struct {
	Name     string  `json:"name"`
	Distance float64 `json:"distance"`
	Target   bool    `json:"target"` // true = held-out target, false = decoy
}

// Calibration is the result of a calibration run.
type Calibration struct {
	Threshold      float64  `json:"threshold"` // on-target if distance <= threshold
	AUC            float64  `json:"auc"`       // separation of target vs decoy (0.5 = chance, 1 = perfect)
	TPR            float64  `json:"tpr"`       // true-positive rate at threshold
	FPR            float64  `json:"fpr"`       // false-positive rate at threshold
	Accuracy       float64  `json:"accuracy"`  // at threshold
	NTargetHoldout int      `json:"n_target_holdout"`
	NDecoy         int      `json:"n_decoy"`
	NTrain         int      `json:"n_train"`
	Scores         []Scored `json:"scores"` // every held-out target + decoy, with distance
	// Separates is false when the distance does not meaningfully separate target
	// from decoys (AUC below floor or TPR not above FPR); the threshold is then not
	// trustworthy. Warning carries the human-readable reason.
	Separates bool   `json:"separates"`
	Warning   string `json:"warning,omitempty"`
}

// minUsefulAUC is the AUC below which the discriminator is treated as
// non-separating and the threshold is flagged untrustworthy.
const minUsefulAUC = 0.6

// Options tunes calibration.
type Options struct {
	// HoldoutEvery holds out every Nth target document (by index) as the positive
	// evaluation set; the rest train the calibration profile. Default 4.
	HoldoutEvery int
	Distill      distill.Options
}

// DefaultOptions returns sensible calibration defaults.
func DefaultOptions() Options {
	return Options{HoldoutEvery: 4, Distill: distill.DefaultOptions()}
}

// Calibrate splits the target corpus into train/holdout, builds a profile from
// the train split, scores the held-out target docs (positives) and the decoys
// (negatives) against it, and derives an acceptance threshold and separation
// metrics. The shipped profile IS the train profile: it carries the discriminator
// and is on the same distance scale the threshold was measured on, so the reported
// TPR/FPR describe the artifact you actually use. (Building the shipped profile
// from all docs instead would widen its ranges and lower distances, making the
// gate quietly more lenient than the reported FPR.) Holding the positives out of
// the train profile keeps "indistinguishable" honest: the threshold is measured
// against target text the profile was never shown (DESIGN.md section 5).
func Calibrate(id, register, language string, target, decoys []distill.DocInput, opts Options) (*stylespec.Profile, *Calibration, error) {
	if opts.HoldoutEvery < 2 {
		opts.HoldoutEvery = DefaultOptions().HoldoutEvery
	}
	if !distill.LanguageImplemented(language) {
		return nil, nil, distill.ErrUnsupportedLanguage(language)
	}
	if len(decoys) == 0 {
		return nil, nil, fmt.Errorf("calibration needs at least one decoy document")
	}

	// Shuffle (fixed seed) before the modulo split so the holdout is not coupled to
	// corpus file order, while staying deterministic and reproducible.
	shuffled := append([]distill.DocInput(nil), target...)
	rng := rand.New(rand.NewSource(splitSeed))
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	var train, holdout []distill.DocInput
	for i, d := range shuffled {
		if i%opts.HoldoutEvery == 0 {
			holdout = append(holdout, d)
		} else {
			train = append(train, d)
		}
	}
	if len(train) < 2 || len(holdout) < 1 {
		return nil, nil, fmt.Errorf("too few target docs to split: %d train, %d holdout", len(train), len(holdout))
	}

	profile, err := distill.Distill(id, register, language, train, opts.Distill)
	if err != nil {
		return nil, nil, err
	}

	cal := &Calibration{NTrain: len(train), NTargetHoldout: len(holdout), NDecoy: len(decoys)}
	var positives, negatives []float64
	for _, d := range holdout {
		res, err := lint.Check(d.Text, profile)
		if err != nil {
			return nil, nil, err
		}
		positives = append(positives, res.Distance)
		cal.Scores = append(cal.Scores, Scored{Name: d.Name, Distance: res.Distance, Target: true})
	}
	for _, d := range decoys {
		res, err := lint.Check(d.Text, profile)
		if err != nil {
			return nil, nil, err
		}
		negatives = append(negatives, res.Distance)
		cal.Scores = append(cal.Scores, Scored{Name: d.Name, Distance: res.Distance, Target: false})
	}

	cal.AUC = auc(positives, negatives)
	cal.Threshold, cal.TPR, cal.FPR, cal.Accuracy = bestThreshold(positives, negatives)
	cal.Separates = cal.AUC >= minUsefulAUC && cal.TPR > cal.FPR
	if !cal.Separates {
		cal.Warning = fmt.Sprintf("weak separation (AUC %.2f, TPR %.0f%% vs FPR %.0f%%): the threshold is not trustworthy; the corpus may be mislabeled, the decoys too similar, or the register mixed",
			cal.AUC, cal.TPR*100, cal.FPR*100)
	}

	profile.Discriminator = &stylespec.Discriminator{
		Threshold: round(cal.Threshold, 4),
		AUC:       round(cal.AUC, 4),
		TPR:       round(cal.TPR, 4),
		FPR:       round(cal.FPR, 4),
		Method:    "distance-threshold",
	}
	return profile, cal, nil
}

// auc is the probability a random target scores as more on-style (lower distance)
// than a random decoy: the Mann-Whitney statistic, ties counted as 0.5. 0.5 is
// chance, 1.0 perfect separation.
func auc(positives, negatives []float64) float64 {
	if len(positives) == 0 || len(negatives) == 0 {
		return 0
	}
	var concordant float64
	for _, p := range positives {
		for _, n := range negatives {
			switch {
			case p < n:
				concordant++
			case p == n:
				concordant += 0.5
			}
		}
	}
	return concordant / float64(len(positives)*len(negatives))
}

// bestThreshold scans candidate cut points and returns the threshold maximizing
// Youden's J (TPR - FPR), with the TPR, FPR, and accuracy at that threshold. A
// draft is classified on-target when distance <= threshold.
func bestThreshold(positives, negatives []float64) (threshold, tpr, fpr, accuracy float64) {
	cands := append(append([]float64{}, positives...), negatives...)
	sort.Float64s(cands)
	// Each distinct observed distance is a candidate cut (distance <= t accepts
	// everything at or below it). The Youden-J optimum always lands on one of
	// these, so this is the minimal sufficient candidate set.
	var thresholds []float64
	for i, v := range cands {
		if i == 0 || v != cands[i-1] {
			thresholds = append(thresholds, v)
		}
	}
	if len(thresholds) == 0 {
		return 0, 0, 0, 0
	}

	nPos, nNeg := float64(len(positives)), float64(len(negatives))
	bestJ := -1.0
	for _, t := range thresholds {
		var tp, fp float64
		for _, p := range positives {
			if p <= t {
				tp++
			}
		}
		for _, n := range negatives {
			if n <= t {
				fp++
			}
		}
		curTPR := tp / nPos
		curFPR := fp / nNeg
		j := curTPR - curFPR
		if j > bestJ {
			bestJ = j
			threshold = t
			tpr = curTPR
			fpr = curFPR
			accuracy = (tp + (nNeg - fp)) / (nPos + nNeg)
		}
	}
	return threshold, tpr, fpr, accuracy
}

func round(f float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(f*p) / p
}
