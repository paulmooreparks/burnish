// Package discriminate owns the calibrated acceptance gate: the protocol and
// threshold for answering "did this come from the target corpus, or not?" and
// the on-target score the massage loop revises against. Calibration holds out
// part of the target corpus and scores it against off-style (generic default-LLM)
// decoys, so "indistinguishable" means indistinguishable from held-out target
// text, not circular self-agreement (DESIGN.md sections 2, 5, 8).
//
// It owns calibration and the scoring rubric, not the inference. In the agentic
// path the caller's LLM renders the judgement in a fresh, isolated context (never
// grading its own draft, DESIGN.md section 7 constraint 1); the headless model/
// adapter runs it only for agent-less callers.
//
// Not yet implemented. Starts as a calibrated LLM judge; upgrade path is a
// trained on-corpus classifier once the corpus is large enough.
package discriminate
