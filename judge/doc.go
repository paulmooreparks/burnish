// Package judge builds the rule-judging payload: it takes a draft plus a
// profile's corpus-validated Rules and produces the structured request a judge
// answers, each rule requiring the offending evidence span to be quoted
// (require_evidence). It does not call a model. In the agentic path the caller's
// LLM renders the judgement in a fresh, isolated context; the headless model/
// adapter runs it only for agent-less callers. The same context dilution that
// makes a generator forget its style rules cannot reach an isolated judge
// (DESIGN.md sections 2, 6, 7, 8 #5).
//
// Not yet implemented: this increment ships the deterministic lint layer and
// the score CLI (DESIGN.md section 9). The MCP server and rule mining come next.
package judge
