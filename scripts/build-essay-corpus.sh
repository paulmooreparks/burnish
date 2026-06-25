#!/usr/bin/env bash
# Assemble the curated single-register personal-essay corpus for the "Paul"
# profile out of the parkscomputing content tree into corpus/paul-essays/
# (gitignored). It pulls a vetted keep-list of markdown essays AND extracts the
# prose from a vetted keep-list of HTML essays (distill can't read .html, so we
# convert them to .md here). This script is the auditable record of which docs
# belong in a personal-essay profile and why the rest were dropped, and it doubles
# as the reference implementation for the corpus-prep docs (see burnish-15).
#
# Curation (burnish-13): of 34 .md docs, 13 are kept. (burnish-16): 8 HTML essays
# are extracted and added. The rest are dropped as off-register (the dominant
# defect: a personal-essay profile must not be averaged with business/spoken/code
# registers, DESIGN section 4) or as non-Paul/contaminated.
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
# HTML essays KEPT (burnish-16, Paul confirmed authorship + the borderline calls):
#   barbecue-and-project-management, becoming-a-developer-overnight-in-only-five-years,
#   compute-magazine-archives, george-orwell-and-effective-coding, personas-in-the-wild
#   (clear reflective essays); how-i-plan-my-day, scheduling-every-minute-revisited
#   (personal but how-to register, kept on Paul's call); master-foo-and-the-technical-recruiter
#   (framing paragraphs only; the Unix-koan pastiche body is cut at its <figure>).
# HTML DROPPED: buzzword-bucket (tech-term list), on-recruiting (~95-word stub),
#   about (bio page); plus all course chapters, code/tool pages, and page fragments.
#
# Usage: scripts/build-essay-corpus.sh [SOURCE_CONTENT_DIR]
#   SOURCE_CONTENT_DIR defaults to Paul's local parkscomputing content tree.
#   Re-run after editing the keep-list; it mirrors, not appends (clears the dest).
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="${1:-$HOME/OneDrive/Documents/parkscomputing.com/wwwroot/content}"
DEST="$ROOT/corpus/paul-essays"

# Whitelist of real HTML tag names. Tag stripping matches only these, so prose
# like "if x<y" or "vector<int>" is NOT mistaken for a tag and silently eaten
# (the failure of a naive <[^>]*> or <letter pattern). Shared by both strip paths
# via $ENV{TAGS} inside the perl regexes.
export TAGS='a|abbr|address|article|aside|b|bdi|bdo|blockquote|br|button|canvas|caption|cite|code|col|colgroup|data|datalist|dd|del|details|dfn|dialog|div|dl|dt|em|embed|fieldset|figcaption|figure|footer|form|h1|h2|h3|h4|h5|h6|head|header|hgroup|hr|html|i|iframe|img|input|ins|kbd|label|legend|li|link|main|map|mark|menu|meta|meter|nav|noscript|object|ol|optgroup|option|output|p|param|picture|pre|progress|q|rp|rt|ruby|s|samp|script|section|select|small|source|span|strong|style|sub|summary|sup|svg|table|tbody|td|template|textarea|tfoot|th|thead|time|title|tr|track|u|ul|var|video|wbr|path|g|rect|circle|line|polygon|polyline|defs|use'

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

# HTML essays to extract (no .html extension here; added in the loop below).
HTML_KEEP="
barbecue-and-project-management
becoming-a-developer-overnight-in-only-five-years
compute-magazine-archives
george-orwell-and-effective-coding
personas-in-the-wild
how-i-plan-my-day
scheduling-every-minute-revisited
master-foo-and-the-technical-recruiter
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
# Tag stripping matches only whitelisted HTML tag names ($ENV{TAGS}), so prose
# comparisons and code mentions ("a < b", "if x<y", "vector<int>") are left intact
# rather than swallowed up to the next ">". Markdown structure (# headings, -
# lists) is kept, since cadence features are measured from it.
strip() {
  awk 'NR==1 && $0=="---"{infm=1;next} infm && $0=="---"{infm=0;next} !infm' "$1" \
    | perl -0777 -pe '
        s{<figure\b[^>]*>.*?</figure>}{}gis;       # whole figure blocks (incl. captions)
        s/<!--.*?-->//gs;                          # HTML comments
        s{</?(?:$ENV{TAGS})\b[^<>]*>}{}gi;         # known HTML tags only (body cannot span a "<")
        s/!\[[^\]]*\]\([^)]*\)//g;                 # markdown images -> drop
        s/\[([^\]]*)\]\([^)]*\)/$1/g;              # markdown links -> visible text
        s{https?://\S+}{}g;                        # bare URLs
      '
}

# html_extract pulls the body prose out of one of the site's hand-written HTML
# essays. Their structure is simple and consistent (body with <h1>/<p>/<blockquote>/
# <a>), so a structural extraction is enough; no HTML-parser dependency. It drops
# head/script/style and <figure> blocks, maps headings/paragraphs/lists to markdown
# structure, strips remaining tags, and decodes the common entities (incl. smart
# quotes via \x27 so no shell-quoting is needed).
html_extract() {
  perl -CSD -0777 -ne '
    s/<!--.*?-->//gs;                                   # comments first
    s{<(script|style|head)\b[^>]*>.*?</\1>}{}gis;       # script/style/head wholesale
    s/.*<body[^>]*>//is; s{</body>.*}{}is;              # then trim to body (if present)
    s{<figure\b[^>]*>.*?</figure>}{}gis;                # figure blocks incl. captions
    s{<h([1-6])\b[^>]*>}{"\n\n" . ("#" x $1) . " "}gie; s{</h[1-6]>}{\n}gi;
    s{</(p|blockquote|li|div|ul|ol)>}{\n\n}gi; s{<li\b[^>]*>}{- }gi; s{<br\s*/?>}{\n}gi;
    s{</?(?:$ENV{TAGS})\b[^<>]*>}{}gi;                  # known HTML tags only; body cannot span "<" (prose x<y safe)
    s/&nbsp;/ /g; s/&amp;/&/g; s/&lt;/</g; s/&gt;/>/g; s/&quot;/"/g;
    s/&#8217;|&#8216;|&rsquo;|&lsquo;/\x27/g; s/&#8220;|&#8221;|&ldquo;|&rdquo;/"/g;
    s/&#8212;|&mdash;/--/g; s/&#8230;|&hellip;/.../g;
    s/&#x([0-9a-fA-F]+);/chr(hex($1))/ge; s/&#(\d+);/chr($1)/ge;  # numeric entities
    s/&[a-zA-Z]+;//g;                                   # drop remaining unknown named entities
    s/^[ \t]+//mg;           # drop source HTML indentation (else 4-space = code block)
    s/\n{3,}/\n\n/g; print;
  ' "$1"
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

for h in $HTML_KEEP; do
  src="$SRC/$h.html"
  if [ ! -f "$src" ]; then
    echo "  MISSING: $h.html" >&2
    missing=$((missing+1))
    continue
  fi
  if [ "$h" = "master-foo-and-the-technical-recruiter" ]; then
    # Keep only Paul's framing paragraphs; the Unix-koan pastiche body and the
    # later update note both start at the <figure>, so cut everything from there.
    html_extract <(perl -0777 -pe 's{<figure\b.*}{</body>}is' "$src") > "$DEST/$h.md"
  else
    html_extract "$src" > "$DEST/$h.md"
  fi
  n=$((n+1))
done

words=$(cat "$DEST"/*.md 2>/dev/null | wc -w | tr -d ' ')
echo "curated $n essays into $DEST ($words words); $missing missing"
[ "$missing" -eq 0 ] || { echo "some keep-list files were missing from the source" >&2; exit 1; }
echo
echo "next: distill the curated corpus, e.g."
echo "  go run ./cmd/burnish distill --corpus \"$DEST\" --register personal-essay --id paul-essays --avoid \"—,--\" --out profiles/paul-essays.profile.yaml"
