// Package discriminate owns the calibrated acceptance gate: the protocol and
// threshold for answering "did this come from the target corpus, or not?" and
// the on-target score the massage loop revises against. Calibration holds out
// part of the target corpus and scores it against off-style (generic default-LLM)
// decoys, so "indistinguishable" means indistinguishable from held-out target
// text, not circular self-agreement (DESIGN.md sections 2, 5, 8).
//
// First cut (implemented): a calibrated threshold over the deterministic distance
// that lint already computes, with no model in the loop. Calibrate holds out part
// of the target corpus, scores it and the decoys, and derives a threshold plus
// separation metrics (AUC, TPR, FPR). See calibrate.go.
//
// Upgrade path: a calibrated LLM judge renders the on-target score in a fresh,
// isolated context (the caller's LLM in the agentic path; the headless model/
// adapter otherwise), reusing the same held-out-target-vs-decoys protocol.
package discriminate
