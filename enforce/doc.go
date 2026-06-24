// Package enforce is the online massage loop that ties the engine together:
// lint -> judge -> retrieve -> discriminate -> revise, bounded at N = 2-3
// iterations, then hard-fail or warn by severity. Deterministic checks run
// first and free; the LLM only sees what survives; the discriminator is the
// final acceptance gate (DESIGN.md sections 5, 6, 8).
//
// Not yet implemented. Depends on judge, retrieve, and discriminate, which are
// later increments. This package is the placeholder home for the loop.
package enforce
