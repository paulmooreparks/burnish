# Profiles

A style profile is a stylometric fingerprint of someone's writing: it is **user
data, not source**, and is **not committed** (this repo is public, and other users
will distill from genuinely private corpora). Profile YAML files are gitignored;
they live as local artifacts and are regenerated from a corpus by `distill`. Only
this manifest is committed, and it documents each profile so it is reproducible.
The raw corpus is not committed either.

## The corpus must be authentically human AND single-register

Two failure modes degrade a profile, both quietly:

1. **Authorship.** The corpus has to be writing the person actually wrote.
   AI-generated or AI-co-authored text is the *negative* class the discriminator
   exists to detect; putting it in the target corpus calibrates toward the voice
   we are moving away from. An early cut was distilled from design docs/PRDs that
   were mostly AI-written, then discarded and rebuilt from hand-written essays.
2. **Register.** Even all-authentic, all-Paul text averages into mush if it mixes
   registers (DESIGN section 4). A personal-essay profile must not be diluted with
   a business plan, video scripts, a job description, or code walk-throughs.

## paul-essays.profile.yaml

- **Register:** `personal-essay` — Paul's hand-written reflective/opinion essays.
- **Language:** `en`. **Avoided:** `—`, `--` (Paul's standing preference; per-profile).
- **Corpus:** 13 markdown essays, ~5.1K words of cleaned prose, curated (burnish-13)
  from the `.md` files under `~/OneDrive/Documents/parkscomputing.com/wwwroot/content`.
  The distiller ingests only `.md`/`.txt`, so that `.md` set (34 docs) was the raw
  pool; 13 were kept, 21 dropped.
  - **Kept:** next-train-to-bracknell, not-the-droid, vibe-coding,
    parks-laws-of-debugging, power-of-guitar, how-many-years-of-pizza,
    i-hate-screenshots-of-text, so-much-more-exotic, fixing-the-plumbing,
    learning-theory, on-travelling, just-spell-the-month, one-word-or-two.
  - **Dropped, contaminated/not-Paul:** my-closet-is-an-lru-cache (mostly AI,
    per Paul), accessibility-guidelines, using-ai, maize-summary, ui-engine.
  - **Dropped, off-register:** bizplan, six video scripts, agile-bridge-jd,
    fizzbuzz + two cache/sitenav code walk-throughs, cloudflared-service-fix,
    house-images, act-now-before-price-increase, my-llm-experience stub,
    tela-and-awan-saya (empty section headings).

### Reproduce

`scripts/build-essay-corpus.sh` holds the keep-list and per-drop rationale. It
copies the kept essays into `corpus/paul-essays/` (gitignored), stripping YAML
front matter, HTML tags, markdown links/images, and bare URLs so only prose
reaches the distiller. Then:

```
scripts/build-essay-corpus.sh                 # writes corpus/paul-essays/
burnish distill --corpus corpus/paul-essays --register personal-essay \
  --id paul-essays --avoid "—,--" --out profiles/paul-essays.profile.yaml
```

### What the curation bought

The pre-curation profile (all 34 `.md`, 27K words) had a lexicon dominated by the
off-register docs: `cache`, `docker`, `lru`, `infrastructure`, `eviction`,
`xferlang`, `runtime`, plus leaked front-matter fields (`commentsallowed`,
`lastmodified`) and base64 media ids. After curation the preferred lexicon is
genuine essay vocabulary: `bracknell`, `hotel`, `singapore`, `travel`/`travelling`,
`playing`, `spell`, `month`, `realized`, `perspective`. Same engine, clean corpus.

### Notes

- Front-matter/HTML/link stripping is done by the build script for this corpus.
  Doing it inside `distill` (so the MCP/CLI path benefits for any corpus) is a
  tracked follow-up.
- The `--` avoided-term check still matches Markdown horizontal rules / table
  separators, so scoring raw Markdown can report spurious hard violations; score
  cleaned prose, or treat that as the separate tracked fix.
