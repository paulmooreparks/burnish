// Package api is the stable, importable entry point for Go applications (Tela,
// planning.fit) that want to wrap their own LLM calls with burnish style
// enforcement. The intended surface is:
//
//	Check(ctx context.Context, text string, profile *stylespec.Profile) (Result, error)
//
// running the full engine (lint -> judge -> retrieve -> discriminate) behind one
// call. The same binary also runs as an HTTP/subprocess `serve` sidecar for
// .NET surfaces (DESIGN.md sections 6, 7).
//
// Not yet implemented: it composes lint (shipped) with judge, retrieve, and
// discriminate (later increments). For now, callers can use the lint package
// directly for the deterministic assessment.
package api
