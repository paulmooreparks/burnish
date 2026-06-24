# bluepencil: Design

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

bluepencil's job is bigger than a banned-token linter. The goal: **point the tool
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
- Cross-register invariants (no em-dash, no "--" stand-in, no surveys,
  recommendation-first) live in a **base profile** that every register inherits and
  extends. Paul's existing CLAUDE.md style rules are mostly this base.

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

**Division of labor: bluepencil owns measurement, calibration, and protocol,
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
cmd/bluepencil stdin text -> exit code + JSON violations / score      (hooks exec this)
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
  execs `bluepencil` against the last assistant turn. Non-zero exit + the JSON
  violation list on stderr blocks the turn and feeds violations back, forcing a
  self-revise. MCP is *pull* (the agent must choose to call it, and may forget,
  which reintroduces the forgetting problem one level up at orchestration); the
  hook is *push* (it fires whether or not the agent remembers). Keep both; same
  engine behind each.
- **Go library**: `pkg/api` imported by Tela / planning.fit to wrap their own LLM
  calls.
- **`serve` mode**: same binary as an HTTP sidecar for .NET surfaces (GK Expense).

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
4. **Discriminator: calibrated judge first**, not a trained classifier. The
   calibration (held-out target + generic-LLM decoys + threshold) is deterministic
   engine work and stays in `discriminate/`; only the per-draft judgement needs a
   model. Upgrade path: fine-tune a small on-corpus classifier for cheap
   deterministic scoring once a corpus is large enough. *(Amended: the engine owns
   calibration and protocol, not the inference; see #5.)*
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
2. **[done]** `cmd/bluepencil score`: feed any text, get a distance-to-style
   number + which features are off, with a hard-violation exit gate.
3. **`mcp/` server** exposing `distill`, `score`, and a first `style_review` that
   returns the deterministic gap report (no judgement yet). This is the agentic
   surface and needs no model; it ships the value we already have to any MCP
   client immediately.
4. `discriminate/` **calibration**: hold out target doc text vs. generic-LLM
   decoys, compute the threshold, and emit the scoring rubric. Inference is the
   caller's (or the `model/` adapter for headless); the engine owns the protocol.
5. Rule mining + `judge/` payload (validated rules + evidence ask), wired into
   `style_review`.
6. Exemplar retrieval (`retrieve/`), then the full massage loop (`enforce/`),
   then the Stop hook (the enforcement guarantee), then the `model/` headless
   adapter and `serve` mode.

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
