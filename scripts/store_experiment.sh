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
RUNS=${RUNS:-50}
READER_SETTLE=${READER_SETTLE:-5}
READY_TIMEOUT=${READY_TIMEOUT:-60}
DUMP_TIMEOUT=${DUMP_TIMEOUT:-15}
KILL_REPLICA=${KILL_REPLICA:-1}
RESULTS_DIR=${RESULTS_DIR:-results/3}

case "$SCHEME" in
  star|ring|tree|multi-seed) ;;
  *)
    echo "ERROR: unknown SCHEME='$SCHEME'" >&2
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

CSV="$RESULTS_DIR/${SCHEME}-store.csv"
TXT="$RESULTS_DIR/${SCHEME}-store.txt"

PIDS=()
declare -A NODE_PIDS=()
declare -A NODE_BY_ID=()

cleanup() {
  echo "stopping nodes"
  for pid in "${PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "building"
go build -o bin/node ./cmd/node

echo "starting network: scheme=${SCHEME} nodes=${NODES} base_port=${BASE_PORT} k=${K}"
rm -rf "$STATE"
mkdir -p "$CONFIGS" "$DUMPS" "$READY"

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
  "log_level": "INFO",
  "ttl_seconds": 180
}
EOF
done

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
    -publish-self \
    >"$log" 2>&1 &

  pid=$!
  PIDS+=("$pid")
  NODE_PIDS["node-${id}"]="$pid"
  sleep 0.3
done

echo "waiting for readiness"
dl=$(( $(date +%s) + READY_TIMEOUT ))
while true; do
  cnt=0
  for i in $(seq 1 "$NODES"); do
    [ -f "$READY/node-$(printf '%02d' "$i")" ] && cnt=$((cnt + 1))
  done
  if [ "$cnt" -eq "$NODES" ]; then
    echo "all ${NODES} nodes ready"
    break
  fi
  if [ "$(date +%s)" -ge "$dl" ]; then
    echo "WARNING: only ${cnt}/${NODES} ready; continuing"
    break
  fi
  sleep 0.2
done

echo "waiting for publish-self results"
sleep 3

declare -a KEYS=()
declare -a OWNERS=()
for i in $(seq 1 "$NODES"); do
  id=$(printf '%02d' "$i")
  name="node-${id}"
  log=$STATE/node-${id}.log
  ready=$READY/node-${id}

  line=$(grep -E "publish-self: key=" "$log" | tail -n1 || true)
  if [ -z "$line" ]; then
    echo "  ${name}: no publish-self line, skipping" >&2
    continue
  fi
  key=$(echo "$line" | sed -n 's/.*key=\([0-9a-f]*\).*/\1/p')
  if [ -z "$key" ]; then
    echo "  ${name}: cannot parse key" >&2
    continue
  fi

  full_id=""
  if [ -f "$ready" ]; then
    full_id=$(cat "$ready")
  fi
  short_id=${full_id:0:8}

  KEYS+=("$key")
  OWNERS+=("$name")
  if [ -n "$short_id" ]; then
    NODE_BY_ID["$short_id"]="$name"
  fi
done

if [ "${#KEYS[@]}" -eq 0 ]; then
  echo "ERROR: no published keys collected" >&2
  exit 1
fi

echo "collected ${#KEYS[@]} published keys"

VICTIM_NAME=""
VICTIM_KEY=""
if [ "$KILL_REPLICA" -eq 1 ] && [ "$NODES" -gt 5 ]; then
  for idx in "${!KEYS[@]}"; do
    key="${KEYS[$idx]}"
    owner="${OWNERS[$idx]}"
    owner_log=$STATE/${owner}.log
    key_short="${key:0:8}"

    line=$(grep -E "store put key=${key_short}.*stored_at=" "$owner_log" | tail -n1 || true)
    if [ -z "$line" ]; then
      continue
    fi
    stored=$(echo "$line" | sed -n 's/.*stored_at=\([0-9a-f,]*\).*/\1/p')
    if [ -z "$stored" ]; then
      continue
    fi

    IFS=',' read -ra REPLICAS <<< "$stored"

    if [ "${#REPLICAS[@]}" -lt 3 ]; then
      continue
    fi

    for r in "${REPLICAS[@]}"; do
      if [ -z "${NODE_BY_ID[$r]:-}" ]; then
        continue
      fi
      candidate="${NODE_BY_ID[$r]}"
      if [ "$candidate" = "$owner" ]; then
        continue
      fi
      if [ "$candidate" = "node-01" ]; then
        continue
      fi
      VICTIM_NAME="$candidate"
      VICTIM_KEY="$key"
      break 2
    done
  done
fi

if [ -n "$VICTIM_NAME" ]; then
  victim_pid="${NODE_PIDS[$VICTIM_NAME]:-}"
  if [ -n "$victim_pid" ]; then
    echo "killing replica of key=${VICTIM_KEY:0:8}: ${VICTIM_NAME} (pid=${victim_pid})"
    kill "$victim_pid" 2>/dev/null || true
    sleep 1
  fi
else
  if [ "$KILL_REPLICA" -eq 1 ]; then
    echo "WARNING: no replica found to kill (KILL_REPLICA=1)"
  fi
fi

echo "running ${RUNS} get operations"
HALF=$(( RUNS / 2 ))
if [ -n "$VICTIM_KEY" ]; then
  echo "plan: first ${HALF} runs read VICTIM_KEY (${VICTIM_KEY:0:8}), remaining $(( RUNS - HALF )) runs random"
else
  echo "plan: no VICTIM_KEY, all ${RUNS} runs random"
fi
echo "reader settle: ${READER_SETTLE}s after bootstrap before Get"
echo "run,initiator,target_key,is_victim_key,success,seq,rpcs,iters,duration_ms" > "$CSV"

ok=0
for run in $(seq 1 "$RUNS"); do
  if [ -n "$VICTIM_KEY" ] && [ "$run" -le "$HALF" ]; then
    key="$VICTIM_KEY"
  else
    idx=$(( RANDOM % ${#KEYS[@]} ))
    key="${KEYS[$idx]}"
  fi

  reader_port=$((BASE_PORT + NODES + 100 + run))
  reader_state=$STATE/reader-$run
  rm -rf "$reader_state"
  mkdir -p "$reader_state"

  reader_cfg=$STATE/reader-$run.json
  cat > "$reader_cfg" <<EOF
{
  "state_dir": "./${reader_state}",
  "listen_host": "127.0.0.1",
  "listen_port": ${reader_port},
  "bootstrap_peers": [
    {"host":"127.0.0.1","port":${BASE_PORT}},
    {"host":"127.0.0.1","port":$((BASE_PORT + 1))},
    {"host":"127.0.0.1","port":$((BASE_PORT + 2))}
  ],
  "node_id_bits": 256,
  "k_bucket_size": ${K},
  "alpha": ${ALPHA},
  "connect_timeout_ms": 3000,
  "read_timeout_ms": 5000,
  "ping_timeout_ms": 5000,
  "max_frame_payload": 65536,
  "protocol_version": 1,
  "log_level": "INFO",
  "ttl_seconds": 180
}
EOF

  reader_log=$STATE/reader-$run.log
  reader_ready=$STATE/reader-$run.ready

  ./bin/node \
    -config "$reader_cfg" \
    -get-key "$key" \
    -ready-file "$reader_ready" \
    >"$reader_log" 2>&1 &
  reader_pid=$!
  PIDS+=("$reader_pid")

  dl=$(( $(date +%s) + 15 ))
  while [ ! -f "$reader_ready" ]; do
    if [ "$(date +%s)" -ge "$dl" ]; then
      break
    fi
    sleep 0.1
  done
  sleep "$READER_SETTLE"

  dl=$(( $(date +%s) + 15 ))
  while true; do
    if grep -qE "get-key: (found|not found)" "$reader_log" 2>/dev/null; then
      break
    fi
    if [ "$(date +%s)" -ge "$dl" ]; then
      break
    fi
    sleep 0.2
  done

  kill "$reader_pid" 2>/dev/null || true
  wait "$reader_pid" 2>/dev/null || true

  line=$(grep -E "get-key: (found|not found)" "$reader_log" | tail -n1 || true)
  if echo "$line" | grep -q "found seq="; then
    success=true
    ok=$((ok + 1))
    seq=$(echo "$line" | sed -n 's/.*seq=\([0-9]*\).*/\1/p')
  else
    success=false
    seq=0
  fi

  getline=$(grep -E "store get key=" "$reader_log" | tail -n1 || true)
  rpcs=0; iters=0; dur=0
  if [ -n "$getline" ]; then
    rpcs=$(echo "$getline" | sed -n 's/.*rpcs=\([0-9]*\).*/\1/p')
    iters=$(echo "$getline" | sed -n 's/.*iters=\([0-9]*\).*/\1/p')
    dur=$(echo "$getline" | sed -n 's/.*duration=\([0-9]*\)ms.*/\1/p')
  fi

  is_victim=0
  if [ -n "$VICTIM_KEY" ] && [ "$key" = "$VICTIM_KEY" ]; then
    is_victim=1
  fi

  echo "${run},reader-${run},${key},${is_victim},${success},${seq},${rpcs},${iters},${dur}" >> "$CSV"
  printf "  run=%-3s key=%s victim=%s success=%s rpcs=%s iters=%s dur=%sms\n" \
    "$run" "${key:0:8}" "$is_victim" "$success" "$rpcs" "$iters" "$dur"
done

victim_reads=0
victim_success=0
non_victim_reads=0
non_victim_success=0
if [ -n "$VICTIM_KEY" ]; then
  victim_reads=$(awk -F, -v k="$VICTIM_KEY" 'NR>1 && $3==k {n++} END{print n+0}' "$CSV")
  victim_success=$(awk -F, -v k="$VICTIM_KEY" 'NR>1 && $3==k && $5=="true" {n++} END{print n+0}' "$CSV")
  non_victim_reads=$(awk -F, -v k="$VICTIM_KEY" 'NR>1 && $3!=k {n++} END{print n+0}' "$CSV")
  non_victim_success=$(awk -F, -v k="$VICTIM_KEY" 'NR>1 && $3!=k && $5=="true" {n++} END{print n+0}' "$CSV")
else
  non_victim_reads=$(awk -F, 'NR>1 {n++} END{print n+0}' "$CSV")
  non_victim_success=$(awk -F, 'NR>1 && $5=="true" {n++} END{print n+0}' "$CSV")
fi

echo "summary: ${ok}/${RUNS} successful"
{
  echo "scheme=${SCHEME} nodes=${NODES} runs=${RUNS} ok=${ok}"
  if [ -n "$VICTIM_NAME" ]; then
    echo "killed_replica=${VICTIM_NAME} key=${VICTIM_KEY:0:8}"
    echo "victim_key=${VICTIM_KEY:0:8} reads=${victim_reads} success=${victim_success}"
    echo "non_victim_reads=${non_victim_reads} non_victim_success=${non_victim_success}"
  else
    echo "victim_key=none reads=0 success=0"
    echo "non_victim_reads=${non_victim_reads} non_victim_success=${non_victim_success}"
  fi
  echo
  echo "raw CSV: $CSV"
  echo
  echo "aggregates"
  awk -F, 'NR>1 && $5=="true" {
      n++;
      sum_rpcs += $7;
      sum_iters += $8;
      sum_dur += $9;
      durs[n] = $9;
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

exit 0