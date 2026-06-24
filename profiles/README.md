# Profiles

A style profile is a stylometric fingerprint of someone's writing: it is **user
data, not source**, and is **not committed** (this repo is public, and other users
will distill from genuinely private corpora). Profile YAML files are gitignored;
they live as local artifacts and are regenerated from a corpus by `distill`. Only
this manifest is committed, and it documents each profile so it is reproducible.
The raw corpus is not committed either.

## The corpus must be authentically human

The target corpus has to be writing the person actually wrote. AI-generated or
AI-co-authored text is the *negative* class the discriminator exists to detect;
putting it in the target corpus calibrates the profile toward the very voice we
are trying to move away from. A first cut of this profile was distilled from
design docs and PRDs that turned out to be mostly AI-written, which is exactly
that mistake. It was discarded and rebuilt from hand-written essays.

## paul-essays.profile.yaml

- **Register:** `personal-essay` — Paul's hand-written personal/opinion essays and
  articles from parkscomputing.com. Deliberately homogeneous: excludes tutorials
  and code walk-throughs, product/landing pages, the resume, a job description, a
  business plan, reference docs, and video scripts (all different registers).
- **Language:** `en`.
- **Corpus:** 25 essays, ~16K words, from
  `~/OneDrive/Documents/parkscomputing.com/wwwroot/content`.
  - HTML (body text extracted): barbecue-and-project-management,
    becoming-a-developer-overnight-in-only-five-years, buzzword-bucket,
    compute-magazine-archives, george-orwell-and-effective-coding, how-i-plan-my-day,
    it-is-okay-to-be-just-okay, master-foo-and-the-technical-recruiter, on-recruiting,
    personas-in-the-wild, scheduling-every-minute-revisited
  - Markdown: how-many-years-of-pizza, i-hate-screenshots-of-text, just-spell-the-month,
    learning-theory, my-closet-is-an-lru-cache, next-train-to-bracknell, not-the-droid,
    on-travelling, one-word-or-two, parks-laws-of-debugging, power-of-guitar,
    so-much-more-exotic, using-ai, vibe-coding

### Reproduce

HTML body text is extracted (tags stripped, headings/lists mapped to Markdown);
Markdown frontmatter and fenced code blocks are stripped. Gather the cleaned
essays into one directory (kept out of git), then:

```
burnish distill --corpus <dir> --register personal-essay \
  --id paul-essays --out profiles/paul-essays.profile.yaml
```

### What the signature captures

Versus the earlier AI-corpus attempt, the authentic voice is unmistakable:
first-person rate ~45 per 1000 words (the AI design docs sat near 2), contraction
rate ~14, longer sentences (~21 words), reading grade ~10. An authentic in-corpus
essay scores 0.12; an AI-written design doc 0.31; a generic-LLM draft 0.92.

### Known artifacts (to clean later)

- A few lexicon entries are extraction noise, not voice: `https` / `com` / `org`
  (visible URLs split on punctuation), `alt`, and `ive` (from "I've" losing its
  apostrophe). A URL-strip in extraction and the larger-baseline card would clean
  these.
- The naive `--` avoided-term check matches Markdown horizontal rules and table
  separators (`---`, `|---|`), so scoring raw Markdown can report many spurious
  hard violations. Tracked as a separate fix.
