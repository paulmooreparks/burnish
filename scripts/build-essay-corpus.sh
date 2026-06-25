#!/usr/bin/env bash
# Assemble the curated single-register personal-essay corpus for the "Paul"
# profile by copying a vetted keep-list out of the parkscomputing content tree
# into corpus/paul-essays/ (gitignored). distill ingests only .md/.txt, so the
# source set is the .md files under that tree; this script is the auditable record
# of which ones belong in a personal-essay profile and why the rest were dropped.
#
# Curation (burnish-13): of 34 .md docs, 13 are kept. The rest are dropped as
# off-register (the dominant defect: a personal-essay profile must not be averaged
# with business/spoken/code registers, DESIGN section 4) or as non-Paul/contaminated.
#
# DROPPED, author/contaminated:
#   my-closet-is-an-lru-cache (mostly AI-written, per Paul)
#   accessibility-guidelines (generic WCAG boilerplate, uses em-dashes)
#   using-ai               (literal (start ai)/(end ai) blocks + leftover prompt)
#   maize-summary, ui-engine (AI-style architecture/session summaries)
# DROPPED, off-register:
#   bizplan (business plan); agile-bridge-jd (job description);
#   video-scripts/01..05 + my-closet-is-an-lru-cache-video-script (spoken scripts);
#   fizzbuzz, drafts/xferlang-site-navigation,
#   drafts/set-associative-cache-in-c-part-3-implementation (code walkthroughs);
#   cloudflared-service-fix (ops note);
#   house-images, act-now-before-price-increase (image captions);
#   drafts/my-llm-experience (2-sentence stub);
#   tela-and-awan-saya (real voice but empty section headings skew heading cadence)
#
# Usage: scripts/build-essay-corpus.sh [SOURCE_CONTENT_DIR]
#   SOURCE_CONTENT_DIR defaults to Paul's local parkscomputing content tree.
#   Re-run after editing the keep-list; it mirrors, not appends (clears the dest).
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="${1:-$HOME/OneDrive/Documents/parkscomputing.com/wwwroot/content}"
DEST="$ROOT/corpus/paul-essays"

# The keep-list: Paul-authored, personal-essay register, complete enough to measure.
# NOTE: my-closet-is-an-lru-cache.md was excluded by Paul as mostly AI-written
# (a false keep the automated audit missed; authorship is the author's call).
KEEP="
next-train-to-bracknell.md
not-the-droid.md
vibe-coding.md
parks-laws-of-debugging.md
power-of-guitar.md
how-many-years-of-pizza.md
i-hate-screenshots-of-text.md
so-much-more-exotic.md
fixing-the-plumbing.md
learning-theory.md
on-travelling.md
just-spell-the-month.md
one-word-or-two.md
"

if ! command -v perl >/dev/null 2>&1; then
  echo "perl is required (used for markup stripping); install it or run under an environment that has it" >&2
  exit 1
fi
if [ ! -d "$SRC" ]; then
  echo "source content dir not found: $SRC" >&2
  echo "pass it as the first argument: scripts/build-essay-corpus.sh /path/to/content" >&2
  exit 1
fi

rm -rf "$DEST"
mkdir -p "$DEST"

# Normalize each essay to prose before copying: distill reads raw .md, so
# anything that is not running prose leaks into the corpus and surfaces as bogus
# "characteristic" vocabulary. We remove, in order:
#   - the leading YAML front-matter block (--- ... ---): field names like
#     commentsAllowed, lastModified, slug, hasAudio, audioFile;
#   - whole <figure>...</figure> blocks, including the figcaption text (image
#     captions are not essay prose and skew the lexicon);
#   - HTML comments and any remaining HTML tags (their attribute values carry
#     image paths/ids);
#   - markdown images ![alt](url): dropped whole;
#   - markdown links [text](url): kept as their visible text, URL discarded;
#   - bare URLs: dropped (they leaked posts, paulmooreparks, youtube, wikipedia,
#     and base64-ish video/image ids like jjbckfbd into the lexicon).
# The HTML-tag pattern requires a letter or slash right after "<", so prose
# comparisons ("a < b", "x > y") are left intact rather than swallowed. Markdown
# structure (# headings, - lists) is kept, since cadence features are measured
# from it.
strip() {
  awk 'NR==1 && $0=="---"{infm=1;next} infm && $0=="---"{infm=0;next} !infm' "$1" \
    | perl -0777 -pe '
        s{<figure\b[^>]*>.*?</figure>}{}gis;  # whole figure blocks (incl. captions)
        s/<!--.*?-->//gs;                     # HTML comments
        s{</?[a-zA-Z][^>]*>}{}g;              # HTML tags (must start with a letter/slash)
        s/!\[[^\]]*\]\([^)]*\)//g;            # markdown images -> drop
        s/\[([^\]]*)\]\([^)]*\)/$1/g;         # markdown links -> visible text
        s{https?://\S+}{}g;                   # bare URLs
      '
}

n=0 missing=0
for f in $KEEP; do
  if [ -f "$SRC/$f" ]; then
    strip "$SRC/$f" > "$DEST/$f"
    n=$((n+1))
  else
    echo "  MISSING: $f" >&2
    missing=$((missing+1))
  fi
done

words=$(cat "$DEST"/*.md 2>/dev/null | wc -w | tr -d ' ')
echo "curated $n essays into $DEST ($words words); $missing missing"
[ "$missing" -eq 0 ] || { echo "some keep-list files were missing from the source" >&2; exit 1; }
echo
echo "next: distill the curated corpus, e.g."
echo "  go run ./cmd/burnish distill --corpus \"$DEST\" --register personal-essay --id paul-essays --avoid \"—,--\" --out profiles/paul-essays.profile.yaml"
