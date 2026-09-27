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
READY_TIMEOUT=${READY_TIMEOUT:-60}
DUMP_TIMEOUT=${DUMP_TIMEOUT:-15}
LOOKUP_TIMEOUT=${LOOKUP_TIMEOUT:-20}
SEED_FAILURE=${SEED_FAILURE:-1}
HOLD=${HOLD:-0}
CHECK_NONDEGEN=${CHECK_NONDEGEN:-1}

case "$SCHEME" in
  star)
    echo "==> scheme: star (all nodes bootstrap via node-01)"
    ;;
  ring)
    echo "==> scheme: ring (node-i bootstraps via node-(i-1))"
    ;;
  tree)
    echo "==> scheme: tree (node-i bootstraps via node-(i/2))"
    ;;
  multi-seed)
    echo "==> scheme: multi-seed (${SEEDS} seeds chained; clients use 2 seeds each)"
    ;;
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

rm -rf "$STATE"
mkdir -p "$CONFIGS" "$DUMPS" "$READY"

echo "==> building"
go build -o bin/node ./cmd/node

for i in $(seq 1 "$NODES"); do
  port=$((BASE_PORT + i - 1))
  cfg=$CONFIGS/node-$(printf '%02d' "$i").json

  case "$SCHEME" in
    star)
      if [ "$i" -eq 1 ]; then
        peers='[]'
      else
        peers="[{\"host\":\"127.0.0.1\",\"port\":${BASE_PORT}}]"
      fi
      ;;
    ring)
      if [ "$i" -eq 1 ]; then
        peers='[]'
      else
        prev_port=$((BASE_PORT + i - 2))
        peers="[{\"host\":\"127.0.0.1\",\"port\":${prev_port}}]"
      fi
      ;;
    tree)
      if [ "$i" -eq 1 ]; then
        peers='[]'
      else
        parent=$((i / 2))
        parent_port=$((BASE_PORT + parent - 1))
        peers="[{\"host\":\"127.0.0.1\",\"port\":${parent_port}}]"
      fi
      ;;
    multi-seed)
      if [ "$i" -le "$SEEDS" ]; then
        if [ "$i" -eq 1 ]; then
          peers='[]'
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

echo "==> starting ${NODES} nodes (base port ${BASE_PORT}, scheme ${SCHEME}, K=${K})"

PIDS=()
declare -A NODE_PIDS=()

cleanup() {
  if [ "$HOLD" -eq 1 ]; then
    echo "==> HOLD=1: leaving network running, pids: ${PIDS[*]:-}"
    return
  fi
  echo "==> stopping nodes"
  for pid in "${PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

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
  NODE_PIDS["node-${id}"]="$pid"
  echo "  node-${id} pid=${pid} port=$((BASE_PORT + i - 1))"
  sleep 0.3
done

echo "==> waiting for readiness markers (timeout ${READY_TIMEOUT}s)"
deadline=$(( $(date +%s) + READY_TIMEOUT ))
while true; do
  ready_count=0
  for i in $(seq 1 "$NODES"); do
    [ -f "$READY/node-$(printf '%02d' "$i")" ] && ready_count=$((ready_count + 1))
  done
  if [ "$ready_count" -eq "$NODES" ]; then
    echo "  all ${NODES} nodes ready"
    break
  fi
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "  WARNING: timeout, ${ready_count}/${NODES} ready; continuing"
    break
  fi
  sleep 0.2
done

echo "==> waiting for initial dumps"
dump_deadline=$(( $(date +%s) + DUMP_TIMEOUT ))
while true; do
  count=$(ls -1 "$DUMPS"/routing-*-initial.json 2>/dev/null | wc -l)
  if [ "$count" -ge "$NODES" ]; then
    echo "  got ${count} initial dumps"
    break
  fi
  if [ "$(date +%s)" -ge "$dump_deadline" ]; then
    echo "  WARNING: only ${count}/${NODES} initial dumps; continuing"
    break
  fi
  sleep 0.2
done

select_pair() {
  local exclude_name="$1"
  INIT_NAME=""
  INIT_ID=""
  TGT_NAME=""
  TGT_ID=""

  for ready_file in "$READY"/node-*; do
    [ -f "$ready_file" ] || continue
    local cand_name
    cand_name=$(basename "$ready_file")
    [ "$cand_name" = "$exclude_name" ] && continue

    local cand_id
    cand_id=$(cat "$ready_file")
    local cand_dump="$DUMPS/routing-${cand_id:0:8}-initial.json"
    [ -f "$cand_dump" ] || continue

    local total
    total=$(jq -r '.total_contacts' "$cand_dump")
    [ "$total" -eq 0 ] && continue

    local known
    known=$(jq -r '.buckets[].contacts[].node_id' "$cand_dump" | sort -u)

    for tgt_file in "$READY"/node-*; do
      [ -f "$tgt_file" ] || continue
      local tgt_name
      tgt_name=$(basename "$tgt_file")
      [ "$tgt_name" = "$cand_name" ] && continue
      [ "$tgt_name" = "$exclude_name" ] && continue

      local tgt_node_id
      tgt_node_id=$(cat "$tgt_file")

      if ! printf '%s\n' "$known" | grep -qxF "$tgt_node_id"; then
        INIT_NAME="$cand_name"
        INIT_ID="$cand_id"
        TGT_NAME="$tgt_name"
        TGT_ID="$tgt_node_id"
        return 0
      fi
    done
  done
  return 1
}

run_lookup() {
  local init_name="$1"
  local tgt_id="$2"

  local trigger_dir="$STATE/${init_name}"
  local trigger="$trigger_dir/lookup.trigger"
  local done_path="$trigger_dir/lookup.done"

  rm -f "$trigger" "$done_path"

  echo -n "$tgt_id" > "$trigger"
  echo "  trigger placed at $trigger"

  local dl=$(( $(date +%s) + LOOKUP_TIMEOUT ))
  while [ ! -f "$done_path" ]; do
    if [ "$(date +%s)" -ge "$dl" ]; then
      echo "  WARNING: lookup did not complete in time"
      return 1
    fi
    sleep 0.2
  done
  return 0
}

echo "==> selecting lookup pair"
if select_pair ""; then
  echo "  initiator=${INIT_NAME} (${INIT_ID:0:8}…)"
  echo "  target   =${TGT_NAME} (${TGT_ID:0:8}…)"
  echo "  target absent from initiator's INITIAL table — OK"

  if run_lookup "$INIT_NAME" "$TGT_ID"; then
    idx=$(echo "$INIT_NAME" | sed 's/node-0*//')
    init_log="$STATE/node-$(printf '%02d' "$idx").log"
    echo "==> lookup log ($init_log):"
    grep -E "lookup target|closest:" "$init_log" || true
  fi
else
  echo "  no suitable pair found; skipping lookup"
fi

if [ "$CHECK_NONDEGEN" -eq 1 ]; then
  echo
  echo "==> non-degeneracy check"
  SCHEME="$SCHEME" NODES="$NODES" ./scripts/check_non_degenerate.sh || true
fi

if [ "$SEED_FAILURE" -eq 1 ] && [ "$NODES" -gt 2 ]; then
  echo
  echo "==> seed failure test: killing node-01 (seed) and re-running lookup"

  SEED_PID="${NODE_PIDS[node-01]:-}"
  if [ -n "$SEED_PID" ]; then
    kill "$SEED_PID" 2>/dev/null || true
    echo "  node-01 (pid=${SEED_PID}) killed"
  fi

  sleep 1

  echo "==> selecting lookup pair after seed failure (node-01 excluded)"
  if select_pair "node-01"; then
    echo "  post-failure initiator=${INIT_NAME} (${INIT_ID:0:8}…)"
    echo "  post-failure target   =${TGT_NAME} (${TGT_ID:0:8}…)"

    if run_lookup "$INIT_NAME" "$TGT_ID"; then
      idx2=$(echo "$INIT_NAME" | sed 's/node-0*//')
      init_log2="$STATE/node-$(printf '%02d' "$idx2").log"
      echo "==> post-failure lookup log ($init_log2):"
      grep -E "lookup target|closest:" "$init_log2" || true
    fi
  else
    echo "  no suitable pair after seed failure; skipping"
  fi
fi

echo
echo "==> routing table summary (initial snapshots, scheme=${SCHEME})"
for f in "$DUMPS"/routing-*-initial.json; do
  [ -f "$f" ] || continue
  node=$(jq -r '.node_id[0:8]' "$f")
  total=$(jq -r '.total_contacts' "$f")
  buckets=$(jq -r '.buckets | length' "$f")
  printf "  %s…  contacts=%s  buckets=%s\n" "$node" "$total" "$buckets"
done

echo
echo "==> artifacts (scheme=${SCHEME})"
echo "  configs:  $CONFIGS"
echo "  dumps:    $DUMPS"
echo "  ready:    $READY"
echo "  logs:     $STATE/node-*.log"