package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/paulmooreparks/burnish/stylespec"
)

// connect wires a client to an in-process burnish server (with no profiles
// directory) and returns the client session.
func connect(t *testing.T) *sdk.ClientSession {
	t.Helper()
	return connectDir(t, "")
}

// connectDir is connect with a configured profiles directory, for the
// name-resolution and list_profiles tests.
func connectDir(t *testing.T, profilesDir string) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientT, serverT := sdk.NewInMemoryTransports()

	srv := NewServer(profilesDir)
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
	for _, want := range []string{"list_profiles", "distill", "calibrate", "retrieve", "score", "style_review"} {
		if !got[want] {
			t.Errorf("missing tool %q (have %v)", want, got)
		}
	}
}

// TestCalibrateTool exercises the MCP calibrate tool end to end: a target and a
// decoy corpus produce a saved, calibrated profile reported with its threshold.
func TestCalibrateTool(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t")
	decoys := filepath.Join(dir, "d")
	for _, d := range []string{target, decoys} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tbody := "The cat sat. The dog ran.\n\nBirds fly south. Fish swim east.\n\nWe walk home. They drive far."
	dbody := "This sentence wanders on at considerable length, accumulating clause upon clause, never quite arriving at its point."
	for _, n := range []string{"a.md", "b.md", "c.md", "e.md"} {
		write(t, filepath.Join(target, n), tbody)
		write(t, filepath.Join(decoys, n), dbody)
	}

	out := filepath.Join(dir, "cal.profile.yaml")
	cs := connect(t)
	res, text := callText(t, cs, "calibrate", map[string]any{
		"target_dir":    target,
		"decoys_dir":    decoys,
		"register":      "cal",
		"holdout_every": 2,
		"out":           out,
	})
	if res.IsError {
		t.Fatalf("calibrate errored: %s", text)
	}
	if !strings.Contains(strings.ToLower(text), "threshold") {
		t.Errorf("calibrate should report a threshold: %s", text)
	}
	// The saved profile must be calibrated (carry a discriminator).
	prof, err := stylespec.Load(out)
	if err != nil {
		t.Fatalf("load saved profile: %v", err)
	}
	if prof.Discriminator == nil {
		t.Error("saved profile has no discriminator")
	}

	// Missing decoys_dir is a clear error.
	mres, mtext := callText(t, cs, "calibrate", map[string]any{"target_dir": target, "register": "cal"})
	if !mres.IsError {
		t.Fatalf("missing decoys_dir should error, got: %s", mtext)
	}
}

// TestRetrieveTool exercises the MCP retrieve tool end to end: a corpus dir and a
// query return ranked exemplars.
func TestRetrieveTool(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, "corpus")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(corpus, "a.md"), "The cat sat on the mat. The dog ran in the park. Birds fly south for winter.")
	write(t, filepath.Join(corpus, "b.md"), "We walk home each evening. They drive far on weekends. Fish swim east at dawn.")

	cs := connect(t)
	res, text := callText(t, cs, "retrieve", map[string]any{
		"corpus_dir": corpus,
		"query":      "the dog ran in the park",
		"k":          2,
		"min_words":  1,
	})
	if res.IsError {
		t.Fatalf("retrieve errored: %s", text)
	}
	if !strings.Contains(strings.ToLower(text), "exemplar") {
		t.Errorf("retrieve should return exemplars: %s", text)
	}

	// Empty query is a clear error.
	eres, etext := callText(t, cs, "retrieve", map[string]any{"corpus_dir": corpus, "query": "  "})
	if !eres.IsError {
		t.Fatalf("empty query should error, got: %s", etext)
	}
}

// TestProfileByName exercises the registry path: list_profiles enumerates a
// configured directory, and score/style_review resolve a profile by register
// name rather than a filesystem path.
func TestProfileByName(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, "corpus")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(corpus, "a.md"), "The engine measures style. It does not guess. "+
		"A profile records the language it was built for. Distance is a number, not a vibe.")
	write(t, filepath.Join(corpus, "b.md"), "Build the engine once. Expose it through a hook, a library, and a sidecar. "+
		"Deterministic checks run first and free. The judge sees only what is left.")

	// A profiles dir whose file the agent should NOT need to know about.
	profilesDir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// distill writes the profile into profilesDir under the stem convention.
	cs := connectDir(t, profilesDir)
	dres, dtext := callText(t, cs, "distill", map[string]any{
		"corpus_dir": corpus,
		"register":   "house",
		"id":         "house",
		"out":        filepath.Join(profilesDir, "house.profile.yaml"),
	})
	if dres.IsError {
		t.Fatalf("distill errored: %s", dtext)
	}

	// list_profiles should surface the profile by id/register, no path needed.
	lres, ltext := callText(t, cs, "list_profiles", map[string]any{})
	if lres.IsError {
		t.Fatalf("list_profiles errored: %s", ltext)
	}
	if !strings.Contains(ltext, "house") {
		t.Errorf("list_profiles should name the 'house' profile: %s", ltext)
	}

	// score by register NAME, not by path.
	sres, stext := callText(t, cs, "score", map[string]any{
		"profile": "house",
		"text":    "This is a short draft to score by name.",
	})
	if sres.IsError {
		t.Fatalf("score by name errored: %s", stext)
	}
	if !strings.Contains(stext, "register house") {
		t.Errorf("score should resolve the named profile: %s", stext)
	}

	// style_review by name should also work and instruct fresh-context judging.
	rres, rtext := callText(t, cs, "style_review", map[string]any{
		"profile": "house",
		"text":    "Another draft to review by name.",
	})
	if rres.IsError {
		t.Fatalf("style_review by name errored: %s", rtext)
	}
	if !strings.Contains(strings.ToLower(rtext), "fresh") {
		t.Errorf("style_review should instruct fresh-context judgement: %s", rtext)
	}

	// An unknown name is a clear error, not a silent miss.
	ures, utext := callText(t, cs, "score", map[string]any{
		"profile": "nonesuch",
		"text":    "x",
	})
	if !ures.IsError {
		t.Fatalf("expected error for unknown profile name, got: %s", utext)
	}

	// When both are given, profile_path wins: a real path with a bogus name must
	// score against the path, not error on the name.
	bres, btext := callText(t, cs, "score", map[string]any{
		"profile":      "nonesuch",
		"profile_path": filepath.Join(profilesDir, "house.profile.yaml"),
		"text":         "draft scored via the explicit path.",
	})
	if bres.IsError {
		t.Fatalf("profile_path should win over profile: %s", btext)
	}
	if !strings.Contains(btext, "register house") {
		t.Errorf("expected the path-resolved profile: %s", btext)
	}
}

func TestServerShipsUseProtocol(t *testing.T) {
	cs := connect(t)
	got := cs.InitializeResult().Instructions
	if got == "" {
		t.Fatal("server shipped no instructions; agents have no source for the use protocol")
	}
	// The protocol must carry the loop, the fresh-context discipline, and the
	// one-register-per-profile rule, the three things a tool description cannot.
	for _, want := range []string{"style_review", "FRESH", "register"} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions missing %q:\n%s", want, got)
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

	// style_review should surface the lexicon and the fresh-context instruction.
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
