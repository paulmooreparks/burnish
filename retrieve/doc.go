// Package retrieve embeds corpus exemplars and, at massage time, retrieves the
// most stylistically relevant ones as targeted few-shot for the revision step.
// Exemplars capture the rhythm and voice that no explicit rule articulates
// (DESIGN.md sections 3, 6).
//
// First cut (implemented): a TF-IDF cosine bank over corpus chunks, with no
// embedding model or vector-store dependency. Build indexes the corpus by
// paragraph; Retrieve returns the chunks most topically relevant to a draft (term
// overlap), each an authentic target-style passage. See retrieve.go.
//
// Upgrade path: dense semantic embeddings (an embedding model + a real vector
// store) match by meaning rather than shared terms, reusing the same chunk +
// Retrieve shape. That is the deferred "embedding model + vector store" decision.
package retrieve
