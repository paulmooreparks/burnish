// Command burnish is the CLI front end over the burnish engine. It exposes:
//
//	distill    build a style profile from a single-register corpus
//	score      measure a draft's distance from a profile's target style
//	calibrate  derive a discriminator threshold from target vs decoy corpora
//	mcp        run the MCP server (stdio) exposing distill/score/style_review
//
// The judge, retrieval, full massage loop, and the Claude Code Stop hook are
// later increments. See DESIGN.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/paulmooreparks/burnish/discriminate"
	"github.com/paulmooreparks/burnish/distill"
	"github.com/paulmooreparks/burnish/judge"
	"github.com/paulmooreparks/burnish/lint"
	bpmcp "github.com/paulmooreparks/burnish/mcp"
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
	case "mcp":
		err = bpmcp.Serve(context.Background())
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
  burnish distill   --corpus DIR --register NAME [--language en] [--id ID] [--out FILE]
  burnish score     --profile FILE [FILE]
  burnish calibrate --target DIR --decoys DIR --register NAME [--id ID] [--out FILE] [--holdout-every N]
  burnish mcp

  calibrate builds a profile from the target corpus and derives a discriminator
  threshold separating held-out target text from the decoy (off-style) corpus.
  score reads the draft from FILE or, if omitted, from stdin.
  Add --json to score for machine-readable output.
  mcp runs the MCP server over stdio (distill, score, style_review tools).
`)
}

func cmdDistill(args []string) error {
	fs := flag.NewFlagSet("distill", flag.ContinueOnError)
	corpus := fs.String("corpus", "", "directory of single-register .md/.txt documents")
	register := fs.String("register", "", "register name (e.g. long-form-design-doc)")
	id := fs.String("id", "", "profile id (defaults to register)")
	out := fs.String("out", "", "output profile path (defaults to <id>.profile.yaml)")
	language := fs.String("language", distill.DefaultLanguage, "corpus language code (only 'en' implemented)")
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

	prof, err := distill.Distill(idV, registerV, *language, docs, distill.DefaultOptions())
	if err != nil {
		return err
	}
	prof.Rules = judge.Mine(docs, judge.DefaultMinSupport)
	// Resolve inheritance once, after every field (including rules) is set, so the
	// saved profile carries the base invariants and matches what Load produces.
	if prof, err = stylespec.Resolve(prof, ""); err != nil {
		return err
	}
	if err := prof.Save(outV); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "distilled %d documents (%d words) -> %s\n", prof.Corpus.Documents, prof.Corpus.Words, outV)
	fmt.Fprintf(os.Stderr, "  %d features, %d preferred lexicon terms, %d deterministic rules\n", len(prof.Features), len(prof.Lexicon.Preferred), len(prof.Rules))
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
	prof, cal, err := discriminate.Calibrate(idV, *register, *language, targetDocs, decoyDocs, opts)
	if err != nil {
		return err
	}
	prof.Rules = judge.Mine(targetDocs, judge.DefaultMinSupport)
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

