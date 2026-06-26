// Package mcp is the primary agentic surface: an MCP server over the burnish
// engine. Tools:
//
//	list_profiles  enumerate the profiles discoverable by id / register name
//	distill        corpus -> style profile
//	calibrate      target + decoy corpora -> profile + discriminator threshold
//	retrieve       corpus + query -> target-style exemplar passages
//	score          draft + profile -> deterministic distance + violations
//	style_review   draft + profile -> gap report + lexicon + corpus-validated
//	               rules + discriminator verdict + judged-rule prompt
//
// score and style_review take the profile by id / register name (resolved against
// the server's --profiles directory) or by an explicit profile_path.
//
// The server-level instructions (sent in the initialize result) carry the use
// protocol: the draft -> review -> revise loop, its bound, which profile to pass,
// and the fresh-context discipline. The per-tool descriptions only describe each
// tool in isolation, so a connected agent reads the protocol from the server
// instructions, not from any one tool.
//
// The server owns no model. style_review returns the payload; the calling agent
// renders the subjective judged-rule verdict and the revision, in a fresh
// isolated context (DESIGN.md sections 6, 7). A tool result is enforcement
// outside generation: deterministic checks cannot forget, and the result
// re-injects violations as a hard structured signal at check time rather than a
// soft prior. MCP is pull; the Stop hook (cmd/burnish) is the complementary
// push guarantee.
//
// Current state: distill and score are fully wired (deterministic, no model).
// style_review returns the deterministic gap report, the profile's lexicon and
// rules, the distance-to-style number, and the calibrated on-target/off-target
// discriminator verdict when the profile is calibrated; for the subjective judged
// rules it returns a judging_prompt for the caller to render in a fresh, isolated
// context. Only that subjective step needs a model.
package mcp
