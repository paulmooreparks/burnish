# Preparing a corpus

A burnish profile is only as good as the corpus it is distilled from. `distill`
measures style with no judgement of its own: feed it clean, single-**register**
prose authentic to the voice you are targeting and it produces a faithful
fingerprint; feed it a mix of genres and it faithfully fingerprints the mush. This
guide is the work you do **before** `distill`.

The "voice you are targeting" can be **one author** or **a whole body of work
written by many authors** (a house / consensus voice you want new output to blend
into). Both are first-class; the difference is only what counts as "belongs in this
corpus." See the README's Use cases section.

> Today this preparation is manual (a script you adapt). It is deliberately a
> visible, inspectable step, not hidden engine magic, because the two decisions
> that matter most (does this belong to the target voice? is it one register?) need
> a human eye. A future `burnish ingest` subcommand will formalize the mechanical
> part (raw files in, clean prose out) without taking those judgements away from
> you.

## The two failure modes

Both degrade a profile silently. Neither throws an error. Distillation still
produces a plausible-looking artifact.

### 1. Authorship (belonging)

The corpus must be writing that genuinely **belongs to the target voice**. For a
single-author profile that means text the author actually wrote; for a house /
consensus voice it means text that is genuinely part of the body of work you want
to blend into. Either way, burnish's discriminator exists to tell the target voice
apart from everything else, so any text that does not belong, AI-generated, a guest
post or a different author who is not part of the house, a quoted passage,
boilerplate, sits in the corpus calibrating the profile toward the very voice you
are trying to move away from.

This is subtle. In practice it has bitten twice on a single corpus: once from
AI-generated drafts mixed in with hand-written essays, and once from an essay that
read fine to an automated audit but the author immediately recognized as mostly
AI-written. **Belonging is a human call.** Automated classification is a first
pass, not the authority. (For a many-author house corpus the bar is "is this part
of our body of work?" rather than "did one specific person write it?", many authors
is expected; off-house or non-house-authored material is what to exclude.)

### 2. Register

Even all-authentic text averages into mush if it spans registers.
A reflective personal essay, a business plan, a job description, a spoken video
script, a how-to walkthrough, and a code tutorial are different kinds of writing
with different sentence shapes, cadences, and vocabularies. Distilling them
together produces a profile that matches none of them. Pick **one** register per
profile; build a separate profile for each register you care about (they can share
a base profile for cross-register invariants; see the main README).

## What `distill` actually reads

`distill` ingests `.md` and `.txt` files only, recursively, **raw**. It does not
parse HTML, strip front matter, or follow links. Whatever bytes are in those files
become the corpus, including YAML front-matter fields, HTML tags and their
attribute values, markdown link URLs, and embedded media ids. All of that leaks
into the metrics and the distinctiveness lexicon as bogus "characteristic"
vocabulary. So normalizing your sources to clean prose is your job.

## The ingest step

Think of corpus prep as one explicit step with a clear contract:

```
raw files (HTML, front-mattered .md, ...)  ->  clean prose .md  ->  inspect  ->  distill
```

The "inspect" is not optional. After extraction, read the cleaned files. Confirm
they are the author's voice, one register, and free of stray markup. Only then
distill.

## Extraction recipes

### Markdown with front matter / embedded markup

Strip, in order: the leading `--- ... ---` YAML front-matter block; whole
`<figure>...</figure>` blocks (caption text is not prose); HTML comments and
remaining HTML tags; markdown images `![alt](url)` (dropped whole); markdown links
`[text](url)` (keep the visible text, drop the URL); and bare URLs. Keep markdown
structure (`#` headings, `-` lists), since cadence features are measured from it.

When stripping HTML tags, match only real tag names and never let a "tag" span a
`<`. A naive `<[^>]*>` (or even `<letter[^>]*>`) will eat prose like `if x<y` or
`vector<int>` up to the next `>`. Restrict to a whitelist of HTML tag names and use
`[^<>]*` for the tag body so it cannot run across a `<` into the next real tag.

### HTML to text

The site essays here are hand-written and structurally simple (`<body>` with
`<h1>`/`<p>`/`<blockquote>`/`<a>`), so a structural extraction is enough; no HTML
parser dependency. Remove HTML comments and `<script>`/`<style>`/`<head>` blocks
**before** trimming to `<body>` (so a `<body>` token inside a script can't mis-cut
the document, and head content can't leak when `<body>` is absent). Then map
headings/paragraphs/lists to markdown structure, strip the remaining whitelisted
tags, decode HTML entities (named **and** numeric, decimal and hex, so accented
characters and smart quotes don't survive as `&#233;` noise), and drop source
indentation (a 4-space indent reads as a markdown code block).

If a file mixes the author's own prose with quoted/pastiche material (a quoted
koan, a long block quote), keep only the author's part. Cutting at the markup
boundary that introduces the quoted block (for example, the `<figure>` or
`<blockquote>` that starts it) is usually a robust trim.

## The reference implementation

[`scripts/build-essay-corpus.sh`](../scripts/build-essay-corpus.sh) does all of the
above for the `paul-essays` profile. Read it as a template:

- Its header is the **manifest**: the keep-list of files and, for everything
  dropped, the reason (off-register vs not-the-author/contaminated). This is the
  auditable record of the curation.
- `strip()` normalizes markdown; `html_extract()` extracts HTML; both use the
  whitelisted, non-spanning tag pattern described above.
- It mirrors (clears and rebuilds the destination), so editing the keep-list and
  re-running is safe.

Adapt it by pointing `SRC` at your content, replacing the keep-lists, and adjusting
the drop manifest to explain your choices.

```bash
scripts/build-essay-corpus.sh /path/to/your/content   # writes corpus/<name>/ (gitignored)
```

## Authorship + register checklist

Before distilling, for **each** document:

- [ ] Does it belong to the target voice? (For a single-author profile: the author
      wrote it. For a house voice: it is genuinely part of the body of work. Not AI,
      not a guest/outside author, not a quoted passage.)
- [ ] Is it the **one** register this profile targets? (Reflective essay vs how-to
      vs business doc vs spoken script vs code walk-through are different.)
- [ ] Is it complete enough to measure? (A two-sentence stub or a doc full of empty
      section headings skews cadence features.)
- [ ] Is it actually prose after extraction? (No leftover front-matter fields, tag
      soup, URLs, or media ids in the cleaned text.)

When in doubt, drop it and note why. A smaller clean corpus beats a larger
contaminated one. After building, sanity-check the result: the preferred lexicon in
the distilled profile should read like the author's vocabulary, not markup or
off-register jargon.

## Worked example: the `paul-essays` profile

1. **Source.** Personal essays under a website content tree, as a mix of `.md` and
   `.html`. The tree also holds a business plan, video scripts, a job description,
   code tutorials, page fragments, and a resume, all off-register.
2. **Audit.** Read every candidate. The dominant problem was register (most of the
   raw word count was non-essay), plus a few AI/contaminated pieces. Authorship
   calls were confirmed by the author (who caught an AI piece the automated audit
   had kept).
3. **Curate.** Keep only single-register personal essays: 13 markdown + 8 HTML
   (the HTML extracted to prose `.md`), about 11.7K words. Everything else dropped
   with a recorded reason.
4. **Distill.**

   ```bash
   scripts/build-essay-corpus.sh
   burnish distill --corpus corpus/paul-essays --register personal-essay \
     --id paul-essays --avoid "—,--" --out profiles/paul-essays.profile.yaml
   ```

5. **Sanity-check.** Before curation the lexicon was dominated by off-register
   terms (`cache`, `docker`, `infrastructure`) and leaked front-matter fields;
   after, it reads like the author's essays (`bracknell`, `hotel`, `singapore`,
   `travel`, `guitar`, `perspective`). Same engine, clean corpus.

Profiles and raw corpora are user data and stay out of git; only the build script
and this guide are committed. See [`profiles/README.md`](../profiles/README.md) for
the per-profile manifest.

## The future `burnish ingest`

The mechanical half of this (HTML extraction, front-matter and markup
normalization) is slated to become a `burnish ingest` subcommand: point it at raw
files, get clean prose `.md` you can inspect before distilling. The shape stays an
explicit, inspectable step on purpose, so the authorship and register judgements
above remain yours. Until then, the build script is the reference.
