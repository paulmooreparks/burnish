#!/usr/bin/env bash
# Fetch the legal corpora behind the distinctiveness baseline and regenerate
# distill/data/baseline_en.txt. Sources: Project Gutenberg (public domain) for
# bulk prose across genres and eras (through the 1920s, the most-modern legal bulk
# text), plus a modest modern Wikipedia (CC-BY-SA) sample. Only the derived
# frequency counts are committed, never the source text (a frequency table is a
# non-copyrightable factual derivative). The fetch dir is gitignored.
#
# Usage: scripts/fetch-baseline-corpus.sh
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIR="$ROOT/.baseline-corpus"
G="$DIR/gutenberg"; W="$DIR/wiki"
mkdir -p "$G" "$W"

# Diverse Project Gutenberg works: 19th-century novels, gothic/adventure, early
# 20th-century / modernist prose, essays, short stories, drama, philosophy.
GUT_IDS="98 1342 158 1400 1260 768 2701 76 74 33 2554 1399 2600 1184 996 84 345 \
43 120 174 219 35 5230 36 64317 2814 4217 2641 408 205 1080 132 1232 3300 1661 \
2852 1952 11 12 514 1727 1497 100 2542 844 25344 1250 30254 4300"

echo "fetching Project Gutenberg..."
for id in $GUT_IDS; do
  f="$G/$id.txt"
  if [ ! -s "$f" ] || [ "$(wc -c < "$f")" -lt 20000 ]; then
    curl -s -L --max-time 90 "https://www.gutenberg.org/cache/epub/$id/pg$id.txt" -o "$f"
  fi
  kb=$(( $(wc -c < "$f" 2>/dev/null || echo 0) / 1024 ))
  [ "$kb" -lt 20 ] && rm -f "$f" && printf '%s:skip ' "$id" || printf '%s:%sK ' "$id" "$kb"
done
echo

# Modern sample: substantial Wikipedia articles by title, plus random batches.
echo "fetching Wikipedia modern sample..."
T1="Computer|Internet|Software|Artificial_intelligence|Machine_learning|Smartphone|Programming_language|World_Wide_Web|Social_media|Video_game|Climate_change|Renewable_energy|Quantum_mechanics|DNA|Vaccine|Economics|Democracy|Education|Music|Film"
T2="Television|Association_football|Basketball|Cooking|Photography|Marketing|Cryptocurrency|Electric_vehicle|Podcast|Streaming_media|United_States|India|Science|Technology|Psychology|Medicine|History|Philosophy|Business|Engineering"
i=0
for t in "$T1" "$T2"; do
  i=$((i+1))
  curl -s -L --max-time 60 "https://en.wikipedia.org/w/api.php?action=query&format=json&prop=extracts&explaintext=1&exlimit=max&redirects=1&titles=$t" -o "$W/big$i.json"
done
for n in $(seq 1 20); do
  curl -s -L --max-time 30 "https://en.wikipedia.org/w/api.php?action=query&format=json&generator=random&grnnamespace=0&grnlimit=20&prop=extracts&explaintext=1&exlimit=max" -o "$W/rand$n.json"
done

echo "regenerating baseline..."
( cd "$ROOT" && go run ./cmd/mkbaseline "$G" "$W" 20000 > distill/data/baseline_en.txt )
head -4 "$ROOT/distill/data/baseline_en.txt"
