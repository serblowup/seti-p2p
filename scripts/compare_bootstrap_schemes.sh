#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

RESULTS_DIR=${RESULTS_DIR:-results/2/bootstrap-schemes}
OUT="$RESULTS_DIR/bootstrap-comparison.txt"

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required" >&2
  exit 1
fi

if [ ! -d "$RESULTS_DIR" ]; then
  echo "ERROR: $RESULTS_DIR not found; run scripts/bootstrap_schemes.sh first" >&2
  exit 1
fi

TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

{
  printf 'scheme\tN\tK\tALPHA\tavg_contacts\tavg_nonempty_buckets\tnondegeneracy_pass\n'
  for s in star ring tree multi-seed; do
    f="$RESULTS_DIR/$s/summary.json"
    [ -f "$f" ] || continue
    jq -r '[.scheme, (.N|tostring), (.K|tostring), (.alpha|tostring), (.avg_contacts|tostring), (.avg_nonempty_buckets|tostring), (.nondegeneracy_pass|tostring)] | @tsv' "$f"
  done
} > "$TMP"

{
  echo "Сравнение bootstrap-схем"
  echo "Источник: $RESULTS_DIR/<scheme>/summary.json"
  echo "Воспроизведение: ./scripts/bootstrap_schemes.sh && ./scripts/compare_bootstrap_schemes.sh"
  echo
  if command -v column >/dev/null 2>&1; then
    column -t -s $'\t' "$TMP"
  else
    cat "$TMP"
  fi
} > "$OUT"

echo "wrote $OUT"
cat "$OUT"