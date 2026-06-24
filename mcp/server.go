package mcp

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/paulmooreparks/burnish/distill"
	"github.com/paulmooreparks/burnish/lint"
	"github.com/paulmooreparks/burnish/stylespec"
)

// version is reported to clients in the server implementation info.
const version = "0.1.0"

// Serve runs the burnish MCP server on the stdio transport until the client
// disconnects or ctx is cancelled.
func Serve(ctx context.Context) error {
	return NewServer().Run(ctx, &sdk.StdioTransport{})
}

// NewServer builds the MCP server with the distill, score, and style_review
// tools registered.
func NewServer() *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "burnish", Version: version}, nil)

	sdk.AddTool(s, &sdk.Tool{
		Name: "distill",
		Description: "Distill a single-register corpus (a directory of .md/.txt files) into a " +
			"style profile written to disk. Feed one genre at a time; mixing registers averages to mush.",
	}, handleDistill)

	sdk.AddTool(s, &sdk.Tool{
		Name: "score",
		Description: "Score a draft against a distilled profile. Returns a distance-to-style number " +
			"(weighted stddev deviation; 0 = within all target ranges), the off-target features, and " +
			"any hard violations (e.g. em-dashes). Deterministic, no model.",
	}, handleScore)

	sdk.AddTool(s, &sdk.Tool{
		Name: "style_review",
		Description: "Review a draft against a profile and return a revision payload: the deterministic " +
			"gap report plus the profile's preferred/avoided lexicon and rules. The judgement (rule judge " +
			"and calibrated discriminator) is not yet built; you, the calling agent, render it. Judge in a " +
			"FRESH, ISOLATED context, not the one that wrote the draft, to avoid grading your own work.",
	}, handleStyleReview)

	return s
}

// --- distill -------------------------------------------------------------

type distillArgs struct {
	CorpusDir string `json:"corpus_dir" jsonschema:"directory of single-register .md/.txt documents"`
	Register  string `json:"register" jsonschema:"register/genre name, e.g. long-form-design-doc"`
	Language  string `json:"language,omitempty" jsonschema:"language code; defaults to en (only en implemented)"`
	ID        string `json:"id,omitempty" jsonschema:"profile id; defaults to the register name"`
	Out       string `json:"out,omitempty" jsonschema:"output profile path; defaults to <id>.profile.yaml"`
}

type distillResult struct {
	ProfilePath  string `json:"profile_path"`
	Documents    int    `json:"documents"`
	Words        int    `json:"words"`
	Features     int    `json:"features"`
	LexiconTerms int    `json:"lexicon_terms"`
	Note         string `json:"note,omitempty"`
}

func handleDistill(ctx context.Context, _ *sdk.CallToolRequest, a distillArgs) (*sdk.CallToolResult, distillResult, error) {
	if a.CorpusDir == "" || a.Register == "" {
		return errResult[distillResult]("distill requires corpus_dir and register")
	}
	id := a.ID
	if id == "" {
		id = a.Register
	}
	out := a.Out
	if out == "" {
		out = id + ".profile.yaml"
	}

	docs, err := distill.ReadCorpusDir(a.CorpusDir)
	if err != nil {
		return errResult[distillResult](fmt.Sprintf("read corpus: %v", err))
	}
	if len(docs) == 0 {
		return errResult[distillResult](fmt.Sprintf("no .md or .txt documents under %s", a.CorpusDir))
	}

	prof, err := distill.Distill(id, a.Register, a.Language, docs, distill.DefaultOptions())
	if err != nil {
		return errResult[distillResult](err.Error())
	}
	if err := prof.Save(out); err != nil {
		return errResult[distillResult](err.Error())
	}

	res := distillResult{
		ProfilePath:  out,
		Documents:    prof.Corpus.Documents,
		Words:        prof.Corpus.Words,
		Features:     len(prof.Features),
		LexiconTerms: len(prof.Lexicon.Preferred),
	}
	if prof.Corpus.Documents < 5 {
		res.Note = fmt.Sprintf("thin corpus (%d docs); target ranges are low-confidence", prof.Corpus.Documents)
	}
	summary := fmt.Sprintf("distilled %d documents (%d words) -> %s\n%d features, %d preferred lexicon terms",
		res.Documents, res.Words, res.ProfilePath, res.Features, res.LexiconTerms)
	if res.Note != "" {
		summary += "\nwarning: " + res.Note
	}
	return textResult(summary, res)
}

// --- score ---------------------------------------------------------------

type scoreArgs struct {
	ProfilePath string `json:"profile_path" jsonschema:"path to a distilled profile YAML"`
	Text        string `json:"text" jsonschema:"the draft text to score"`
}

func handleScore(ctx context.Context, _ *sdk.CallToolRequest, a scoreArgs) (*sdk.CallToolResult, lint.Result, error) {
	prof, res, err := loadAndCheck(a.ProfilePath, a.Text)
	if err != nil {
		return errResult[lint.Result](err.Error())
	}
	return textResult(scoreSummary(prof, res), res)
}

// --- style_review --------------------------------------------------------

type reviewArgs struct {
	ProfilePath string `json:"profile_path" jsonschema:"path to a distilled profile YAML"`
	Text        string `json:"text" jsonschema:"the draft text to review"`
}

type reviewResult struct {
	Distance         float64                  `json:"distance"`
	HardViolations   int                      `json:"hard_violations"`
	Features         []lint.FeatureViolation  `json:"features,omitempty"`
	Lexical          []lint.LexicalViolation  `json:"lexical,omitempty"`
	PreferredLexicon []string                 `json:"preferred_lexicon,omitempty"`
	AvoidedLexicon   []string                 `json:"avoided_lexicon,omitempty"`
	Rules            []stylespec.Rule         `json:"rules,omitempty"`
	Judgement        string                   `json:"judgement"`
	Guidance         string                   `json:"guidance"`
}

func handleStyleReview(ctx context.Context, _ *sdk.CallToolRequest, a reviewArgs) (*sdk.CallToolResult, reviewResult, error) {
	prof, res, err := loadAndCheck(a.ProfilePath, a.Text)
	if err != nil {
		return errResult[reviewResult](err.Error())
	}
	rev := reviewResult{
		Distance:         res.Distance,
		HardViolations:   res.HardViolations,
		Features:         res.Features,
		Lexical:          res.Lexical,
		PreferredLexicon: prof.Lexicon.Preferred,
		AvoidedLexicon:   prof.Lexicon.Avoided,
		Rules:            prof.Rules,
		Judgement:        "not-yet-available: the rule judge and calibrated discriminator are not built; render judgement yourself",
		Guidance: "Revise the draft to bring the off-target features into range and remove avoided terms, " +
			"favoring the preferred lexicon. Then judge whether it now reads like the target corpus in a " +
			"FRESH, ISOLATED context (a separate subagent or invocation), never the one that wrote it.",
	}
	return textResult(reviewSummary(prof, res), rev)
}

// --- shared helpers ------------------------------------------------------

func loadAndCheck(profilePath, text string) (*stylespec.Profile, lint.Result, error) {
	if profilePath == "" {
		return nil, lint.Result{}, fmt.Errorf("profile_path is required")
	}
	prof, err := stylespec.Load(profilePath)
	if err != nil {
		return nil, lint.Result{}, err
	}
	res, err := lint.Check(text, prof)
	if err != nil {
		return nil, lint.Result{}, err
	}
	return prof, res, nil
}

func scoreSummary(p *stylespec.Profile, res lint.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "profile: %s (register %s, language %s)\n", p.ID, p.Register, p.Language)
	fmt.Fprintf(&b, "distance to style: %.3f stddev (0 = within all target ranges)\n", res.Distance)
	if res.HardViolations > 0 {
		fmt.Fprintf(&b, "HARD violations: %d\n", res.HardViolations)
	}
	if len(res.Features) > 0 {
		b.WriteString("off-target features (worst first):\n")
		for _, f := range res.Features {
			fmt.Fprintf(&b, "  %-20s value=%.3g  %.2f stddev out  [%s]\n", f.ID, f.Value, f.Deviation, f.Severity)
		}
	}
	if len(res.Lexical) > 0 {
		b.WriteString("avoided terms present:\n")
		for _, l := range res.Lexical {
			fmt.Fprintf(&b, "  %q at byte %d\n", l.Term, l.Start)
		}
	}
	if len(res.Features) == 0 && len(res.Lexical) == 0 {
		b.WriteString("within target on every measured feature.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func reviewSummary(p *stylespec.Profile, res lint.Result) string {
	s := scoreSummary(p, res)
	if len(p.Lexicon.Preferred) > 0 {
		s += "\npreferred lexicon: " + strings.Join(p.Lexicon.Preferred, ", ")
	}
	s += "\njudgement: render it yourself in a fresh, isolated context (judge/discriminator not yet built)."
	return s
}

func textResult[T any](summary string, structured T) (*sdk.CallToolResult, T, error) {
	return &sdk.CallToolResult{
		Content: []sdk.Content{&sdk.TextContent{Text: summary}},
	}, structured, nil
}

func errResult[T any](msg string) (*sdk.CallToolResult, T, error) {
	var zero T
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: msg}},
	}, zero, nil
}
