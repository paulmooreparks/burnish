# burnish: Design

> A tool for auto-editing LLM output into a target text style.
>
> Status: design converged, no code yet. See [CARRYOVER.md](CARRYOVER.md) to resume.

Applies the cross-project [Architectural Principles](file:///C:/Users/paul/OneDrive/Documents/Architectural%20Principles.md) unless explicitly overridden here.

---

## 1. The problem

Style rules placed in an LLM's memory or instructions are quickly forgotten. The
structural reason: instructions in context are *priors on the next-token
distribution*, not enforcement. They bias generation, weighted against everything
else competing for attention, and as context grows the bias dilutes. Anything that
relies on the generating model "remembering" inherits this failure mode. Few-shot
examples help but are still soft.

burnish's job is bigger than a banned-token linter. The goal: **point the tool
at a repository of work (a blog, a knowledge base, a collection of customer-facing
documents) and have it distill that corpus into a style, then massage arbitrary LLM
output until it is nearly indistinguishable from the target style.**

## 2. Core reframes

Two reframes drive the whole design.

**A. "Indistinguishable" is a discriminator score, not a vibe.** Stop thinking
"set of rules," start thinking "distance to a target distribution." The clean
objective is a **discriminator**: a judge calibrated to answer "did this come from
the target corpus, or not?" You massage output until the discriminator can't tell,
i.e. until its confidence that the text is on-target crosses a threshold. This
converts an aesthetic goal ("sounds like us") into a number you can optimize
against, and gives the massage loop a real stopping condition. The shape is
adversarial (a generator proposes, a discriminator scores, you revise on the gap):
a GAN, but both halves are LLM/statistical and it runs at inference time, not
training time.

**B. Enforcement must live outside the generation step.** The reliable fix for the
forgetting problem is to move enforcement out of the generating model into a
separate process whose only job is to verify style, ideally in a *fresh context
window*. The same dilution that makes the generator forget is the reason an
isolated checker stays reliable. Deterministic checks (regex/stats) cannot forget
at all.

## 3. The style profile artifact ("tokenized rules")

Do not distill to a Markdown document. A doc is read once by the generator and then
forgotten: the exact failure mode. Distill instead to a **structured profile**: a
set of independently addressable, individually checkable, weighted style tokens.
The profile is compact, diffable, version-controlled, and composable; each token
plugs into a *different* enforcement mechanism. Four token types, each capturing
what the others cannot:

1. **Features (statistical / deterministic).** A stylometric signature computed
   over the corpus: sentence-length mean and variance, paragraph length,
   punctuation rates (em-dash, semicolon, parens, exclamation), passive-voice rate,
   reading grade, contraction rate, hedge-word rate, person distribution
   (1st/2nd/3rd), function-word frequencies (the classic authorship-attribution
   signal), list/heading cadence. Each becomes a metric with a target range and a
   weight. "Tokenized" in the most literal, most reliable sense: numbers, checkable
   with zero LLM calls, incapable of being forgotten.

2. **Rules (judged, but empirically validated).** An LLM induces candidate rules
   from corpus samples, then *each rule is validated back against the corpus* and
   kept only if it has high support (e.g. holds in >90% of paragraphs). Every rule
   carries its support stat and counterexamples. This kills the central risk of
   LLM rule-induction: enforcing hallucinated rules the corpus does not actually
   follow.

3. **Lexicon.** Characteristic vocabulary mined by distinctiveness (TF-IDF against
   a general-English baseline): preferred terms, avoided terms, signature
   phrasings. Deterministic to check.

4. **Exemplars (embeddings).** The corpus chunked and embedded. Captures the
   ineffable rhythm/voice that no rule articulates. At massage time, retrieve the
   most stylistically relevant exemplars as targeted few-shot. "Tokenize" in the
   embedding sense.

Plus profile-level metadata: a `register` field (see below), a `discriminator`
calibration set + threshold, and per-token weights.

### Profile schema (sketch, to be firmed up in code)

```yaml
profile:
  id: paul-longform
  register: long-form-design-doc      # see multi-register model
  inherits: paul-base                 # cross-register invariants
  features:
    - id: sentence-length-mean
      target: { min: 14, max: 26 }
      weight: 0.6
    - id: em-dash-rate
      target: { max: 0.0 }            # hard invariant from base
      weight: 1.0
  lexicon:
    preferred: [ ... ]
    avoided:   [ ... ]
  rules:
    - id: recommendation-first
      class: judged
      severity: soft
      statement: >
        Leads with a concrete recommendation, not a survey of options.
      support: 0.93                   # measured against corpus
      require_evidence: true
  exemplars:
    index: exemplars/paul-longform.vec
  discriminator:
    threshold: 0.82
    calibration: calib/paul-longform/
```

## 4. Multi-register model

Style is context-dependent. Paul's terse, decision-first *chat* voice is a
different style from his *PRD/design-doc* voice, which is different again from a
*customer-facing* voice. A single profile that averages them produces mush that
matches none. Therefore:

- Distillation is **per-register**. Each profile targets one genre; the corpus fed
  to `distill/` for a given profile must be genre-homogeneous.
- The profile carries a `register` field. The massage step selects the profile by
  the kind of output being checked.
- An author's cross-register invariants (their own no-em-dash rule, say) live in a
  **base profile** that their register profiles inherit and extend. This is
  per-author: burnish bakes **no universal invariants**, because it is a tool for
  anyone's style and must not impose one author's taste (many writers use em-dashes
  well). Paul's existing CLAUDE.md style rules are *his* base, not everyone's.

**Invariants are per-profile, never universal.** Two sources, both opt-in:
(1) avoided terms set on a profile via `distill --avoid "—,--"` (the author's
choice; they become the profile's hard avoided-lexicon); (2) a shared base profile
**file** inherited via `inherits` for cross-register invariants. `stylespec.Resolve`
merges the base (a file path) at **load time** (with `distill` pre-resolving in
memory after rules are mined), so every consumer sees one resolved profile and the
merge is idempotent; the base **wins** on conflict so a register cannot relax an
invariant the author set. The em-dash rate is otherwise just a normal statistical
feature (soft distance), not a hard rule, unless the author avoids it. There is no
built-in opinionated base.

### Single-author vs consensus (house) voice

The corpus does not have to be one author. Two modes, both first-class:

- **Single author.** The corpus is one person's writing; the profile captures how
  *they* write.
- **Consensus / house voice.** The corpus is a body of work by **many authors** in
  one register (a documentation set, a knowledge base, a customer-facing corpus).
  The goal is to make new output read as if written by yet another author who fits
  the existing body, for example customer-facing documents for a software system
  being customized for one customer, each blending into the larger set.

This drops out of the same machinery with no special mode. Each feature's target
range is the corpus mean plus or minus a multiple of the corpus stddev over the
corpus documents; with many authors the per-feature stddev grows, so the ranges
widen, and that widening **is** the house envelope: the mean is the
consensus central tendency, the spread is how much the house legitimately varies. A
draft is on-target when it lands inside that envelope, so "fit in" is the literal
acceptance condition. The discriminator (section 2A) is already a membership test
("did this come from the target corpus?"), which for a body of work means "is this
plausibly one of ours?". The lexicon and judged rules capture the conventions the
authors **share** (house terminology, preferred forms, structural habits);
individual quirks wash out, which is exactly the goal.

Two practical consequences:

- **Many authors is fine; many registers is not.** The single-register rule is the
  only hard corpus constraint. A documentation set spanning distinct document types
  (install guide vs API reference vs release notes) is multiple registers: distill
  each as its own profile sharing a base, do not lump them.
- **Calibrate with the right decoys.** For a house voice, calibrate (section 5) with
  target = the body of work and decoys = off-house text (generic-LLM prose, a
  different product's docs, or a different register). The reported AUC then measures
  whether the house has a learnable signature at all; a low AUC honestly says the
  corpus is too heterogeneous to enforce against, rather than manufacturing a
  crispness that is not there.

Fitting a house voice is generally **more forgiving** than cloning one author
(section 10): you are matching a region, not a single point.

## 5. The two pipelines

### Distill (offline): corpus -> profile

```
corpus -> feature extractor        -> features[]
       -> rule miner + validator   -> rules[] (with support stats)
       -> distinctiveness miner    -> lexicon
       -> chunk + embed            -> exemplars
       -> discriminator calibrator -> threshold + held-out target set
```

Calibration detail that matters: hold out part of the target corpus and calibrate
the discriminator against *that* plus off-style decoys, so "indistinguishable"
means indistinguishable from target text the discriminator was never told was
target. Otherwise the score is circular. For the "your own writing" target, the
negative/decoy class is conveniently *generic default-LLM voice*: the exact thing
the tool exists to move away from.

### Massage (online): draft -> conformant text

```
draft -> feature scorer (cheap, deterministic)   -]
      -> lexicon check                             |-> gap report
      -> rule judge (validated rules only)        -]
      -> if off: revise w/ targeted feedback + retrieved exemplars
      -> discriminator gate: on-target >= threshold?
      -> repeat until pass or budget exhausted (N = 2-3)
```

Deterministic feature/lexicon checks run first and free; the LLM only sees what is
left. The discriminator is the final acceptance gate. `hard` violations block;
`soft` violations warn. Bound the loop at N = 2-3 iterations to prevent thrashing,
then hard-fail or warn by severity.

## 6. Engine architecture

A standalone Go module. One artifact serves every surface: a single static binary
that exposes an MCP server (the primary agentic surface), runs as a Claude Code
Stop hook with zero runtime deps, is importable as a package for Go apps (Tela,
planning.fit), and runs as a `serve` HTTP sidecar for .NET surfaces (GK Expense).

**Division of labor: burnish owns measurement, calibration, and protocol,
never the inference, except as a fallback adapter.** The deterministic checks
(features, lexicon) need no model at all. The judgement steps (subjective rules,
the discriminator) need an LLM, but that LLM is *the caller's* in the agentic
path: the engine returns the gap report, the corpus-validated rules, retrieved
exemplars, and a calibrated scoring rubric, and the calling agent renders the
judgement and the revision. The engine packages `judge/` and `discriminate/`
build the payload and own the protocol and calibration; they do not bake in a
model call. A built-in model adapter exists only for headless, agent-less callers
(see section 7).

```
distill/       corpus -> style profile                              (offline)
stylespec/     parse/load profile -> typed model
lint/          deterministic checks: features + lexicon -> violations w/ spans
judge/         validated rules -> judging payload (rules + evidence ask) for caller
retrieve/      exemplar embedding + nearest-style retrieval
discriminate/  calibration (held-out + decoys + threshold) + scoring rubric/protocol
enforce/       the massage loop: lint -> judge -> retrieve -> discriminate -> revise|fail
model/         optional inference adapter for headless callers (configurable; Haiku default)
mcp/           MCP server: distill, score, style_review tools         (agents call this)
cmd/burnish stdin text -> exit code + JSON violations / score      (hooks exec this)
pkg/api        Check(ctx, text, profile) (Result, error)             (apps import this)
```

### Token type -> enforcement mechanism map

| Profile token | Enforcement mechanism          | Inference run by      | Package        |
|---------------|--------------------------------|-----------------------|----------------|
| Features      | deterministic check            | none                  | `lint/`        |
| Lexicon       | deterministic check            | none                  | `lint/`        |
| Rules         | judging payload, isolated      | caller's LLM (or adapter) | `judge/`   |
| Exemplars     | retrieval-augmented revise     | caller's LLM          | `retrieve/`    |
| Discriminator | calibrated rubric + gate       | caller's LLM (or adapter) | `discriminate/`|

## 7. Delivery surfaces

A skill alone will not fix the forgetting problem: a skill that is just rules is the
same soft instruction that already fails. Reliability comes from a tool/hook that
runs deterministic code. Four front ends over the one engine, two of which carry
the load:

- **MCP server** (primary agentic surface): exposes `distill`, `score`, and
  `style_review` as tools. `style_review` returns the deterministic gap report,
  the corpus-validated rules, retrieved exemplars, and the calibrated scoring
  rubric; the calling agent renders the judgement and revision. This is the
  agentic path, and it owns no model: the agent is already the LLM. A tool result
  is enforcement *outside* generation (section 2B): the deterministic linter
  cannot forget, and the result re-injects violations as a hard structured signal
  at check time, not as a soft prior buried in instructions.
- **Claude Code Stop hook** (the enforcement guarantee, complementary to MCP):
  execs `burnish` against the last assistant turn. Non-zero exit + the JSON
  violation list on stderr blocks the turn and feeds violations back, forcing a
  self-revise. MCP is *pull* (the agent must choose to call it, and may forget,
  which reintroduces the forgetting problem one level up at orchestration); the
  hook is *push* (it fires whether or not the agent remembers). Keep both; same
  engine behind each.
- **Go library**: `pkg/api` imported by Tela / planning.fit to wrap their own LLM
  calls.
- **`serve` mode**: same binary as an HTTP sidecar for .NET surfaces (GK Expense).
  Its REST API **must be Fielding-style** (HATEOAS, hypermedia-driven, illegal
  actions absent from the representation), per the cross-project Architectural
  Principles, wherever practical. Code review of the `serve` API is required to
  enforce this: a non-hypermedia / RPC-over-HTTP design is a review failure.

### Two constraints the agentic path must honor

Moving the inference to the caller is right, but it must not quietly discard what
the isolated judge was buying:

1. **Isolation (no self-grading).** The agent that wrote a draft must not judge
   "is this on-target?" in the same context: that is the fox guarding the
   henhouse, biased toward passing its own output and polluted by the generation
   task. The discriminator judgement routes to a *fresh* context: a separate
   subagent or a separate invocation. MCP enables this; the design requires it.
2. **Calibration (a score, not a vibe).** "Does this sound like us?" stays a
   calibrated number: scored against held-out target text plus generic-LLM decoys,
   with a threshold (section 5). That harness is deterministic engine work and
   lives in `discriminate/` regardless of who runs the model. The caller renders
   the judgement; the engine owns the protocol and the threshold.

### Headless fallback

For agent-less callers (the .NET sidecar, CI), there is no orchestrating LLM to do
the cognition. There, an optional built-in `model/` adapter (configurable, Haiku
4.5 default) runs the judging and discriminator inference so the tool is
self-sufficient. This is a fallback, not the default path.

## 8. Key decisions (settled)

1. **Language: Go.** Single static binary for the hook + importable package for the
   Go apps; .NET surfaces call the same binary via subprocess/HTTP.
2. **Engine-first.** Build the spec + linter + judge + loop engine once as a
   library, expose through hook and callable. (Chosen over building per-surface.)
3. **Both layers from the start.** Deterministic linter *and* isolated LLM judge;
   the rule mix is an even split of mechanical and subjective.
4. **Discriminator: calibrated *deterministic distance threshold* first** (built),
   not an LLM judge and not even a trained classifier. The calibration (held-out
   target + decoys + Youden-J threshold + AUC) is pure engine work in
   `discriminate/`; the score is the distance `lint` already computes, so the
   runtime gate needs no model at all. This inverts the original "LLM judge first"
   plan and fits the deterministic-first thesis. Upgrade path: a richer score (a
   trained on-corpus classifier, then the calibrated LLM judge) reusing the same
   held-out-vs-decoys protocol; only that per-draft judgement needs a model, and it
   runs in the caller (#5).
5. **Inference runs in the caller, not the binary.** *(Supersedes the original
   "bake in Haiku 4.5" decision.)* In the agentic path the calling agent is
   already a capable LLM; having the tool shell out to a second, weaker model for
   judgement is worse cognition and couples the binary to an API key, model
   pinning, network, retries, and cost accounting. So `judge/` and `discriminate/`
   return a judging payload + scoring rubric and the caller renders the judgement,
   in a *fresh isolated context* (section 7 constraint 1). A built-in `model/`
   adapter (configurable, Haiku 4.5 default) exists only as the headless fallback
   for agent-less callers. `require_evidence` (#7) still binds whoever judges.
6. **Deterministic-first ordering.** Anything regex/AST/stat-detectable never
   reaches an LLM. Free, instant, 100% recall.
7. **Judge must quote evidence** (`require_evidence`) for every judged violation:
   kills false positives, makes revision actionable.
8. **Auto-fix only mechanical-and-safe.** Em-dash -> comma is semantically risky,
   so `flag`, not `replace`. Reserve `replace` for unambiguous rewrites
   (trailing whitespace, etc.).
9. **Loop bound N = 2-3**, then hard-fail or warn by severity.
10. **Multi-register profiles** with a shared base profile for cross-register
    invariants.
11. **First grounding corpus: Paul's own writing**, long-form register first.

## 9. Build order (measurement before articulation)

You can measure distance-to-style before you can articulate a single rule, so the
deterministic skeleton comes first.

1. **[done]** `distill/` feature extractor + distinctiveness miner over the
   design-doc corpus -> a `features` + `lexicon` profile; `internal/text/`
   segmenter; `stylespec/` profile + YAML.
2. **[done]** `cmd/burnish score`: feed any text, get a distance-to-style
   number + which features are off, with a hard-violation exit gate.
3. **[done]** **`mcp/` server** (stdio, official go-sdk) exposing `distill`,
   `score`, and a first `style_review` that returns the deterministic gap report
   plus the profile's lexicon/rules as a revision payload, with judgement marked
   not-yet-available. The agentic surface; needs no model. Run via
   `burnish mcp`.
4. **[done]** `discriminate/` **calibration**, first cut as a *deterministic
   distance threshold* (no model): split target into train/holdout, build the
   profile from train, score held-out target vs decoys, derive a Youden-J
   threshold + AUC/TPR/FPR, and ship the train profile carrying the gate (same
   distance scale the threshold was measured on, so the reported FPR describes the
   shipped artifact). A non-separating corpus is flagged, not silently shipped.
   `burnish calibrate`; the on-target verdict surfaces in `score` and
   `style_review`. First measured result: authentic-essay vs AI-technical-prose
   AUC ~0.80. The calibrated LLM judge is the upgrade, reusing this same
   held-out-vs-decoys protocol with the caller's inference.
5. **[done]** Rule mining (`judge/`). Deterministic: per-instance structural
   rules (max sentence/paragraph length) with corpus-validated support, flagged
   against a draft. LLM-judged (subjective): `InductionPrompt` proposes candidate
   style rules from corpus samples (the caller's LLM renders it; validated against
   the corpus, kept with high support), stored as `class: judged` and merged into
   a profile via `distill --rules-file`; `JudgingPrompt` evaluates a draft against
   them with required evidence. style_review hands the agent the judging payload.
   Both bake no model. (A first set was induced for the essay corpus in-session.)
6. **[done, first cut]** Exemplar retrieval (`retrieve/`), deterministic: a
   TF-IDF cosine bank over corpus chunks (`burnish retrieve`), returning
   topically-relevant authentic-style passages as few-shot, no embedding model.
   Dense semantic embeddings are the upgrade.
7. **[done]** The massage loop (`enforce/`): `Massage` runs
   lint -> judge -> retrieve -> discriminate -> revise, bounded at N, with the
   revise step an injected `Reviser` (the caller's LLM; the agentic path can drive
   the same loop via the MCP `style_review` tool). Subjective judged rules are an
   optional injected `Judge` hook (same caller-inference model as `Reviser`):
   failing verdicts fold into the brief with quoted evidence and a hard one blocks
   acceptance; nil `Judge` is deterministic-only. Acceptance = no hard violations
   (lint or rule) and on-target. Exposed via `pkg/api`. Demonstrated: one revision
   drove a generic draft 0.48 -> 0.29 toward Paul's essay voice.
8. **[done]** The Claude Code **Stop hook** (`burnish hook`): the push-enforcement
   guarantee. Reads the stop payload, extracts the last top-level assistant turn
   (skipping subagent sidechains), and blocks the stop on hard violations so Claude
   revises. Hard-invariants-only against the base by default (zero config), fails
   open.
9. **[done, build + mock]** The headless fallback: `model/` adapter + `serve`
   mode. `model/` is a stdlib-only Anthropic Messages client (injectable HTTP
   doer, no SDK dependency) exposing a `Reviser` for the massage loop and a
   `JudgeRules` verdict call; the engine still bakes no model (decision #5), and
   `model/` is constructed only by `serve`/CLI. `serve` is a Fielding-style
   (HATEOAS) HTTP REST API over the engine: hypermedia representations with
   `_links`, link-driven reachability from `/`, and the massage action absent
   from a profile unless a reviser is configured. Tested entirely against a
   mocked transport + stub reviser; no live API call. (Live smoke test since
   passed; judged-rules-in-the-massage-loop wiring landed via the `enforce.Judge`
   hook, with `model.JudgeRules` as the headless judge.)

Steps 1-2 (the deterministic walking skeleton) are already useful standalone:
point `score` at any draft and see how far from the target voice it sits.

## 10. Honest limits

Surface and lexical style (features + lexicon + validated rules) are highly
learnable; output will get genuinely close. Deep authorial voice (argument
structure, humor, rhythm) is partly captured by exemplars + discriminator but
asymptotic: the discriminator gives you a distance and you drive it down, but
expect a plateau where the last increment of "indistinguishable" costs a lot. Frame
it as "measurably close and improving," not "perfect clone." The discriminator at
least reports honestly where you are on that curve.

## 11. Multi-language

burnish must support any language, not only English. The architecture is built
so that adding a language touches one replaceable part and nothing else.

**Language-neutral core (unchanged per language):** the engine, the profile
format, the `lint` scoring math (weighted deviation in stddev units over whatever
metrics the profile declares), the `judge` protocol, the `discriminate`
calibration and gate, the `enforce` massage loop, and the MCP surface. None of
these know or care what language the text is in: they operate on a named metric
map and a profile, both language-tagged.

**Language module (the one replaceable part):** segmentation (sentence and word
boundaries), the statistical feature set (which metrics are computed and how),
the lexicon baseline, and the readability formula. The metric-extraction code in
`distill/` today *is* the English module. Adding French or Chinese means
registering a new module; the core above does not change.

Three things make this clean:

1. **Metric ids are a per-language contract, not a fixed list.** Features are an
   open `map[string]float64`. A language module contributes whatever metrics make
   sense for it, and `lint` compares same-id metrics between the profile and the
   draft. A space-delimited language measures words-per-sentence; a Chinese or
   Japanese module would measure characters-per-sentence and skip the
   syllable-based readability grade entirely, contributing its own metrics
   instead. The scoring layer is indifferent.

2. **A profile carries its `language`, and a draft is scored with the module that
   distilled the profile.** Scoring text with a different module than built the
   profile yields incomparable numbers, so `distill` and `lint` refuse a language
   with no module rather than emit or score an English-measured artifact
   mislabeled as another language. (Today only `en` is registered; an unsupported
   language is an explicit error, not silent garbage.)

3. **The LLM half generalizes across languages for free.** The `judge` and the
   `discriminate` steps are rendered by a multilingual LLM (the caller's, per
   section 7): "does this sound like the corpus?" needs no per-language code. So a
   new language gets the adversarial discriminator and rule-judging immediately;
   only the cheap deterministic feature layer waits on its module. This inverts
   the usual cost: the hard, subjective half is already multilingual; the easy,
   mechanical half is what needs porting.

**Foundations already laid:** the segmenter uses Unicode letter/mark classes
(`\p{L}\p{M}`), not `[A-Za-z]`, and recognizes non-Latin sentence terminators, so
no script is silently dropped at tokenization. Correct *word* segmentation for
non-spacing scripts (Chinese, Japanese, Thai) is a known gap deferred to those
modules; the foundation is Unicode-clean, not yet CJK-correct.

**Still English-centric, to revisit per language:** the em-dash invariant and its
`--` stand-in are a Latin-script, Paul-specific base rule; base profiles will
themselves be per-language. The function-word fingerprint, hedge/pronoun sets,
contraction handling, and Flesch-Kincaid are all the English module's content.
