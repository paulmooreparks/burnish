// Command bluepencil is the CLI front end over the bluepencil engine. It exposes:
//
//	distill  build a style profile from a single-register corpus
//	score    measure a draft's distance from a profile's target style
//	mcp       run the MCP server (stdio) exposing distill/score/style_review
//
// The judge, retrieval, discriminator, and full massage loop (and the Claude
// Code Stop hook that execs this binary) are later increments. See DESIGN.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/paulmooreparks/bluepencil/distill"
	"github.com/paulmooreparks/bluepencil/lint"
	bpmcp "github.com/paulmooreparks/bluepencil/mcp"
	"github.com/paulmooreparks/bluepencil/stylespec"
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
	case "mcp":
		err = bpmcp.Serve(context.Background())
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "bluepencil: unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bluepencil: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `bluepencil - distill a writing style and measure drafts against it

usage:
  bluepencil distill --corpus DIR --register NAME [--language en] [--id ID] [--out FILE]
  bluepencil score   --profile FILE [FILE]
  bluepencil mcp

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
	if err := prof.Save(outV); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "distilled %d documents (%d words) -> %s\n", prof.Corpus.Documents, prof.Corpus.Words, outV)
	fmt.Fprintf(os.Stderr, "  %d features, %d preferred lexicon terms\n", len(prof.Features), len(prof.Lexicon.Preferred))
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
		return enc.Encode(res)
	}
	printResult(prof, res)
	if res.HardViolations > 0 {
		os.Exit(1)
	}
	return nil
}

func printResult(p *stylespec.Profile, res lint.Result) {
	fmt.Printf("profile: %s (register %s)\n", p.ID, p.Register)
	fmt.Printf("distance to style: %.3f stddev (0 = within all target ranges)\n", res.Distance)
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

