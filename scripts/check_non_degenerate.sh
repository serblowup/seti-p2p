#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SCHEME=${SCHEME:-star}
NODES=${NODES:-20}
STATE=state-${SCHEME}
DUMPS=$STATE/dump
RESULTS_DIR=${RESULTS_DIR:-results}

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required" >&2
  exit 1
fi

OUT="$RESULTS_DIR/${SCHEME}-nondegeneracy.json"
mkdir -p "$RESULTS_DIR"

echo "==> checking non-degeneracy for scheme=${SCHEME} N=${NODES}"

mapfile -t dumps < <(ls "$DUMPS"/routing-*-initial.json 2>/dev/null || true)

if [ "${#dumps[@]}" -lt 2 ]; then
  echo "ERROR: need at least 2 initial dumps, found ${#dumps[@]}" >&2
  exit 1
fi

total_nodes=${#dumps[@]}
echo "  found ${total_nodes} initial dumps (expected ${NODES})"

if [ "$total_nodes" -ne "$NODES" ]; then
  echo "  ERROR: expected ${NODES} dumps, got ${total_nodes}" >&2
  echo "  some nodes failed to produce initial dumps; cannot reliably" >&2
  echo "  assess non-degeneracy. Check readiness markers and increase" >&2
  echo "  DUMP_TIMEOUT if needed." >&2
  exit 2
fi

threshold=$(( total_nodes * 80 / 100 ))
le_count=0
declare -a contacts_arr

for f in "${dumps[@]}"; do
  c=$(jq -r '.total_contacts' "$f")
  contacts_arr+=("$c")
  if [ "$c" -lt $(( total_nodes - 1 )) ]; then
    le_count=$((le_count + 1))
  fi
done

echo "  nodes with contacts < N-1: ${le_count}/${total_nodes} (threshold ${threshold})"

full_catalog=0
for c in "${contacts_arr[@]}"; do
  if [ "$c" -ge $(( total_nodes - 1 )) ]; then
    full_catalog=$((full_catalog + 1))
  fi
done
echo "  nodes with full catalog (contacts >= N-1): ${full_catalog}"

structured=0
for f in "${dumps[@]}"; do
  nonempty_buckets=$(jq -r '.buckets | length' "$f")
  if [ "$nonempty_buckets" -ge 2 ]; then
    structured=$((structured + 1))
  fi
done
echo "  nodes with >=2 non-empty buckets: ${structured}/${total_nodes}"

pass=1
if [ "$le_count" -lt "$threshold" ]; then
  echo "  FAIL: less than 80% nodes have contacts < N-1" >&2
  pass=0
fi
if [ "$full_catalog" -gt 0 ]; then
  echo "  FAIL: some nodes have full catalog" >&2
  pass=0
fi
if [ "$structured" -lt "$threshold" ]; then
  echo "  FAIL: less than 80% nodes have structured buckets" >&2
  pass=0
fi

jq -n \
  --arg scheme "$SCHEME" \
  --argjson n "$total_nodes" \
  --argjson le "$le_count" \
  --argjson threshold "$threshold" \
  --argjson full "$full_catalog" \
  --argjson structured "$structured" \
  --argjson pass "$pass" \
  '{
    scheme: $scheme,
    total_nodes: $n,
    nodes_with_contacts_lt_n_minus_1: $le,
    threshold_80pct: $threshold,
    nodes_with_full_catalog: $full,
    nodes_with_structured_buckets: $structured,
    pass: ($pass == 1)
  }' | tee "$OUT"

if [ "$pass" -eq 1 ]; then
  echo "==> PASS: DHT is non-degenerate"
  exit 0
else
  echo "==> FAIL: DHT is degenerate" >&2
  exit 1
fi