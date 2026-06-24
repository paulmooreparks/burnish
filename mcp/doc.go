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
// Not yet implemented. This is the next surface to build (DESIGN.md section 9
// step 3): it can ship the deterministic distill/score value immediately, with
// style_review's judgement payload filled in as judge/ and discriminate/ land.
package mcp
