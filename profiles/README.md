# Profiles

Distilled style profiles. The profile YAML (statistics + lexicon) is committed;
the raw corpus is not (some sources are proprietary, and the profile is the
reusable artifact). Each profile documents its corpus here so it is reproducible.

## paul-longform.profile.yaml

- **Register:** `long-form-design-doc` — Paul's long-form technical design and PRD
  prose. Deliberately homogeneous: expository design writing only. Terser,
  list-heavy status/carryover docs and READMEs are a different register and were
  excluded; so were chat and customer-facing copy.
- **Language:** `en`.
- **Corpus:** 10 documents, ~54.7K words.
  - Architectural Principles (`~/OneDrive/Documents/Architectural Principles.md`)
  - Andoneer PRD Draft (`repos/Andoneer/docs/Andoneer PRD Draft.md`)
  - Travel Expense Companion PRD Draft v0.11 (GK Software, `~/OneDrive - GK Software SE/Documents/`)
  - Overt: `DESIGN.md`, `docs/why-overt.md`, `docs/concurrency.md`,
    `docs/closures.md`, `docs/osl.md`, `docs/ffi.md`
  - burnish: `DESIGN.md`

### Reproduce

Gather the sources above into one directory (kept out of git), then:

```
burnish distill --corpus <dir> --register long-form-design-doc \
  --id paul-longform --out profiles/paul-longform.profile.yaml
```

### Notes

- The corpus contains em-dashes (the hand-written sources predate the no-em-dash
  rule), so the distilled `em_dash_rate` mean is non-zero, but distill bakes the
  hard `max: 0` invariant regardless. That is the base-invariant override working
  as designed.
- The em-dash invariant aside, this is a single-register profile; do not score
  chat or status-doc drafts against it. Distill a separate profile per register.
