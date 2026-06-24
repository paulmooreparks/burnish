# burnish

A tool for auto-editing LLM output into a target text style.

Point it at a corpus of writing; it distills that corpus into a structured
**style profile**, then measures how far any draft sits from that style. The
goal is to move style enforcement *out* of the generating model (where
in-context rules are quickly forgotten) into a separate, deterministic checker.

See [DESIGN.md](DESIGN.md) for the architecture and [CARRYOVER.md](CARRYOVER.md)
for current status.

## Status

Walking skeleton plus the MCP server. The deterministic, no-model engine is
built: distill a profile, score a draft, and serve all of it over MCP. What
ships today measures distance-to-style and hard-fails on banned constructs; it
does not yet rewrite drafts.

The **MCP server** (the primary agentic path) exposes `distill`, `score`, and a
`style_review` tool, and owns no model. burnish provides deterministic
measurement, calibration, and the scoring protocol; the *calling agent* (already
an LLM) renders the judgement and revision, in a fresh isolated context. A
built-in model adapter exists only as a headless fallback for agent-less callers.
The rule judge, exemplar retrieval, calibrated discriminator, massage loop, and
Claude Code hook are not built yet (`judge/`, `retrieve/`, `discriminate/`,
`enforce/`, `model/`, `pkg/api` are documented stubs); `style_review` therefore
returns the deterministic gap report with judgement marked not-yet-available.

## Build

```
go build ./cmd/burnish
```

Requires Go 1.25+ (the MCP SDK sets the floor). The binary is self-contained.

## Concepts

A **profile** is a YAML artifact distilled from one *register* (genre) of
writing: a set of weighted statistical **features** with target ranges, plus a
**lexicon** of characteristic and avoided vocabulary. Distillation is
per-register: feed a single distill run a genre-homogeneous corpus (all design
docs, or all chat, not a mix), or the averages turn to mush.

**Distance** is the headline number: the weighted mean deviation, in standard
deviations, of the draft's features that fall outside their target ranges. `0`
means every measured feature sits inside the corpus's range. **Hard violations**
(currently: any em-dash, present as the literal `—` or the `--` stand-in) make
`score` exit non-zero so it can gate a pipeline.

## Usage

### 1. Distill a profile from a corpus

```
burnish distill --corpus DIR --register NAME [--id ID] [--out FILE]
```

`--corpus` is a directory; every `.md` and `.txt` under it (recursively) becomes
one corpus document. `--register` names the genre. `--id` defaults to the
register; `--out` defaults to `<id>.profile.yaml`.

```
burnish distill \
  --corpus ./corpus/longform \
  --register long-form-design-doc \
  --id paul-longform \
  --out paul-longform.profile.yaml
```

```
distilled 5 documents (27536 words) -> paul-longform.profile.yaml
  49 features, 40 preferred lexicon terms
```

Under five documents, it warns that target ranges are low-confidence. A profile
is plain YAML: readable, diffable, hand-editable. Each feature carries its
corpus `mean`, `stddev`, target `min`/`max`, and a `weight` (stable features,
those with low corpus variance, are weighted higher).

### 2. Score a draft against the profile

```
burnish score --profile FILE [DRAFT]
```

Reads the draft from the file argument, or from stdin if omitted.

```
burnish score --profile paul-longform.profile.yaml draft.md
```

```
profile: paul-longform (register long-form-design-doc)
distance to style: 5.207 stddev (0 = within all target ranges)
HARD violations: 3

off-target features (worst first):
  hedge_rate           value=58.1  target=[0, 1.74]   84.66 stddev out  [soft]
  sentence_len_mean    value=28.7  target=[11.1, 14.1] 15.01 stddev out  [soft]
  em_dash_rate         value=23.3  target=[-inf, 0]     6.88 stddev out  [hard]
  ...

avoided terms present:
  "—" at byte 112
  "—" at byte 490
```

Exit code is non-zero when there are hard violations, so it composes in scripts:

```
burnish score --profile p.yaml draft.md || echo "off-style, blocked"
```

Add `--json` for machine-readable output (the same result as a JSON object with
`distance`, `features`, `lexical`, and `hard_violations` fields):

```
burnish score --profile p.yaml --json < draft.md
```

### 3. Use it agentically over MCP

```
burnish mcp
```

Runs an MCP server on stdio (built on the official Go MCP SDK), exposing three
tools to any MCP client:

- `distill` (`corpus_dir`, `register`, optional `language`/`id`/`out`) writes a
  profile and returns a summary.
- `score` (`profile_path`, `text`) returns the distance, off-target features, and
  hard-violation count as structured output.
- `style_review` (`profile_path`, `text`) returns the gap report plus the
  profile's preferred/avoided lexicon and rules as a revision payload. The
  judgement itself is left to the calling agent, which should render it in a
  fresh, isolated context (not the one that wrote the draft). The rule judge and
  calibrated discriminator are not built yet, so the payload marks judgement as
  not-yet-available.

Register it with Claude Code, e.g.:

```
claude mcp add burnish -- /path/to/burnish mcp
```

## What gets measured

- **Length & cadence:** sentence-length mean and variance, paragraph length,
  word length, Flesch-Kincaid reading grade, markdown heading/list cadence.
- **Punctuation rates** (per 1000 words): em-dash, semicolon, colon, parens,
  exclamation, question, comma.
- **Lexical class rates:** contractions, hedge words, first/second/third-person.
- **Function-word fingerprint:** per-word frequencies of 30 high-frequency
  function words, the classic authorship-attribution signal.
- **Lexicon:** distinctive vocabulary mined against a general-English baseline
  (preferred terms), plus a hand-seeded avoided list.

## Known limits

- Function-word rates are noisy on short drafts (a single word swings a per-1k
  rate by 20+ stddev at ~50 words). The signature is reliable at document scale.
- The lexicon baseline is a small seed list, so some mid-frequency English words
  read as distinctive.
- It measures and gates; it does not yet rewrite. Rewriting arrives with the
  massage loop (`enforce/`), which depends on the LLM judge and discriminator.

## Languages

The design is language-agnostic: segmentation, the feature set, and the lexicon
baseline are a per-language module selected by a profile's `language` field. The
core engine, profile format, scoring, and the LLM judge/discriminator are
language-neutral, and the LLM half works across languages with no extra code.
Today only the English (`en`) module is built; `distill --language X` and `score`
refuse a language with no module rather than mis-measure it. The text foundation
is Unicode-clean; correct word segmentation for non-spacing scripts (Chinese,
Japanese, Thai) awaits those modules. See DESIGN.md section 11.
