// Command burnish is the CLI front end over the burnish engine. It exposes:
//
//	distill    build a style profile from a single-register corpus
//	score      measure a draft's distance from a profile's target style
//	calibrate  derive a discriminator threshold from target vs decoy corpora
//	mcp        run the MCP server (stdio) exposing list_profiles/distill/score/style_review
//	serve      run the headless HTTP REST API (Fielding-style) for .NET/CI callers
//
// See DESIGN.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/paulmooreparks/burnish/discriminate"
	"github.com/paulmooreparks/burnish/distill"
	"github.com/paulmooreparks/burnish/judge"
	"github.com/paulmooreparks/burnish/lint"
	bpmcp "github.com/paulmooreparks/burnish/mcp"
	"github.com/paulmooreparks/burnish/model"
	"github.com/paulmooreparks/burnish/pkg/api"
	"github.com/paulmooreparks/burnish/retrieve"
	"github.com/paulmooreparks/burnish/serve"
	"github.com/paulmooreparks/burnish/stylespec"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "distill":
		err = cmdDistill(os.Args[2:])
	case "score":
		err = cmdScore(os.Args[2:])
	case "calibrate":
		err = cmdCalibrate(os.Args[2:])
	case "retrieve":
		err = cmdRetrieve(os.Args[2:])
	case "hook":
		err = cmdHook(os.Args[2:])
	case "mcp":
		err = cmdMCP(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "burnish: unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "burnish: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `burnish - distill a writing style and measure drafts against it

usage:
  burnish distill   --corpus DIR --register NAME [--language en] [--id ID] [--out FILE] [--avoid "—,--"] [--base FILE]
  burnish score     --profile FILE [FILE]
  burnish calibrate --target DIR --decoys DIR --register NAME [--id ID] [--out FILE] [--holdout-every N] [--avoid "—,--"] [--base FILE]
  burnish retrieve  --corpus DIR --query TEXT [-k N] [--min-words N]
  burnish hook      [--avoid "—,--"] [--profile FILE]
  burnish mcp       [--profiles DIR]
  burnish serve     --profiles DIR [--addr :8080] [--model NAME]

  --avoid lists terms this author avoids (hard violations); burnish imposes no
  universal avoidance. --base inherits a shared base profile (cross-register
  invariants) from a file.
  calibrate builds a profile from the target corpus and derives a discriminator
  threshold separating held-out target text from the decoy (off-style) corpus.
  retrieve returns the corpus passages most relevant to a query, as target-style
  few-shot exemplars.
  hook is the Claude Code Stop hook: reads the stop payload on stdin and blocks
  the turn on hard violations of what YOU configure (--avoid / --profile /
  $BURNISH_AVOID / $BURNISH_PROFILE). With nothing configured it enforces nothing.
  score reads the draft from FILE or, if omitted, from stdin.
  Add --json to score for machine-readable output.
  mcp runs the MCP server over stdio (list_profiles, distill, score, style_review
  tools). --profiles (or $BURNISH_PROFILE_DIR) makes profiles discoverable by id or
  register name; without it, tools need an explicit profile_path.
  serve runs the headless HTTP REST API (Fielding-style/HATEOAS) over the engine
  for agent-less callers (.NET sidecar, CI). It loads every *.profile.yaml under
  --profiles. If ANTHROPIC_API_KEY is set it also offers the massage action via the
  built-in model adapter (--model, default `+model.DefaultModel+`); without a key
  the massage action is simply absent from the API.
`)
}

func cmdMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	profilesDir := fs.String("profiles", os.Getenv("BURNISH_PROFILE_DIR"),
		"directory of *.profile.yaml files discoverable by name (list_profiles, profile=)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return bpmcp.Serve(context.Background(), *profilesDir)
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	profilesDir := fs.String("profiles", "", "directory of *.profile.yaml files to serve")
	addr := fs.String("addr", ":8080", "listen address")
	modelName := fs.String("model", model.DefaultModel, "Anthropic model for the massage action (used only if ANTHROPIC_API_KEY is set)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profilesDir == "" {
		return fmt.Errorf("serve requires --profiles")
	}
	profiles, err := serve.LoadProfiles(*profilesDir)
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		return fmt.Errorf("no *.profile.yaml files under %s", *profilesDir)
	}

	opts := serve.Options{Profiles: profiles}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		mc := model.NewClient(key)
		mc.Model = *modelName
		opts.Reviser = mc.Reviser()
		opts.Judge = mc.JudgeRules // judged-rule verdicts inside the massage loop
	}
	return serve.New(opts).ListenAndServe(context.Background(), *addr)
}

func cmdDistill(args []string) error {
	fs := flag.NewFlagSet("distill", flag.ContinueOnError)
	corpus := fs.String("corpus", "", "directory of single-register .md/.txt documents")
	register := fs.String("register", "", "register name (e.g. long-form-design-doc)")
	id := fs.String("id", "", "profile id (defaults to register)")
	out := fs.String("out", "", "output profile path (defaults to <id>.profile.yaml)")
	language := fs.String("language", distill.DefaultLanguage, "corpus language code (only 'en' implemented)")
	avoid := fs.String("avoid", "", "comma-separated terms this author avoids (hard violations), e.g. \"—,--\"")
	base := fs.String("base", "", "path to a base profile to inherit (shared cross-register invariants)")
	rulesFile := fs.String("rules-file", "", "YAML profile whose judged (LLM-induced) rules to merge in")
	if err := fs.Parse(args); err != nil {
		return err
	}
	corpusV, registerV, idV, outV := *corpus, *register, *id, *out
	if corpusV == "" || registerV == "" {
		return fmt.Errorf("distill requires --corpus and --register")
	}
	if idV == "" {
		idV = registerV
	}
	if outV == "" {
		outV = idV + ".profile.yaml"
	}

	docs, err := distill.ReadCorpusDir(corpusV)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("no .md or .txt documents under %s", corpusV)
	}

	// Shared distill-and-finish path (mining, base inheritance, judged-rule merge),
	// identical to the MCP tool so the two front ends cannot drift.
	outcome, err := api.Distill(docs, api.DistillOptions{
		ID:        idV,
		Register:  registerV,
		Language:  *language,
		Avoid:     splitCSV(*avoid),
		BasePath:  *base,
		RulesFile: *rulesFile,
	})
	if err != nil {
		return err
	}
	prof := outcome.Profile
	for _, skipped := range outcome.SkippedJudged {
		fmt.Fprintf(os.Stderr, "  warning: judged rule %q collides with an existing rule id; skipped\n", skipped)
	}
	if err := prof.Save(outV); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "distilled %d documents (%d words) -> %s\n", prof.Corpus.Documents, prof.Corpus.Words, outV)
	fmt.Fprintf(os.Stderr, "  %d features, %d preferred lexicon terms, %d deterministic rules, %d judged rules\n", len(prof.Features), len(prof.Lexicon.Preferred), outcome.DeterministicRules, outcome.JudgedRules)
	if prof.Corpus.Documents < 5 {
		fmt.Fprintf(os.Stderr, "  warning: thin corpus (%d docs); target ranges are low-confidence\n", prof.Corpus.Documents)
	}
	return nil
}

func cmdScore(args []string) error {
	fs := flag.NewFlagSet("score", flag.ContinueOnError)
	profile := fs.String("profile", "", "path to a distilled profile YAML")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	positional := fs.Args()
	if *profile == "" {
		return fmt.Errorf("score requires --profile")
	}

	prof, err := stylespec.Load(*profile)
	if err != nil {
		return err
	}

	var draft []byte
	if len(positional) > 0 {
		draft, err = os.ReadFile(positional[0])
		if err != nil {
			return fmt.Errorf("read draft: %w", err)
		}
	} else {
		draft, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
	}

	res, err := lint.Check(string(draft), prof)
	if err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			lint.Result
			RuleViolations []judge.RuleViolation `json:"rule_violations,omitempty"`
		}{res, judge.CheckRules(string(draft), prof.Rules)})
	}
	printResult(prof, res)
	if rv := judge.CheckRules(string(draft), prof.Rules); len(rv) > 0 {
		fmt.Println("\nrule violations:")
		for _, v := range rv {
			fmt.Printf("  [%s] %s\n      para %d: %s\n", v.Severity, v.Statement, v.Paragraph, v.Evidence)
		}
	}
	if res.HardViolations > 0 {
		os.Exit(1)
	}
	return nil
}

func cmdCalibrate(args []string) error {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	target := fs.String("target", "", "directory of target-style .md/.txt documents")
	decoys := fs.String("decoys", "", "directory of off-style (decoy) .md/.txt documents")
	register := fs.String("register", "", "register name")
	id := fs.String("id", "", "profile id (defaults to register)")
	out := fs.String("out", "", "output profile path (defaults to <id>.profile.yaml)")
	language := fs.String("language", distill.DefaultLanguage, "language code (only 'en' implemented)")
	holdout := fs.Int("holdout-every", 4, "hold out every Nth target doc for evaluation")
	avoid := fs.String("avoid", "", "comma-separated terms this author avoids (hard violations), e.g. \"—,--\"")
	base := fs.String("base", "", "path to a base profile to inherit (shared cross-register invariants)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *target == "" || *decoys == "" || *register == "" {
		return fmt.Errorf("calibrate requires --target, --decoys, and --register")
	}
	idV := *id
	if idV == "" {
		idV = *register
	}
	outV := *out
	if outV == "" {
		outV = idV + ".profile.yaml"
	}

	targetDocs, err := distill.ReadCorpusDir(*target)
	if err != nil {
		return err
	}
	decoyDocs, err := distill.ReadCorpusDir(*decoys)
	if err != nil {
		return err
	}
	if len(targetDocs) == 0 || len(decoyDocs) == 0 {
		return fmt.Errorf("need .md/.txt docs in both --target (%d) and --decoys (%d)", len(targetDocs), len(decoyDocs))
	}

	opts := discriminate.DefaultOptions()
	opts.HoldoutEvery = *holdout
	opts.Distill.Avoid = splitCSV(*avoid)
	prof, cal, err := discriminate.Calibrate(idV, *register, *language, targetDocs, decoyDocs, opts)
	if err != nil {
		return err
	}
	prof.Rules = judge.Mine(targetDocs, judge.DefaultMinSupport)
	prof.Inherits = *base
	if prof, err = stylespec.Resolve(prof, ""); err != nil {
		return err
	}
	if err := prof.Save(outV); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "calibrated %s -> %s (profile built from %d train docs)\n", idV, outV, cal.NTrain)
	fmt.Printf("discriminator (distance-threshold):\n")
	fmt.Printf("  AUC:        %.3f  (0.5 = chance, 1.0 = perfect separation)\n", cal.AUC)
	fmt.Printf("  threshold:  %.4f  (on-target if distance <= threshold)\n", cal.Threshold)
	fmt.Printf("  at thresh:  TPR %.0f%%, FPR %.0f%%, accuracy %.0f%%\n", cal.TPR*100, cal.FPR*100, cal.Accuracy*100)
	fmt.Printf("  evaluated:  %d held-out target vs %d decoys (%d train)\n", cal.NTargetHoldout, cal.NDecoy, cal.NTrain)
	if !cal.Separates {
		fmt.Printf("\nWARNING: %s\n", cal.Warning)
	}
	return nil
}

func cmdRetrieve(args []string) error {
	fs := flag.NewFlagSet("retrieve", flag.ContinueOnError)
	corpus := fs.String("corpus", "", "directory of target-style .md/.txt documents")
	query := fs.String("query", "", "query text (defaults to stdin)")
	k := fs.Int("k", 3, "number of exemplars to return")
	minWords := fs.Int("min-words", 20, "skip corpus chunks shorter than this")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *corpus == "" {
		return fmt.Errorf("retrieve requires --corpus")
	}
	q := *query
	if q == "" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read query from stdin: %w", err)
		}
		q = string(b)
	}
	if strings.TrimSpace(q) == "" {
		return fmt.Errorf("retrieve requires a non-empty query (--query or stdin)")
	}

	docs, err := distill.ReadCorpusDir(*corpus)
	if err != nil {
		return err
	}
	rdocs := make([]retrieve.Document, len(docs))
	for i, d := range docs {
		rdocs[i] = retrieve.Document{Name: d.Name, Text: d.Text}
	}
	bank := retrieve.Build(rdocs, retrieve.Options{MinWords: *minWords})
	results := bank.Retrieve(q, *k)
	if len(results) == 0 {
		fmt.Println("no relevant exemplars found.")
		return nil
	}
	fmt.Printf("top %d exemplars (of %d chunks):\n", len(results), len(bank.Chunks))
	for i, r := range results {
		fmt.Printf("\n%d. [%.3f] %s #%d\n   %s\n", i+1, r.Score, r.Chunk.Source, r.Chunk.Index, oneLine(r.Chunk.Text, 240))
	}
	return nil
}

// splitCSV splits a comma-separated flag value into trimmed, non-empty terms.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

func printResult(p *stylespec.Profile, res lint.Result) {
	fmt.Printf("profile: %s (register %s)\n", p.ID, p.Register)
	fmt.Printf("distance to style: %.3f stddev (0 = within all target ranges)\n", res.Distance)
	if res.OnTarget != nil {
		verdict := "OFF-TARGET"
		if *res.OnTarget {
			verdict = "ON-TARGET"
		}
		fmt.Printf("discriminator: %s (distance %.3f vs threshold %.3f)\n", verdict, res.Distance, *res.Threshold)
	}
	if res.HardViolations > 0 {
		fmt.Printf("HARD violations: %d\n", res.HardViolations)
	}
	if len(res.Features) > 0 {
		fmt.Println("\noff-target features (worst first):")
		for _, f := range res.Features {
			fmt.Printf("  %-20s value=%.3g  target=%s  %.2f stddev out  [%s]\n",
				f.ID, f.Value, rangeStr(f.Min, f.Max), f.Deviation, f.Severity)
		}
	}
	if len(res.Lexical) > 0 {
		fmt.Println("\navoided terms present:")
		for _, l := range res.Lexical {
			fmt.Printf("  %q at byte %d\n", l.Term, l.Start)
		}
	}
	if len(res.Features) == 0 && len(res.Lexical) == 0 {
		fmt.Println("\nwithin target on every measured feature.")
	}
}

func rangeStr(min, max *float64) string {
	lo, hi := "-inf", "+inf"
	if min != nil {
		lo = fmt.Sprintf("%.3g", *min)
	}
	if max != nil {
		hi = fmt.Sprintf("%.3g", *max)
	}
	return "[" + lo + ", " + hi + "]"
}
