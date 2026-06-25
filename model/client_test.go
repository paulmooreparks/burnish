package model

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/paulmooreparks/burnish/enforce"
	"github.com/paulmooreparks/burnish/stylespec"
)

// mockDoer captures the request it received and returns a canned response, so no
// test touches the network.
type mockDoer struct {
	gotReq  *http.Request
	gotBody string
	status  int
	respaw  string
	err     error
}

func (m *mockDoer) Do(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.gotReq = req
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		m.gotBody = string(b)
	}
	status := m.status
	if status == 0 {
		status = 200
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(m.respaw)),
		Header:     make(http.Header),
	}, nil
}

func textResponse(s string) string {
	// A minimal Messages API success body with one text content block.
	return `{"content":[{"type":"text","text":` + jsonQuote(s) + `}]}`
}

func jsonQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

func TestReviserCallsAPIAndReturnsText(t *testing.T) {
	m := &mockDoer{respaw: textResponse("the revised draft")}
	c := NewClient("test-key")
	c.HTTP = m
	c.BaseURL = "https://example.test"

	revise := c.Reviser()
	got, err := revise(context.Background(), "original draft", enforce.Brief{
		Guidance:       "Rewrite toward the target.",
		AvoidedLexicon: []string{"—"},
	})
	if err != nil {
		t.Fatalf("revise: %v", err)
	}
	if got != "the revised draft" {
		t.Errorf("revised text = %q, want %q", got, "the revised draft")
	}

	// Request shape: correct URL, method, and the required Anthropic headers.
	if m.gotReq == nil {
		t.Fatal("no request captured")
	}
	if m.gotReq.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", m.gotReq.Method)
	}
	if m.gotReq.URL.String() != "https://example.test/v1/messages" {
		t.Errorf("url = %s", m.gotReq.URL.String())
	}
	if h := m.gotReq.Header.Get("x-api-key"); h != "test-key" {
		t.Errorf("x-api-key = %q", h)
	}
	if h := m.gotReq.Header.Get("anthropic-version"); h != apiVersion {
		t.Errorf("anthropic-version = %q, want %q", h, apiVersion)
	}
	if h := m.gotReq.Header.Get("content-type"); h != "application/json" {
		t.Errorf("content-type = %q, want application/json", h)
	}
	// The brief content and the draft reach the model.
	for _, want := range []string{"original draft", "Rewrite toward the target.", DefaultModel} {
		if !strings.Contains(m.gotBody, want) {
			t.Errorf("request body missing %q; body=%s", want, m.gotBody)
		}
	}
}

func TestJudgeRulesParsesVerdicts(t *testing.T) {
	m := &mockDoer{respaw: textResponse(`Here are my findings:
[{"id":"open-with-anecdote","holds":false,"evidence":"It starts with a definition."},
 {"id":"first-person","holds":true}]`)}
	c := NewClient("k")
	c.HTTP = m

	rules := []stylespec.Rule{
		{ID: "open-with-anecdote", Class: "judged", Statement: "Open with an anecdote.", Support: 0.8},
		{ID: "first-person", Class: "judged", Statement: "Write in the first person."},
		{ID: "max-sentence", Class: "deterministic", Statement: "Keep sentences short."},
	}
	verdicts, err := c.JudgeRules(context.Background(), "some draft", rules)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if len(verdicts) != 2 {
		t.Fatalf("got %d verdicts, want 2", len(verdicts))
	}
	if verdicts[0].ID != "open-with-anecdote" || verdicts[0].Holds {
		t.Errorf("verdict[0] = %+v", verdicts[0])
	}
	if verdicts[0].Evidence == "" {
		t.Errorf("expected evidence on the failing verdict")
	}
	// The judging prompt must include the judged rules but not the deterministic one.
	if !strings.Contains(m.gotBody, "open-with-anecdote") || strings.Contains(m.gotBody, "max-sentence") {
		t.Errorf("judging prompt rule selection wrong; body=%s", m.gotBody)
	}
}

func TestJudgeRulesNoJudgedRulesSkipsCall(t *testing.T) {
	m := &mockDoer{respaw: textResponse("should not be called")}
	c := NewClient("k")
	c.HTTP = m
	v, err := c.JudgeRules(context.Background(), "draft", []stylespec.Rule{{ID: "x", Class: "deterministic"}})
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if v != nil {
		t.Errorf("expected nil verdicts, got %+v", v)
	}
	if m.gotReq != nil {
		t.Error("expected no API call when there are no judged rules")
	}
}

func TestCompleteNon2xxIsError(t *testing.T) {
	m := &mockDoer{status: 429, respaw: `{"error":{"message":"rate limited"}}`}
	c := NewClient("k")
	c.HTTP = m
	_, err := c.Reviser()(context.Background(), "draft", enforce.Brief{Guidance: "g"})
	if err == nil {
		t.Fatal("expected error on 429")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error should carry the response body, got %v", err)
	}
}

func TestCompleteNoAPIKeyIsError(t *testing.T) {
	c := NewClient("")
	c.HTTP = &mockDoer{respaw: textResponse("x")}
	_, err := c.Reviser()(context.Background(), "draft", enforce.Brief{Guidance: "g"})
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("expected missing-API-key error, got %v", err)
	}
}

func TestMaxTokensTruncationIsError(t *testing.T) {
	// A complete-looking body but with stop_reason=max_tokens must be rejected,
	// not returned as if whole.
	m := &mockDoer{respaw: `{"stop_reason":"max_tokens","content":[{"type":"text","text":"half a rewr"}]}`}
	c := NewClient("k")
	c.HTTP = m
	_, err := c.Reviser()(context.Background(), "draft", enforce.Brief{Guidance: "g"})
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Errorf("expected truncation error, got %v", err)
	}
}

func TestTransportErrorPropagates(t *testing.T) {
	c := NewClient("k")
	c.HTTP = &mockDoer{err: errors.New("dial tcp: no route")}
	_, err := c.Reviser()(context.Background(), "draft", enforce.Brief{Guidance: "g"})
	if err == nil || !strings.Contains(err.Error(), "no route") {
		t.Errorf("expected transport error, got %v", err)
	}
}
