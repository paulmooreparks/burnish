package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect wires a client to an in-process burnish server and returns the
// client session.
func connect(t *testing.T) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientT, serverT := sdk.NewInMemoryTransports()

	srv := NewServer()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { ss.Close() })

	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callText(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (*sdk.CallToolResult, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text += tc.Text
		}
	}
	return res, text
}

func TestListTools(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"distill", "score", "style_review"} {
		if !got[want] {
			t.Errorf("missing tool %q (have %v)", want, got)
		}
	}
}

func TestDistillScoreReviewRoundTrip(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, "corpus")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	// A small two-document corpus of short, declarative, list-leaning prose.
	write(t, filepath.Join(corpus, "a.md"), "The engine measures style. It does not guess. "+
		"A profile records the language it was built for. Distance is a number, not a vibe.")
	write(t, filepath.Join(corpus, "b.md"), "Build the engine once. Expose it through a hook, a library, and a sidecar. "+
		"Deterministic checks run first and free. The judge sees only what is left.")
	profile := filepath.Join(dir, "p.profile.yaml")

	cs := connect(t)

	// distill
	dres, dtext := callText(t, cs, "distill", map[string]any{
		"corpus_dir": corpus,
		"register":   "test-register",
		"out":        profile,
	})
	if dres.IsError {
		t.Fatalf("distill errored: %s", dtext)
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatalf("profile not written: %v", err)
	}
	if !strings.Contains(dtext, "distilled 2 documents") {
		t.Errorf("unexpected distill summary: %s", dtext)
	}

	// score a draft full of em-dashes and hedging (off-style); expect hard violation.
	sres, stext := callText(t, cs, "score", map[string]any{
		"profile_path": profile,
		"text":         "Perhaps, generally speaking, this might possibly be a rather long and meandering sentence — one that goes on and on.",
	})
	if sres.IsError {
		t.Fatalf("score errored: %s", stext)
	}
	if !strings.Contains(stext, "HARD violations") {
		t.Errorf("expected a hard violation (em-dash) in score output: %s", stext)
	}

	// style_review should surface the lexicon and the deferred-judgement marker.
	rres, rtext := callText(t, cs, "style_review", map[string]any{
		"profile_path": profile,
		"text":         "This is a draft to review.",
	})
	if rres.IsError {
		t.Fatalf("style_review errored: %s", rtext)
	}
	if !strings.Contains(strings.ToLower(rtext), "fresh") {
		t.Errorf("style_review should instruct fresh-context judgement: %s", rtext)
	}
}

func TestScoreRejectsUnsupportedLanguage(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "zh.profile.yaml")
	write(t, profile, "id: x\nregister: r\nlanguage: zh\nfeatures: []\nlexicon: {}\n")

	cs := connect(t)
	res, text := callText(t, cs, "score", map[string]any{
		"profile_path": profile,
		"text":         "anything",
	})
	if !res.IsError {
		t.Fatalf("expected error for zh profile, got: %s", text)
	}
	if !strings.Contains(text, "zh") {
		t.Errorf("error should name the language: %s", text)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
