package mcp

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/paulmooreparks/burnish/distill"
	"github.com/paulmooreparks/burnish/judge"
	"github.com/paulmooreparks/burnish/lint"
	"github.com/paulmooreparks/burnish/pkg/api"
	"github.com/paulmooreparks/burnish/stylespec"
)

// version is reported to clients in the server implementation info.
const version = "0.1.0"

// instructions is the server-level protocol sent to clients in the initialize
// result. A connected agent sees this as its source of truth for HOW to use
// burnish, the per-tool descriptions only describe each tool in isolation and
// cannot carry the loop, the profile choice, or the fresh-context discipline.
const instructions = `burnish enforces a target writing style on LLM output. A generating model only
biases toward a style and forgets it as context grows; burnish moves enforcement
OUT of the generator into deterministic checks plus an isolated judge, so style
is measured, not merely hoped for.

Use it as a loop around your own drafting:
 1. Draft normally.
 2. Before you return prose to the user, call style_review with the draft and the
    target register's profile (pass profile= with an id or register name; call
    list_profiles first if you do not know it, or pass an explicit profile_path).
    It returns a deterministic gap report
    (off-target features, avoided terms present, rule violations), the distance-to-
    style number and an on-target/off-target verdict when the profile is calibrated,
    the preferred/avoided lexicon, the rules, and, when the profile has subjective
    judged rules, a judging_prompt.
 3. Revise to close the gaps: bring off-target features into range, drop avoided
    terms, favor the preferred lexicon, fix the quoted rule violations.
 4. Re-run style_review. Stop when the verdict is on-target with zero hard
    violations, or after 3 rounds, whichever comes first. Returns diminish past 3 rounds.

Judge in a FRESH, ISOLATED context. The subjective judged rules (judging_prompt)
and any holistic "does this read like the corpus" call must be made by a separate
subagent or invocation, never the context that wrote the draft, which cannot grade
its own work. The deterministic results (distance, rule_violations, discriminator
verdict) need no model and are trustworthy as returned.

Profiles: each profile is a distilled style for ONE register. Never mix registers,
a single global profile averages distinct voices to mush. If you do not know which
profile to pass, call list_profiles and choose by register; do not guess.

Tool map: list_profiles enumerates the available profiles; score is the cheap
deterministic-only check; style_review is the full revision payload you loop on;
distill builds a profile from a single-register corpus; calibrate builds a profile
AND derives the discriminator threshold that gives score/style_review their
on-target verdict; retrieve returns target-style exemplar passages to ground a
revision.`

// server holds the per-instance configuration shared by the tool handlers. The
// profiles directory is what lets a caller name a register instead of passing a
// filesystem path: list_profiles enumerates it, and score/style_review resolve
// a profile name against it.
type server struct {
	profilesDir string
}

// Serve runs the burnish MCP server on the stdio transport until the client
// disconnects or ctx is cancelled. profilesDir is the directory whose
// *.profile.yaml files are discoverable by name; pass "" to disable name
// resolution (callers must then give an explicit profile_path).
func Serve(ctx context.Context, profilesDir string) error {
	return NewServer(profilesDir).Run(ctx, &sdk.StdioTransport{})
}

// NewServer builds the MCP server with the list_profiles, distill, score, and
// style_review tools registered. profilesDir backs profile-by-name resolution;
// "" leaves only the explicit profile_path path working.
func NewServer(profilesDir string) *sdk.Server {
	srv := &server{profilesDir: profilesDir}
	s := sdk.NewServer(&sdk.Implementation{Name: "burnish", Version: version},
		&sdk.ServerOptions{Instructions: instructions})

	sdk.AddTool(s, &sdk.Tool{
		Name: "list_profiles",
		Description: "List the distilled style profiles available to this server, each with its id, " +
			"register, language, whether it is calibrated, and its path. Call this first to learn which " +
			"profile to pass to score/style_review; you can then refer to one by id or register name, not " +
			"by filesystem path.",
	}, srv.handleListProfiles)

	sdk.AddTool(s, &sdk.Tool{
		Name: "distill",
		Description: "Distill a single-register corpus (a directory of .md/.txt files) into a " +
			"style profile written to disk. Feed one genre at a time; mixing registers averages to mush.",
	}, srv.handleDistill)

	sdk.AddTool(s, &sdk.Tool{
		Name: "calibrate",
		Description: "Build a profile from a target corpus AND derive a calibrated discriminator threshold " +
			"separating held-out target text from a decoy (off-style) corpus. Writes the profile to disk and " +
			"returns the threshold, AUC, TPR/FPR, and accuracy. A calibrated profile is what makes score/style_review " +
			"return an on-target/off-target verdict. Same corpus discipline as distill: one register.",
	}, srv.handleCalibrate)

	sdk.AddTool(s, &sdk.Tool{
		Name: "retrieve",
		Description: "Return the corpus passages most relevant to a query, as target-style few-shot exemplars " +
			"to ground a revision. Reads a corpus directory and ranks chunks against the query (TF-IDF).",
	}, srv.handleRetrieve)

	sdk.AddTool(s, &sdk.Tool{
		Name: "score",
		Description: "Score a draft against a distilled profile. Identify the profile by `profile` (an id " +
			"or register name resolved against the server's profiles directory; call list_profiles) or by an " +
			"explicit `profile_path`. Returns a distance-to-style number (weighted stddev deviation; 0 = within " +
			"all target ranges), the off-target features, and any hard violations (e.g. em-dashes). Deterministic, no model.",
	}, srv.handleScore)

	sdk.AddTool(s, &sdk.Tool{
		Name: "style_review",
		Description: "Review a draft against a profile and return the full revision payload: the deterministic " +
			"gap report (off-target features, avoided terms, deterministic rule violations), the distance-to-style " +
			"number and calibrated on-target/off-target verdict when the profile is calibrated, the preferred/avoided " +
			"lexicon, the rules, and a judging_prompt for the profile's subjective judged rules. Identify the profile " +
			"by `profile` (id or register name; call list_profiles) or by an explicit `profile_path`. The deterministic " +
			"checks and the discriminator verdict are computed here; you, the calling agent, render the subjective " +
			"judged-rule verdict, in a FRESH, ISOLATED context, never the one that wrote the draft.",
	}, srv.handleStyleReview)

	return s
}

// --- list_profiles -------------------------------------------------------

type listProfilesResult struct {
	ProfilesDir string                  `json:"profiles_dir"`
	Profiles    []stylespec.ProfileInfo `json:"profiles"`
	Note        string                  `json:"note,omitempty"`
}

func (s *server) handleListProfiles(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, listProfilesResult, error) {
	if s.profilesDir == "" {
		res := listProfilesResult{Note: "no profiles directory configured; pass an explicit profile_path, or start the server with --profiles / $BURNISH_PROFILE_DIR"}
		return textResult(res.Note, res)
	}
	infos, err := stylespec.ListProfiles(s.profilesDir)
	if err != nil {
		return errResult[listProfilesResult](fmt.Sprintf("list profiles in %s: %v", s.profilesDir, err))
	}
	res := listProfilesResult{ProfilesDir: s.profilesDir, Profiles: infos}
	if len(infos) == 0 {
		res.Note = "no *.profile.yaml files found in " + s.profilesDir
	}
	return textResult(listProfilesSummary(res), res)
}

func listProfilesSummary(res listProfilesResult) string {
	if len(res.Profiles) == 0 {
		if res.Note != "" {
			return res.Note
		}
		return "no profiles available"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d profile(s) in %s:\n", len(res.Profiles), res.ProfilesDir)
	for _, p := range res.Profiles {
		cal := "uncalibrated"
		if p.Calibrated {
			cal = "calibrated"
		}
		fmt.Fprintf(&b, "  %-20s register=%s language=%s [%s]\n", p.ID, p.Register, p.Language, cal)
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- distill -------------------------------------------------------------

type distillArgs struct {
	CorpusDir string `json:"corpus_dir" jsonschema:"directory of single-register .md/.txt documents"`
	Register  string `json:"register" jsonschema:"register/genre name, e.g. long-form-design-doc"`
	Language  string `json:"language,omitempty" jsonschema:"language code; defaults to en (only en implemented)"`
	ID        string `json:"id,omitempty" jsonschema:"profile id; defaults to the register name"`
	Out       string `json:"out,omitempty" jsonschema:"output profile path; defaults to <id>.profile.yaml"`
	Avoid     string `json:"avoid,omitempty" jsonschema:"comma-separated terms this author avoids (hard violations), e.g. \"—,--\""`
	Base      string `json:"base,omitempty" jsonschema:"path to a base profile to inherit (shared cross-register invariants)"`
	RulesFile string `json:"rules_file,omitempty" jsonschema:"path to a YAML profile whose judged (LLM-induced) rules to merge in"`
}

type distillResult struct {
	ProfilePath        string `json:"profile_path"`
	Documents          int    `json:"documents"`
	Words              int    `json:"words"`
	Features           int    `json:"features"`
	LexiconTerms       int    `json:"lexicon_terms"`
	DeterministicRules int    `json:"deterministic_rules"`
	JudgedRules        int    `json:"judged_rules"`
	Note               string `json:"note,omitempty"`
}

func (s *server) handleDistill(ctx context.Context, _ *sdk.CallToolRequest, a distillArgs) (*sdk.CallToolResult, distillResult, error) {
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

	// Shared distill-and-finish path: same mining, base inheritance, and judged-
	// rule merge as the CLI, so the MCP profile is never a weaker one.
	outcome, err := api.Distill(docs, api.DistillOptions{
		ID:        id,
		Register:  a.Register,
		Language:  a.Language,
		Avoid:     splitCSV(a.Avoid),
		BasePath:  a.Base,
		RulesFile: a.RulesFile,
	})
	if err != nil {
		return errResult[distillResult](err.Error())
	}
	prof := outcome.Profile
	if err := prof.Save(out); err != nil {
		return errResult[distillResult](err.Error())
	}

	res := distillResult{
		ProfilePath:        out,
		Documents:          prof.Corpus.Documents,
		Words:              prof.Corpus.Words,
		Features:           len(prof.Features),
		LexiconTerms:       len(prof.Lexicon.Preferred),
		DeterministicRules: outcome.DeterministicRules,
		JudgedRules:        outcome.JudgedRules,
	}
	if prof.Corpus.Documents < 5 {
		res.Note = fmt.Sprintf("thin corpus (%d docs); target ranges are low-confidence", prof.Corpus.Documents)
	}
	summary := fmt.Sprintf("distilled %d documents (%d words) -> %s\n%d features, %d preferred lexicon terms, %d deterministic rules, %d judged rules",
		res.Documents, res.Words, res.ProfilePath, res.Features, res.LexiconTerms, res.DeterministicRules, res.JudgedRules)
	if len(outcome.SkippedJudged) > 0 {
		summary += fmt.Sprintf("\nnote: skipped %d judged rule(s) on id collision: %s", len(outcome.SkippedJudged), strings.Join(outcome.SkippedJudged, ", "))
	}
	if res.Note != "" {
		summary += "\nwarning: " + res.Note
	}
	return textResult(summary, res)
}

// --- calibrate -----------------------------------------------------------

type calibrateArgs struct {
	TargetDir    string `json:"target_dir" jsonschema:"directory of target-style .md/.txt documents"`
	DecoysDir    string `json:"decoys_dir" jsonschema:"directory of off-style (decoy) .md/.txt documents"`
	Register     string `json:"register" jsonschema:"register/genre name"`
	Language     string `json:"language,omitempty" jsonschema:"language code; defaults to en (only en implemented)"`
	ID           string `json:"id,omitempty" jsonschema:"profile id; defaults to the register name"`
	Out          string `json:"out,omitempty" jsonschema:"output profile path; defaults to <id>.profile.yaml"`
	Avoid        string `json:"avoid,omitempty" jsonschema:"comma-separated terms this author avoids (hard violations)"`
	Base         string `json:"base,omitempty" jsonschema:"path to a base profile to inherit (shared cross-register invariants)"`
	HoldoutEvery int    `json:"holdout_every,omitempty" jsonschema:"hold out every Nth target doc for evaluation; defaults to 4"`
}

type calibrateResult struct {
	ProfilePath        string  `json:"profile_path"`
	Threshold          float64 `json:"threshold"`
	AUC                float64 `json:"auc"`
	TPR                float64 `json:"tpr"`
	FPR                float64 `json:"fpr"`
	Accuracy           float64 `json:"accuracy"`
	TargetHoldout      int     `json:"target_holdout"`
	Decoys             int     `json:"decoys"`
	Train              int     `json:"train"`
	DeterministicRules int     `json:"deterministic_rules"`
	Separates          bool    `json:"separates"`
	Warning            string  `json:"warning,omitempty"`
}

func (s *server) handleCalibrate(ctx context.Context, _ *sdk.CallToolRequest, a calibrateArgs) (*sdk.CallToolResult, calibrateResult, error) {
	if a.TargetDir == "" || a.DecoysDir == "" || a.Register == "" {
		return errResult[calibrateResult]("calibrate requires target_dir, decoys_dir, and register")
	}
	id := a.ID
	if id == "" {
		id = a.Register
	}
	out := a.Out
	if out == "" {
		out = id + ".profile.yaml"
	}

	targetDocs, err := distill.ReadCorpusDir(a.TargetDir)
	if err != nil {
		return errResult[calibrateResult](fmt.Sprintf("read target: %v", err))
	}
	decoyDocs, err := distill.ReadCorpusDir(a.DecoysDir)
	if err != nil {
		return errResult[calibrateResult](fmt.Sprintf("read decoys: %v", err))
	}
	if len(targetDocs) == 0 || len(decoyDocs) == 0 {
		return errResult[calibrateResult](fmt.Sprintf("need .md/.txt docs in both target_dir (%d) and decoys_dir (%d)", len(targetDocs), len(decoyDocs)))
	}

	outcome, err := api.Calibrate(targetDocs, decoyDocs, api.CalibrateOptions{
		ID:           id,
		Register:     a.Register,
		Language:     a.Language,
		Avoid:        splitCSV(a.Avoid),
		BasePath:     a.Base,
		HoldoutEvery: a.HoldoutEvery,
	})
	if err != nil {
		return errResult[calibrateResult](err.Error())
	}
	if err := outcome.Profile.Save(out); err != nil {
		return errResult[calibrateResult](err.Error())
	}

	cal := outcome.Calibration
	res := calibrateResult{
		ProfilePath:        out,
		Threshold:          cal.Threshold,
		AUC:                cal.AUC,
		TPR:                cal.TPR,
		FPR:                cal.FPR,
		Accuracy:           cal.Accuracy,
		TargetHoldout:      cal.NTargetHoldout,
		Decoys:             cal.NDecoy,
		Train:              cal.NTrain,
		DeterministicRules: outcome.DeterministicRules,
		Separates:          cal.Separates,
		Warning:            cal.Warning,
	}
	var b strings.Builder
	fmt.Fprintf(&b, "calibrated %s -> %s\n", id, out)
	fmt.Fprintf(&b, "threshold %.4f (on-target if distance <= threshold), AUC %.3f\n", res.Threshold, res.AUC)
	fmt.Fprintf(&b, "at threshold: TPR %.0f%%, FPR %.0f%%, accuracy %.0f%%\n", res.TPR*100, res.FPR*100, res.Accuracy*100)
	fmt.Fprintf(&b, "evaluated %d held-out target vs %d decoys (%d train), %d deterministic rules",
		res.TargetHoldout, res.Decoys, res.Train, res.DeterministicRules)
	if !res.Separates {
		fmt.Fprintf(&b, "\nWARNING: %s", res.Warning)
	}
	return textResult(b.String(), res)
}

// --- retrieve ------------------------------------------------------------

type retrieveArgs struct {
	CorpusDir string `json:"corpus_dir" jsonschema:"directory of target-style .md/.txt documents"`
	Query     string `json:"query" jsonschema:"the query text to find exemplars for"`
	K         int    `json:"k,omitempty" jsonschema:"number of exemplars to return; defaults to 3"`
	MinWords  int    `json:"min_words,omitempty" jsonschema:"skip corpus chunks shorter than this; defaults to 20"`
}

type exemplar struct {
	Score  float64 `json:"score"`
	Source string  `json:"source"`
	Index  int     `json:"index"`
	Text   string  `json:"text"`
}

type retrieveResult struct {
	Exemplars []exemplar `json:"exemplars"`
}

func (s *server) handleRetrieve(ctx context.Context, _ *sdk.CallToolRequest, a retrieveArgs) (*sdk.CallToolResult, retrieveResult, error) {
	if a.CorpusDir == "" || strings.TrimSpace(a.Query) == "" {
		return errResult[retrieveResult]("retrieve requires corpus_dir and a non-empty query")
	}
	docs, err := distill.ReadCorpusDir(a.CorpusDir)
	if err != nil {
		return errResult[retrieveResult](fmt.Sprintf("read corpus: %v", err))
	}
	if len(docs) == 0 {
		return errResult[retrieveResult](fmt.Sprintf("no .md or .txt documents under %s", a.CorpusDir))
	}

	results, err := api.Retrieve(docs, a.Query, api.RetrieveOptions{K: a.K, MinWords: a.MinWords})
	if err != nil {
		return errResult[retrieveResult](err.Error())
	}

	res := retrieveResult{Exemplars: make([]exemplar, len(results))}
	for i, r := range results {
		res.Exemplars[i] = exemplar{Score: r.Score, Source: r.Chunk.Source, Index: r.Chunk.Index, Text: r.Chunk.Text}
	}
	if len(results) == 0 {
		return textResult("no relevant exemplars found.", res)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "top %d exemplars:\n", len(results))
	for i, r := range results {
		fmt.Fprintf(&b, "%d. [%.3f] %s #%d\n   %s\n", i+1, r.Score, r.Chunk.Source, r.Chunk.Index, oneLine(r.Chunk.Text, 240))
	}
	return textResult(strings.TrimRight(b.String(), "\n"), res)
}

// --- score ---------------------------------------------------------------

type scoreArgs struct {
	Profile     string `json:"profile,omitempty" jsonschema:"profile id or register name, resolved against the server's profiles directory (see list_profiles)"`
	ProfilePath string `json:"profile_path,omitempty" jsonschema:"explicit path to a distilled profile YAML; an alternative to profile"`
	Text        string `json:"text" jsonschema:"the draft text to score"`
}

type scoreResult struct {
	lint.Result
	RuleViolations []judge.RuleViolation `json:"rule_violations,omitempty"`
}

func (s *server) handleScore(ctx context.Context, _ *sdk.CallToolRequest, a scoreArgs) (*sdk.CallToolResult, scoreResult, error) {
	prof, res, err := s.loadAndCheck(a.Profile, a.ProfilePath, a.Text)
	if err != nil {
		return errResult[scoreResult](err.Error())
	}
	rv := judge.CheckRules(a.Text, prof.Rules)
	return textResult(scoreSummary(prof, res)+ruleSummary(rv), scoreResult{res, rv})
}

// --- style_review --------------------------------------------------------

type reviewArgs struct {
	Profile     string `json:"profile,omitempty" jsonschema:"profile id or register name, resolved against the server's profiles directory (see list_profiles)"`
	ProfilePath string `json:"profile_path,omitempty" jsonschema:"explicit path to a distilled profile YAML; an alternative to profile"`
	Text        string `json:"text" jsonschema:"the draft text to review"`
}

type reviewResult struct {
	Distance         float64                 `json:"distance"`
	OnTarget         *bool                   `json:"on_target,omitempty"`
	Threshold        *float64                `json:"threshold,omitempty"`
	HardViolations   int                     `json:"hard_violations"`
	Features         []lint.FeatureViolation `json:"features,omitempty"`
	Lexical          []lint.LexicalViolation `json:"lexical,omitempty"`
	PreferredLexicon []string                `json:"preferred_lexicon,omitempty"`
	AvoidedLexicon   []string                `json:"avoided_lexicon,omitempty"`
	Rules            []stylespec.Rule        `json:"rules,omitempty"`
	RuleViolations   []judge.RuleViolation   `json:"rule_violations,omitempty"`
	JudgingPrompt    string                  `json:"judging_prompt,omitempty"`
	Judgement        string                  `json:"judgement"`
	Guidance         string                  `json:"guidance"`
}

func (s *server) handleStyleReview(ctx context.Context, _ *sdk.CallToolRequest, a reviewArgs) (*sdk.CallToolResult, reviewResult, error) {
	prof, res, err := s.loadAndCheck(a.Profile, a.ProfilePath, a.Text)
	if err != nil {
		return errResult[reviewResult](err.Error())
	}
	judgement := "no calibrated discriminator on this profile; render judgement yourself"
	if res.OnTarget != nil {
		if *res.OnTarget {
			judgement = "deterministic discriminator: ON-TARGET (distance within calibrated threshold)"
		} else {
			judgement = "deterministic discriminator: OFF-TARGET (distance exceeds calibrated threshold)"
		}
	}
	rv := judge.CheckRules(a.Text, prof.Rules)
	var judgingPrompt string
	if len(judge.JudgedRules(prof.Rules)) > 0 {
		judgingPrompt = judge.JudgingPrompt(a.Text, prof.Rules)
	}
	rev := reviewResult{
		Distance:         res.Distance,
		OnTarget:         res.OnTarget,
		Threshold:        res.Threshold,
		HardViolations:   res.HardViolations,
		Features:         res.Features,
		Lexical:          res.Lexical,
		PreferredLexicon: prof.Lexicon.Preferred,
		AvoidedLexicon:   prof.Lexicon.Avoided,
		Rules:            prof.Rules,
		RuleViolations:   rv,
		JudgingPrompt:    judgingPrompt,
		Judgement:        judgement,
		Guidance: "Revise the draft to bring the off-target features into range, remove avoided terms, favor the " +
			"preferred lexicon, and fix the listed deterministic rule_violations. The deterministic discriminator " +
			"gives a distance-threshold verdict and the deterministic rules are checked here; for the subjective " +
			"judged rules, run judging_prompt yourself. Judge in a FRESH, ISOLATED context (a separate subagent or " +
			"invocation), never the one that wrote the draft.",
	}
	return textResult(reviewSummary(prof, res)+ruleSummary(rv), rev)
}

// --- shared helpers ------------------------------------------------------

func ruleSummary(rv []judge.RuleViolation) string {
	if len(rv) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nrule violations:\n")
	for _, v := range rv {
		fmt.Fprintf(&b, "  [%s] %s (para %d): %s\n", v.Severity, v.Statement, v.Paragraph, v.Evidence)
	}
	return strings.TrimRight(b.String(), "\n")
}

// loadAndCheck resolves the profile (by name against the server's profiles
// directory, or by explicit path) and lints text against it. profilePath wins
// when both are given.
func (s *server) loadAndCheck(profile, profilePath, text string) (*stylespec.Profile, lint.Result, error) {
	ref := profilePath
	if ref == "" {
		ref = profile
	}
	if ref == "" {
		return nil, lint.Result{}, fmt.Errorf("a profile is required: pass `profile` (an id or register name; see list_profiles) or `profile_path`")
	}
	prof, err := stylespec.ResolveProfile(s.profilesDir, ref)
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
	if res.OnTarget != nil {
		verdict := "OFF-TARGET"
		if *res.OnTarget {
			verdict = "ON-TARGET"
		}
		fmt.Fprintf(&b, "discriminator: %s (distance %.3f vs threshold %.3f)\n", verdict, res.Distance, *res.Threshold)
	}
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
	s += "\njudgement: deterministic checks and the discriminator verdict are above; render the subjective" +
		" judged-rule verdict yourself in a fresh, isolated context."
	return s
}

// splitCSV splits a comma-separated value into trimmed, non-empty terms.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// oneLine collapses whitespace and truncates s to at most n runes, for compact
// exemplar previews in the text summary.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "..."
	}
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
