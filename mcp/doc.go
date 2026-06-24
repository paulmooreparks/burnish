// Package mcp is the primary agentic surface: an MCP server over the bluepencil
// engine. Planned tools:
//
//	distill        corpus -> style profile
//	score          draft + profile -> deterministic distance + violations
//	style_review   draft + profile -> gap report + corpus-validated rules +
//	               retrieved exemplars + calibrated scoring rubric
//
// The server owns no model. style_review returns the payload; the calling agent
// renders the judgement and revision, in a fresh isolated context for the
// discriminator step (DESIGN.md sections 6, 7). A tool result is enforcement
// outside generation: deterministic checks cannot forget, and the result
// re-injects violations as a hard structured signal at check time rather than a
// soft prior. MCP is pull; the Stop hook (cmd/bluepencil) is the complementary
// push guarantee.
//
// Current state: distill and score are fully wired (deterministic, no model).
// style_review returns the deterministic gap report plus the profile's lexicon
// and rules as a revision payload; the judgement itself (rule judge and
// calibrated discriminator) is deferred to judge/ and discriminate/, so the
// payload marks judgement as not-yet-available and instructs the caller to judge
// in a fresh, isolated context.
package mcp
