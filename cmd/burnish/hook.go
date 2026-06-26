package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/paulmooreparks/burnish/lint"
	"github.com/paulmooreparks/burnish/stylespec"
)

// The Claude Code Stop hook: the push-enforcement guarantee. Claude Code execs
// `burnish hook` when the assistant finishes a turn, passing the stop payload on
// stdin. The hook checks the last assistant turn for HARD style violations and,
// if any, blocks the stop so Claude must revise before the turn completes. This
// is the deterministic guarantee that memory/instruction rules never had: the
// em-dash rule (and any avoided-lexicon invariant) cannot be forgotten.
//
// It checks HARD violations only (a chat turn is a different register than a
// distilled profile, so scoring full style distance would be register-mismatched
// mush). burnish imposes NO universal invariants: the hook enforces only what YOU
// configure. Pass --avoid "—,--" to block specific terms, and/or --profile /
// $BURNISH_PROFILE for a profile's hard rules. With neither configured, the hook
// enforces nothing.

type stopPayload struct {
	TranscriptPath string `json:"transcript_path"`
	StopHookActive bool   `json:"stop_hook_active"`
}

// hookOutput is the Stop-hook decision. An empty/omitted Decision allows the
// stop; "block" prevents it and feeds Reason back to Claude to drive a revise.
type hookOutput struct {
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func cmdHook(args []string) error {
	// Fail open throughout: a hook error must never break the session.
	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	profile := fs.String("profile", os.Getenv("BURNISH_PROFILE"), "profile YAML whose hard rules to enforce")
	avoid := fs.String("avoid", os.Getenv("BURNISH_AVOID"), "comma-separated terms to block, e.g. \"—,--\"")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "burnish hook: %v (allowing)\n", err)
		return nil
	}

	payload, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "burnish hook: read stdin: %v (allowing)\n", err)
		return nil
	}
	out, err := runHook(payload, *profile, *avoid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "burnish hook: %v (allowing)\n", err)
		return nil
	}
	if out != nil {
		_ = json.NewEncoder(os.Stdout).Encode(out)
	}
	return nil
}

// runHook is the IO glue: parse the payload, read the transcript, load the
// configured profile/avoid set, and decide.
func runHook(payload []byte, profilePath, avoidCSV string) (*hookOutput, error) {
	prof, err := loadHookProfile(profilePath, avoidCSV)
	if err != nil {
		return nil, err
	}
	if prof == nil {
		return nil, nil // nothing configured to enforce
	}
	var pl stopPayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &pl); err != nil {
			return nil, fmt.Errorf("parse stop payload: %w", err)
		}
	}
	// Already inside a stop-hook continuation: allow, to avoid an infinite
	// block loop if the violation cannot be fixed.
	if pl.StopHookActive || pl.TranscriptPath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(pl.TranscriptPath)
	if err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	text := lastAssistantText(data)
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	return decideHook(text, prof)
}

// decideHook is the pure decision: block on hard violations, else allow.
func decideHook(text string, prof *stylespec.Profile) (*hookOutput, error) {
	res, err := lint.Check(text, prof)
	if err != nil {
		return nil, err
	}
	if res.HardViolations == 0 {
		return nil, nil
	}
	return &hookOutput{Decision: "block", Reason: hardViolationReason(res)}, nil
}

// loadHookProfile builds the profile the hook enforces from configuration: an
// ad-hoc profile from --avoid terms, a --profile file, or both merged. Returns
// nil when nothing is configured (the hook then enforces nothing). burnish
// imposes no universal invariants.
func loadHookProfile(path, avoidCSV string) (*stylespec.Profile, error) {
	var avoided []string
	for _, p := range strings.Split(avoidCSV, ",") {
		if t := strings.TrimSpace(p); t != "" {
			avoided = append(avoided, t)
		}
	}
	if path == "" {
		if len(avoided) == 0 {
			return nil, nil
		}
		return &stylespec.Profile{Language: "en", Lexicon: stylespec.Lexicon{Avoided: avoided}}, nil
	}
	prof, err := stylespec.Load(path)
	if err != nil {
		return nil, err
	}
	prof.Lexicon.Avoided = append(prof.Lexicon.Avoided, avoided...)
	return prof, nil
}

func hardViolationReason(res lint.Result) string {
	var b strings.Builder
	b.WriteString("burnish: the response breaks the target style's hard invariants and must be revised. ")
	if len(res.Lexical) > 0 {
		var terms []string
		for _, l := range res.Lexical {
			terms = append(terms, fmt.Sprintf("%q at byte %d", l.Term, l.Start))
		}
		b.WriteString("Avoided terms present: " + strings.Join(terms, ", ") +
			". Remove them (no em-dashes or the '--' stand-in; use commas, colons, or parentheses). ")
	}
	b.WriteString("Rewrite the response so it satisfies the style's hard invariants.")
	return b.String()
}

// lastAssistantText extracts the text of the last assistant message from a
// Claude Code transcript (JSONL), tolerating both string and block-array content.
func lastAssistantText(jsonl []byte) string {
	var last string
	for _, ln := range strings.Split(string(jsonl), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var e struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			Message     struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(ln), &e) != nil {
			continue
		}
		// Skip Task-subagent (sidechain) turns: only the top-level assistant turn
		// is the one the user sees and the main agent can revise. Blocking on
		// subagent text would lint the wrong message and could thrash the session.
		if e.IsSidechain {
			continue
		}
		if e.Type != "assistant" && e.Message.Role != "assistant" {
			continue
		}
		if t := extractContentText(e.Message.Content); t != "" {
			last = t
		}
	}
	return last
}

func extractContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Block-array content: [{ "type":"text", "text":"..." }, ...]
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, bl := range blocks {
			if bl.Type == "text" {
				b.WriteString(bl.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	// Plain-string content.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}
