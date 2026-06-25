// Package judge builds the rule-judging payload: it takes a draft plus a
// profile's corpus-validated Rules and produces the structured request a judge
// answers, each rule requiring the offending evidence span to be quoted
// (require_evidence). It does not call a model. In the agentic path the caller's
// LLM renders the judgement in a fresh, isolated context; the headless model/
// adapter runs it only for agent-less callers. The same context dilution that
// makes a generator forget its style rules cannot reach an isolated judge
// (DESIGN.md sections 2, 6, 7, 8 #5).
//
// First cut (implemented): deterministic, corpus-validated structural rules with
// no model. Mine derives each rule's threshold from the corpus at a support level
// and attaches it to the profile; CheckRules flags per-instance violations a
// draft commits (e.g. a single run-on sentence the aggregate distance hides). See
// rules.go.
//
// Upgrade path: LLM-induced *subjective* rules. judge/ will build the induction
// and per-rule judging payloads (require_evidence) and the caller's LLM renders
// them in a fresh, isolated context (decision #5), reusing the same support
// validation.
package judge
