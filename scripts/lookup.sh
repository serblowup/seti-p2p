#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

NODES=${NODES:-20}
BASE_PORT=${BASE_PORT:-9101}
K=${K:-4}
ALPHA=${ALPHA:-3}
SCHEME=${SCHEME:-star}
SEEDS=${SEEDS:-4}
RUNS=${RUNS:-30}
READY_TIMEOUT=${READY_TIMEOUT:-60}
DUMP_TIMEOUT=${DUMP_TIMEOUT:-15}
LOOKUP_TIMEOUT=${LOOKUP_TIMEOUT:-20}
RESULTS_DIR=${RESULTS_DIR:-results}
HOLD=${HOLD:-0}

case "$SCHEME" in
  star|ring|tree|multi-seed) ;;
  *)
    echo "ERROR: unknown SCHEME='$SCHEME' (expected 'star', 'ring', 'tree', 'multi-seed')" >&2
    exit 1
    ;;
esac

STATE=state-${SCHEME}
CONFIGS=$STATE/configs
DUMPS=$STATE/dump
READY=$STATE/ready

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required" >&2
  exit 1
fi

mkdir -p "$RESULTS_DIR"

CSV="$RESULTS_DIR/${SCHEME}-lookups.csv"
TXT="$RESULTS_DIR/${SCHEME}-lookups.txt"

PIDS=()
cleanup() {
  if [ "$HOLD" -eq 1 ]; then
    echo "==> HOLD=1: network left running, PIDs: ${PIDS[*]:-}"
    return
  fi
  echo "==> stopping network"
  for pid in "${PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "==> building"
go build -o bin/node ./cmd/node

rm -rf "$STATE"
mkdir -p "$CONFIGS" "$DUMPS" "$READY"

echo "==> scheme=${SCHEME} nodes=${NODES} base_port=${BASE_PORT} k=${K}"
for i in $(seq 1 "$NODES"); do
  port=$((BASE_PORT + i - 1))
  cfg=$CONFIGS/node-$(printf '%02d' "$i").json

  case "$SCHEME" in
    star)
      if [ "$i" -eq 1 ]; then peers='[]';
      else peers="[{\"host\":\"127.0.0.1\",\"port\":${BASE_PORT}}]"; fi
      ;;
    ring)
      if [ "$i" -eq 1 ]; then peers='[]';
      else
        prev_port=$((BASE_PORT + i - 2))
        peers="[{\"host\":\"127.0.0.1\",\"port\":${prev_port}}]"
      fi
      ;;
    tree)
      if [ "$i" -eq 1 ]; then peers='[]';
      else
        parent=$((i / 2))
        parent_port=$((BASE_PORT + parent - 1))
        peers="[{\"host\":\"127.0.0.1\",\"port\":${parent_port}}]"
      fi
      ;;
    multi-seed)
      if [ "$i" -le "$SEEDS" ]; then
        if [ "$i" -eq 1 ]; then peers='[]';
        else
          prev_seed_port=$((BASE_PORT + i - 2))
          peers="[{\"host\":\"127.0.0.1\",\"port\":${prev_seed_port}}]"
        fi
      else
        primary=$(( (i - SEEDS - 1) % SEEDS + 1 ))
        secondary=$(( primary % SEEDS + 1 ))
        primary_port=$((BASE_PORT + primary - 1))
        secondary_port=$((BASE_PORT + secondary - 1))
        peers="[{\"host\":\"127.0.0.1\",\"port\":${primary_port}},{\"host\":\"127.0.0.1\",\"port\":${secondary_port}}]"
      fi
      ;;
  esac

  cat > "$cfg" <<EOF
{
  "state_dir": "./${STATE}/node-$(printf '%02d' "$i")",
  "listen_host": "127.0.0.1",
  "listen_port": ${port},
  "bootstrap_peers": ${peers},
  "node_id_bits": 256,
  "k_bucket_size": ${K},
  "alpha": ${ALPHA},
  "connect_timeout_ms": 3000,
  "read_timeout_ms": 5000,
  "ping_timeout_ms": 5000,
  "max_frame_payload": 65536,
  "protocol_version": 1,
  "log_level": "INFO"
}
EOF
done

echo "==> starting nodes"
for i in $(seq 1 "$NODES"); do
  id=$(printf '%02d' "$i")
  cfg=$CONFIGS/node-${id}.json
  log=$STATE/node-${id}.log
  ready=$READY/node-${id}

  ./bin/node \
    -config "$cfg" \
    -dump-dir "$DUMPS" \
    -dump-interval 2s \
    -ready-file "$ready" \
    >"$log" 2>&1 &

  pid=$!
  PIDS+=("$pid")
  echo "  node-${id} pid=${pid} port=$((BASE_PORT + i - 1))"
  sleep 0.3
done

echo "==> waiting for readiness (timeout ${READY_TIMEOUT}s)"
dl=$(( $(date +%s) + READY_TIMEOUT ))
while true; do
  cnt=0
  for i in $(seq 1 "$NODES"); do
    [ -f "$READY/node-$(printf '%02d' "$i")" ] && cnt=$((cnt + 1))
  done
  if [ "$cnt" -eq "$NODES" ]; then
    echo "  all ${NODES} nodes ready"
    break
  fi
  if [ "$(date +%s)" -ge "$dl" ]; then
    echo "  WARNING: only ${cnt}/${NODES} ready; continuing"
    break
  fi
  sleep 0.2
done

echo "==> waiting for initial dumps"
dl=$(( $(date +%s) + DUMP_TIMEOUT ))
while true; do
  cnt=$(ls -1 "$DUMPS"/routing-*-initial.json 2>/dev/null | wc -l)
  if [ "$cnt" -ge "$NODES" ]; then
    echo "  got ${cnt} initial dumps"
    break
  fi
  if [ "$(date +%s)" -ge "$dl" ]; then
    echo "  WARNING: only ${cnt}/${NODES} dumps; continuing"
    break
  fi
  sleep 0.2
done

select_random_pair() {
  INIT_NAME=""; INIT_ID=""; TGT_NAME=""; TGT_ID=""

  local ready_files
  mapfile -t ready_files < <(ls "$READY"/node-* 2>/dev/null || true)
  if [ "${#ready_files[@]}" -lt 2 ]; then return 1; fi

  local shuffled
  shuffled=$(printf '%s\n' "${ready_files[@]}" | shuf)

  while IFS= read -r ready_file; do
    [ -f "$ready_file" ] || continue
    local cand_name cand_id cand_dump total known
    cand_name=$(basename "$ready_file")
    cand_id=$(cat "$ready_file")
    cand_dump="$DUMPS/routing-${cand_id:0:8}-initial.json"
    [ -f "$cand_dump" ] || continue
    total=$(jq -r '.total_contacts' "$cand_dump")
    [ "$total" -eq 0 ] && continue
    known=$(jq -r '.buckets[].contacts[].node_id' "$cand_dump" | sort -u)

    local tgt_shuffled
    tgt_shuffled=$(printf '%s\n' "${ready_files[@]}" | shuf)

    while IFS= read -r tgt_file; do
      [ -f "$tgt_file" ] || continue
      local tgt_name tgt_node_id
      tgt_name=$(basename "$tgt_file")
      [ "$tgt_name" = "$cand_name" ] && continue
      tgt_node_id=$(cat "$tgt_file")
      if ! printf '%s\n' "$known" | grep -qxF "$tgt_node_id"; then
        INIT_NAME="$cand_name"; INIT_ID="$cand_id"
        TGT_NAME="$tgt_name";   TGT_ID="$tgt_node_id"
        return 0
      fi
    done <<< "$tgt_shuffled"
  done <<< "$shuffled"

  return 1
}

run_one_lookup() {
  local init_name="$1"
  local tgt_id="$2"
  local trigger_dir="$STATE/${init_name}"
  local trigger="$trigger_dir/lookup.trigger"
  local done_path="$trigger_dir/lookup.done"

  rm -f "$trigger" "$done_path"
  echo -n "$tgt_id" > "$trigger"

  local dl=$(( $(date +%s) + LOOKUP_TIMEOUT ))
  while [ ! -f "$done_path" ]; do
    if [ "$(date +%s)" -ge "$dl" ]; then
      echo "  WARN: lookup timeout for $init_name" >&2
      return 1
    fi
    sleep 0.2
  done

  mv "$done_path" "$trigger_dir/lookup.done.$(date +%s%N)" 2>/dev/null || true
  return 0
}

echo "==> running ${RUNS} lookups on scheme=${SCHEME}"
echo "run,initiator,target,success,rpcs,iters,timeouts,duration_ms,closest" > "$CSV"

ok=0
for run in $(seq 1 "$RUNS"); do
  if ! select_random_pair; then
    echo "  run=${run}: no suitable pair, skipping" >&2
    continue
  fi

  idx=$(echo "$INIT_NAME" | sed 's/node-0*//')
  init_log="$STATE/node-$(printf '%02d' "$idx").log"
  before_lines=$(wc -l < "$init_log" 2>/dev/null || echo 0)

  if ! run_one_lookup "$INIT_NAME" "$TGT_ID"; then
    echo "${run},${INIT_NAME},${TGT_NAME},timeout,0,0,0,0,0" >> "$CSV"
    continue
  fi

  new_lines=$(tail -n "+$((before_lines + 1))" "$init_log" 2>/dev/null || true)
  line=$(printf '%s\n' "$new_lines" | grep -E "lookup target=" | tail -n 1 || true)

  if [ -z "$line" ]; then
    echo "${run},${INIT_NAME},${TGT_NAME},no-log,0,0,0,0,0" >> "$CSV"
    continue
  fi

  success=$(echo "$line"  | sed -n 's/.*success=\([a-z]*\).*/\1/p')
  rpcs=$(echo "$line"     | sed -n 's/.*rpcs=\([0-9]*\).*/\1/p')
  iters=$(echo "$line"    | sed -n 's/.*iters=\([0-9]*\).*/\1/p')
  timeouts=$(echo "$line" | sed -n 's/.*timeouts=\([0-9]*\).*/\1/p')
  duration=$(echo "$line" | sed -n 's/.*duration=\([0-9]*\)ms.*/\1/p')
  closest=$(echo "$line"  | sed -n 's/.*closest=\([0-9]*\).*/\1/p')

  echo "${run},${INIT_NAME},${TGT_NAME},${success},${rpcs},${iters},${timeouts},${duration},${closest}" >> "$CSV"
  [ "$success" = "true" ] && ok=$((ok + 1))

  printf "  run=%-3s init=%-8s tgt=%-8s success=%-5s rpcs=%-3s iters=%-3s dur=%sms\n" \
    "$run" "$INIT_NAME" "$TGT_NAME" "$success" "$rpcs" "$iters" "$duration"
done

echo
echo "==> summary: ${ok}/${RUNS} successful"
{
  echo "scheme=${SCHEME} nodes=${NODES} runs=${RUNS} ok=${ok}"
  echo
  echo "raw CSV: $CSV"
  echo
  echo "--- per-run ---"
  if command -v column >/dev/null 2>&1; then
    column -t -s, "$CSV"
  else
    cat "$CSV"
  fi
  echo
  echo "--- aggregates ---"
  awk -F, 'NR>1 && $4=="true" {
      n++;
      sum_rpcs  += $5;
      sum_iters += $6;
      sum_dur   += $8;
      durs[n] = $8;
  }
  END {
      if (n == 0) { print "no successful runs"; exit }
      for (i = 1; i <= n; i++)
          for (j = i+1; j <= n; j++)
              if (durs[i] > durs[j]) { t = durs[i]; durs[i] = durs[j]; durs[j] = t }
      med = (n % 2 == 1) ? durs[int(n/2)+1] : (durs[n/2] + durs[n/2+1]) / 2
      printf "successful runs: %d\n", n
      printf "avg rpcs:        %.2f\n", sum_rpcs / n
      printf "avg iters:       %.2f\n", sum_iters / n
      printf "avg duration:    %.2f ms\n", sum_dur / n
      printf "median duration: %.2f ms\n", med
  }' "$CSV"
} | tee "$TXT"

echo
echo "==> artifacts"
echo "  csv:     $CSV"
echo "  summary: $TXT"
echo "  state:   $STATE"