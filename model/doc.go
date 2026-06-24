// Package model is the optional inference adapter for headless, agent-less
// callers (the .NET serve sidecar, CI), where no orchestrating LLM exists to run
// the rule-judging and discriminator inference. It is configurable, defaulting
// to Haiku 4.5 in an isolated context with structured output.
//
// This is a fallback, not the default path. In the agentic path the caller's own
// LLM does the cognition and this package is unused; bluepencil owns measurement,
// calibration, and protocol, never the inference except here (DESIGN.md sections
// 6, 7, 8 #5).
//
// Not yet implemented. It is the last surface in the build order (DESIGN.md
// section 9 step 6), after the agentic path proves out.
package model
