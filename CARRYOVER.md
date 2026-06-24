# burnish: Carryover

> Resume context. Read this first, then [DESIGN.md](DESIGN.md).
> Last updated: 2026-06-24 (walking skeleton built).

## What this is

A tool that distills a corpus of writing into a structured **style profile**, then
massages arbitrary LLM output until it is nearly indistinguishable from that target
style. Born from a recurring problem: style rules placed in an LLM's memory or
instructions are quickly forgotten, because in-context instructions only *bias*
generation and dilute as context grows. burnish moves style enforcement *out* of
the generating model into a separate, deterministic-plus-adversarial checker.

## Where we are

- **Design: converged.** Full architecture in [DESIGN.md](DESIGN.md).
- **Code: deterministic engine + MCP server built, committed.** Module
  `github.com/paulmooreparks/burnish`, Go 1.25 (auto-upgraded by the MCP SDK).
  The no-model core of distill -> score -> serve is implemented, vetted, tested:
  - `internal/text/` deterministic Unicode-aware segmenter.
  - `distill/` stylometric feature extractor (~49 metrics incl. function-word
    fingerprint) + Zipf-baseline distinctiveness lexicon miner + corpus->Profile;
    per-language registry guard (`lang.go`).
  - `stylespec/` Profile types (+ `language` field) + YAML load/save.
  - `lint/` deterministic scorer: weighted distance-to-style (in stddev units) +
    per-feature off-target list + avoided-term spans + hard/soft severity.
  - `mcp/` MCP server (official go-sdk, stdio) exposing `distill`, `score`,
    `style_review`; integration-tested via in-memory transport + real stdio.
  - `cmd/burnish` CLI: `distill`, `score`, `mcp` subcommands.
  - `judge/ retrieve/ discriminate/ enforce/ model/ pkg/api` documented stubs.
- **Smoke-tested end to end.** Distilled a 5-doc long-form profile (Arch
  Principles + Overt/Andoneer/burnish design docs). Generic-LLM draft scored
  5.21 with 3 hard violations (em-dash + 84-sigma hedge rate); Paul-style draft
  scored 1.83 with zero hard violations. The score cleanly separates the two and
  the hard gate fires correctly.
- Em-dash invariant works as designed: the corpus itself contains em-dashes
  (16.7/1k, since Paul's hand docs predate the rule), but distill bakes the hard
  max-0 invariant regardless, overriding the corpus. Good proof of the
  base-invariant concept.

## The shape, in three sentences

1. "Indistinguishable" becomes a **discriminator score** (a calibrated judge:
   "did this come from the target corpus or not?"), turning an aesthetic goal into
   a number to optimize against.
2. Style is distilled not into a Markdown blob (which the generator forgets) but
   into a **tokenized profile**: statistical Features + validated Rules + Lexicon +
   embedded Exemplars, each token wired to a different enforcement mechanism.
3. A standalone **Go engine** runs an offline distill pipeline (corpus -> profile)
   and an online massage loop (draft -> conformant text). The engine owns
   measurement, calibration, and protocol but **not the inference**: the primary
   agentic surface is an **MCP server** whose `style_review` hands the calling
   agent a gap report + rules + exemplars + scoring rubric, and the agent (already
   an LLM) judges and revises. A Claude Code Stop hook is the complementary
   push-enforcement guarantee; a built-in model adapter is only a headless
   fallback.

## Settled decisions (do not re-litigate)

1. Language: **Go** (single binary hook + importable lib; .NET calls via
   subprocess/HTTP).
2. **Engine-first**: build the engine once, expose via multiple front ends.
3. Both deterministic linter **and** isolated LLM judge from the start.
4. Discriminator: **calibrated judge first**; trained classifier as later
   upgrade. Calibration (held-out + decoys + threshold) is deterministic engine
   work in `discriminate/`; only the per-draft judgement needs a model.
5. **Inference runs in the caller, not the binary** *(supersedes "bake in Haiku
   4.5")*. The agentic path is the **MCP server**: `judge/`/`discriminate/` return
   a payload + rubric, the caller's LLM judges/revises in a **fresh isolated
   context** (never grading its own draft), calibration stays in the engine. A
   built-in `model/` adapter (configurable, Haiku 4.5 default) is the headless
   fallback only. MCP (pull) + Stop hook (push guarantee) are complementary, not
   substitutes. `require_evidence` binds whoever judges.
6. Deterministic-first ordering; judged rules must quote evidence.
7. Auto-fix only mechanical-and-safe; em-dash is `flag`, not `replace`.
8. Massage loop bound N = 2-3.
9. **Multi-register** profiles over a shared base profile of cross-register
   invariants (the existing CLAUDE.md rules are mostly the base).
10. First grounding corpus: **Paul's own writing**, long-form register first.
11. **Any language, not just English** (DESIGN section 11). Core (engine, profile
    format, lint math, judge protocol, discriminator, massage loop, MCP) is
    language-neutral; segmentation + feature set + lexicon baseline + readability
    are a per-language **module** selected by the profile's `language`. The LLM
    half (judge/discriminator) is multilingual for free; only the deterministic
    feature layer needs per-language porting. Profiles record `language`;
    distill/lint **refuse** an unimplemented language rather than mis-measure.
    Foundation is Unicode-clean (`\p{L}\p{M}`); CJK word segmentation deferred to
    its module. English module is the only one built.

## Multi-register reminder

Do not build a single global profile. Paul's terse chat voice, his PRD/design-doc
voice, and a customer-facing voice are distinct registers; averaging them produces
mush. Distill per-register over a genre-homogeneous corpus; share a base profile
for invariants.

## Next actions (in order)

1. **Commit increment 1** (not yet committed). Then assemble a *real*
   single-register corpus: the 5-doc smoke set was convenience, not curation.
   Gather Paul's long-form register deliberately (Architectural Principles,
   Overt DESIGN/CARRYOVER, Andoneer PRD, GK Expense PRD, planning.fit design
   docs), confirm register-homogeneity, and re-distill. Thin corpora yield
   low-confidence ranges (distill warns under 5 docs).
2. **[done] `mcp/`: the MCP server** (DESIGN section 9 step 3). Built on the
   official go-sdk over stdio; `distill`/`score` fully wired, `style_review`
   returns the deterministic gap report + lexicon/rules payload with judgement
   marked not-yet-available. Run via `burnish mcp`.
3. **[done] Authentic corpus curated** (board card burnish-1). The target corpus
   must be authentically HUMAN; AI-generated text is the decoy class and the first
   cut (AI-written design docs/PRDs) was discarded. Rebuilt from 25 hand-written
   personal essays at `~/OneDrive/Documents/parkscomputing.com/wwwroot/content`
   (register `personal-essay`, ~16K words). Profile is **user data**: gitignored
   (`*.profile.yaml`), kept local, regenerated by `distill`; only
   `profiles/README.md` is committed. NOTE: repo is **public**; profiles must
   never be committed. Local history was rewritten to scrub a previously-committed
   profile (nothing was pushed), so publishing needs a force-push.
4. **`discriminate/`: calibration** (DESIGN section 2, 5) is the next code step
   (board card burnish-2, priority now). Hold out target text + generic-LLM
   decoys, compute the threshold, emit the scoring rubric. Inference is the
   **caller's** LLM (the agent), in a fresh isolated context, *not* a baked Haiku
   call. Wire the rubric + held-out exemplars into `style_review`. Open decision
   to surface at Spec: where the calibration-time judging inference comes from
   (the agent scores the calibration set, vs. a one-off adapter call) to find the
   threshold without a baked model in the hot path.
4. Then, in DESIGN section 9 order: rule mining + `judge/` payload, exemplar
   retrieval (`retrieve/`), full massage loop (`enforce/`), the Claude Code Stop
   hook (push guarantee), and finally the `model/` headless adapter + `serve`.

## Known limits in the current skeleton (address as they bite)

- **Function-word metrics are noisy on short drafts.** Per-1k-word rates
  quantize hard below ~document scale: one "an" in a 50-word draft reads as 20+
  stddev out. The signature is reliable at document scale, jittery at paragraph
  scale. Options when it matters: length-gate the fw.* features, or report a
  trimmed distance that drops sub-1-stddev noise. Not yet done.
- **Lexicon baseline is a small seed list** (~190 words, Zipf-modeled). Mid-
  frequency English words absent from it can score as distinctive. minDocs/
  minCount floors + the 3-char filter blunt it; a larger embedded baseline is
  the real fix (tracked).
- **Avoided-lexicon is hand-seeded** (em-dash + "--" only). Real avoided-term
  mining needs the LLM-voice decoy corpus, which lands with `discriminate/`.
- **base/inherit is faked.** The em-dash invariant is baked inline in distill
  rather than living in a shared base profile. Implement real inheritance when
  `stylespec/` gains a merge step (parked open question).

## Open questions parked for later

- Embedding model + vector store for the exemplar bank (`retrieve/`).
- Concrete profile file format (YAML sketched in DESIGN.md section 3; firm up when
  `stylespec/` is written).
- Whether the base/inherited-profile merge is static (at load) or layered (at
  check time).
- Threshold-tuning UX: how Paul inspects and adjusts feature weights and the
  discriminator threshold.
