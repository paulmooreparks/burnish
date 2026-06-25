package main

import (
	"strings"
	"testing"

	"github.com/paulmooreparks/burnish/stylespec"
)

func baseProfile(t *testing.T) *stylespec.Profile {
	t.Helper()
	p, err := stylespec.Resolve(&stylespec.Profile{Inherits: stylespec.BaseProfileID}, "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLastAssistantText(t *testing.T) {
	jsonl := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"first answer"}]}}`,
		`{"type":"user","message":{"role":"user","content":"more"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"second "},{"type":"tool_use","id":"x"},{"type":"text","text":"answer"}]}}`,
	}, "\n")
	if got := lastAssistantText([]byte(jsonl)); got != "second answer" {
		t.Errorf("lastAssistantText = %q, want \"second answer\"", got)
	}
}

func TestLastAssistantTextStringContent(t *testing.T) {
	jsonl := `{"type":"assistant","message":{"role":"assistant","content":"plain string content"}}`
	if got := lastAssistantText([]byte(jsonl)); got != "plain string content" {
		t.Errorf("string content = %q", got)
	}
}

func TestLastAssistantTextIgnoresGarbageLines(t *testing.T) {
	jsonl := "not json\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"good"}]}}` + "\n" +
		"\n"
	if got := lastAssistantText([]byte(jsonl)); got != "good" {
		t.Errorf("got %q, want \"good\"", got)
	}
}

func TestLastAssistantTextSkipsSidechain(t *testing.T) {
	// A subagent (sidechain) turn must not be selected: only the top-level
	// assistant turn is the one the user sees and the main agent can revise.
	jsonl := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"top-level answer"}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent text with a dash"}]}}`,
	}, "\n")
	if got := lastAssistantText([]byte(jsonl)); got != "top-level answer" {
		t.Errorf("got %q, want top-level answer (sidechain must be skipped)", got)
	}
}

func TestLastAssistantTextIgnoresTrailingToolUse(t *testing.T) {
	// The final assistant turn may be tool_use-only (no text); keep the last
	// textful top-level turn.
	jsonl := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"the real answer"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"x","name":"Bash"}]}}`,
	}, "\n")
	if got := lastAssistantText([]byte(jsonl)); got != "the real answer" {
		t.Errorf("got %q, want the real answer", got)
	}
}

func TestDecideHookBlocksEmDash(t *testing.T) {
	p := baseProfile(t)
	out, err := decideHook("here is a sentence — with an em dash in it", p)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.Decision != "block" {
		t.Fatalf("expected block, got %+v", out)
	}
	if !strings.Contains(out.Reason, "em-dash") {
		t.Errorf("reason should mention em-dash: %q", out.Reason)
	}
}

func TestDecideHookBlocksDoubleHyphen(t *testing.T) {
	p := baseProfile(t)
	out, err := decideHook("a stand-in -- like this", p)
	if err != nil || out == nil || out.Decision != "block" {
		t.Errorf("expected block for '--', got %+v (err %v)", out, err)
	}
}

func TestDecideHookAllowsClean(t *testing.T) {
	p := baseProfile(t)
	out, err := decideHook("a clean sentence, with a comma and no dashes", p)
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("clean text should allow (nil), got %+v", out)
	}
}
