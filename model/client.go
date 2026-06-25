// Package model is the OPTIONAL headless inference adapter: a thin Anthropic
// Messages API client that supplies the cognition burnish needs when there is no
// orchestrating agent to render it (the .NET sidecar, CI). It is the fallback
// path of decision #5, never the default: the agentic path uses the caller's own
// LLM via the MCP style_review tool, and the engine itself bakes no model.
//
// The client is standard-library only (net/http + encoding/json), so it adds no
// module dependency and pins to nothing but the stable Messages HTTP contract.
// The HTTP transport is an injectable Doer, so every test mocks it and no test
// touches the network.
package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/paulmooreparks/burnish/enforce"
	"github.com/paulmooreparks/burnish/judge"
	"github.com/paulmooreparks/burnish/stylespec"
)

// Defaults for a freshly constructed Client.
const (
	DefaultModel     = "claude-haiku-4-5-20251001"
	DefaultBaseURL   = "https://api.anthropic.com"
	DefaultMaxTokens = 4096
	apiVersion       = "2023-06-01"
)

// Doer is the minimal HTTP surface the client needs. *http.Client satisfies it;
// tests inject a mock so no request leaves the process.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client is a minimal Anthropic Messages API client. Construct it with NewClient
// and override fields as needed (Model, BaseURL, HTTP, MaxTokens) before use.
type Client struct {
	APIKey    string
	Model     string
	BaseURL   string
	HTTP      Doer
	MaxTokens int
}

// NewClient returns a Client with sensible defaults (Haiku, the public endpoint,
// the default HTTP client). apiKey is typically read from ANTHROPIC_API_KEY by
// the caller; an empty key is allowed at construction time and only fails on use.
func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:    apiKey,
		Model:     DefaultModel,
		BaseURL:   DefaultBaseURL,
		HTTP:      http.DefaultClient,
		MaxTokens: DefaultMaxTokens,
	}
}

// Verdict is one judged-rule judgement: whether the rule holds on the draft, and
// (when it does not) the quoted offending span. It mirrors the JSON shape the
// JudgingPrompt asks the model to return.
type Verdict struct {
	ID       string `json:"id"`
	Holds    bool   `json:"holds"`
	Evidence string `json:"evidence,omitempty"`
}

// Reviser returns an enforce.Reviser bound to this client. It is the headless
// substitute for the calling agent's LLM: enforce.Massage hands it the brief and
// the current draft each iteration, and it returns the revised text. Plugs
// directly into enforce.Massage / api.Massage.
func (c *Client) Reviser() enforce.Reviser {
	return func(ctx context.Context, draft string, brief enforce.Brief) (string, error) {
		out, err := c.complete(ctx, revisionSystem, renderRevisionUser(draft, brief))
		if err != nil {
			return "", fmt.Errorf("revise: %w", err)
		}
		return strings.TrimSpace(out), nil
	}
}

// JudgeRules judges a draft against the profile's judged (subjective) rules in a
// fresh, isolated call. It builds the prompt with judge.JudgingPrompt (which
// filters to class="judged" and requires quoted evidence) and parses the model's
// JSON verdict array. If the profile carries no judged rules it returns nil.
//
// This is a building block for headless callers. enforce.Massage takes only a
// Reviser, so the serve massage loop currently enforces the deterministic rules
// and the calibrated discriminator but does NOT yet feed judged-rule verdicts
// back into revision; a headless caller can call JudgeRules directly. Wiring a
// judge hook into the massage loop is a follow-up (see CARRYOVER).
func (c *Client) JudgeRules(ctx context.Context, draft string, rules []stylespec.Rule) ([]Verdict, error) {
	if len(judge.JudgedRules(rules)) == 0 {
		return nil, nil
	}
	out, err := c.complete(ctx, judgeSystem, judge.JudgingPrompt(draft, rules))
	if err != nil {
		return nil, fmt.Errorf("judge: %w", err)
	}
	verdicts, err := parseVerdicts(out)
	if err != nil {
		return nil, fmt.Errorf("judge: %w", err)
	}
	return verdicts, nil
}

const revisionSystem = "You are a careful copy editor. You rewrite a draft to match a target writing style " +
	"WITHOUT changing its meaning. You return ONLY the revised text: no preamble, no explanation, no fences."

const judgeSystem = "You are an impartial style judge. You judge a draft against an author's style rules in a " +
	"fresh, isolated context. You return ONLY the requested JSON, with a quoted evidence span for every violation."

// renderRevisionUser turns the deterministic brief into the user message: the
// engine's guidance, the structured signals, and the draft to rewrite.
func renderRevisionUser(draft string, brief enforce.Brief) string {
	var b strings.Builder
	b.WriteString(brief.Guidance)
	b.WriteString("\n\n")
	if len(brief.AvoidedLexicon) > 0 {
		fmt.Fprintf(&b, "Avoid these terms entirely: %s\n", strings.Join(brief.AvoidedLexicon, ", "))
	}
	if len(brief.PreferredLexicon) > 0 {
		fmt.Fprintf(&b, "Favor these terms where natural: %s\n", strings.Join(brief.PreferredLexicon, ", "))
	}
	for _, f := range brief.Features {
		fmt.Fprintf(&b, "- feature %s is off-target (%.2f stddev out)\n", f.ID, f.Deviation)
	}
	for _, r := range brief.Rules {
		fmt.Fprintf(&b, "- rule [%s] %s: %s\n", r.Severity, r.Statement, r.Evidence)
	}
	for i, ex := range brief.Exemplars {
		fmt.Fprintf(&b, "\nExemplar %d (target-style passage):\n%s\n", i+1, strings.TrimSpace(ex.Chunk.Text))
	}
	fmt.Fprintf(&b, "\n=== DRAFT START ===\n%s\n=== DRAFT END ===\n", strings.TrimSpace(draft))
	return b.String()
}

// parseVerdicts extracts the JSON verdict array from the model's reply, tolerating
// surrounding prose or a ```json fence by slicing to the outermost brackets.
func parseVerdicts(s string) ([]Verdict, error) {
	start := strings.IndexByte(s, '[')
	end := strings.LastIndexByte(s, ']')
	if start < 0 || end < 0 || end < start {
		return nil, fmt.Errorf("no JSON array in model reply: %q", truncate(s, 200))
	}
	var v []Verdict
	if err := json.Unmarshal([]byte(s[start:end+1]), &v); err != nil {
		return nil, fmt.Errorf("parse verdicts: %w", err)
	}
	return v, nil
}

// --- Messages API wire types --------------------------------------------------

type messagesRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []apiMessage `json:"messages"`
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// complete issues one Messages API request and returns the concatenated text
// content blocks. A non-2xx status is an error carrying the status and body.
func (c *Client) complete(ctx context.Context, system, user string) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("no API key configured")
	}
	model := c.Model
	if model == "" {
		model = DefaultModel
	}
	maxTokens := c.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	doer := c.HTTP
	if doer == nil {
		doer = http.DefaultClient
	}

	body, err := json.Marshal(messagesRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  []apiMessage{{Role: "user", Content: user}},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := doer.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("anthropic API %s: %s", resp.Status, truncate(string(raw), 500))
	}

	var mr messagesResponse
	if err := json.Unmarshal(raw, &mr); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	// A max_tokens stop is a truncated reply, not a complete one: returning it
	// silently would hand back a half-rewritten draft or a partial verdict array
	// that parses as if whole. Treat it as an error so the caller can raise
	// MaxTokens rather than accept corrupted output.
	if mr.StopReason == "max_tokens" {
		return "", fmt.Errorf("response truncated at max_tokens (%d); raise MaxTokens", maxTokens)
	}
	var out strings.Builder
	for _, blk := range mr.Content {
		if blk.Type == "text" {
			out.WriteString(blk.Text)
		}
	}
	return out.String(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
