#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

NODES=${NODES:-20}
BASE_PORT=${BASE_PORT:-9101}
K=${K:-4}
ALPHA=${ALPHA:-3}
SEEDS=${SEEDS:-4}
READY_TIMEOUT=${READY_TIMEOUT:-60}
DUMP_TIMEOUT=${DUMP_TIMEOUT:-15}
RESULTS_DIR=${RESULTS_DIR:-results/2/bootstrap-schemes}

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required" >&2
  exit 1
fi

mkdir -p "$RESULTS_DIR"

SCHEMES=(star ring tree multi-seed)

build_once() {
  if [ -x bin/node ]; then
    return
  fi
  echo "building"
  go build -o bin/node ./cmd/node
}

run_scheme() {
  local scheme="$1"
  local state="state-${scheme}"
  local configs="$state/configs"
  local dumps="$state/dump"
  local ready="$state/ready"
  local out="$RESULTS_DIR/${scheme}"

  echo "scheme=${scheme}"

  rm -rf "$state"
  mkdir -p "$configs" "$dumps" "$ready" "$out"

  for i in $(seq 1 "$NODES"); do
    local port=$((BASE_PORT + i - 1))
    local cfg="$configs/node-$(printf '%02d' "$i").json"

    local peers
    case "$scheme" in
      star)
        if [ "$i" -eq 1 ]; then peers='[]';
        else peers="[{\"host\":\"127.0.0.1\",\"port\":${BASE_PORT}}]"; fi
        ;;
      ring)
        if [ "$i" -eq 1 ]; then peers='[]';
        else
          local prev_port=$((BASE_PORT + i - 2))
          peers="[{\"host\":\"127.0.0.1\",\"port\":${prev_port}}]"
        fi
        ;;
      tree)
        if [ "$i" -eq 1 ]; then peers='[]';
        else
          local parent=$((i / 2))
          local parent_port=$((BASE_PORT + parent - 1))
          peers="[{\"host\":\"127.0.0.1\",\"port\":${parent_port}}]"
        fi
        ;;
      multi-seed)
        if [ "$i" -le "$SEEDS" ]; then
          if [ "$i" -eq 1 ]; then peers='[]';
          else
            local prev_seed_port=$((BASE_PORT + i - 2))
            peers="[{\"host\":\"127.0.0.1\",\"port\":${prev_seed_port}}]"
          fi
        else
          local primary secondary primary_port secondary_port
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
  "state_dir": "./${state}/node-$(printf '%02d' "$i")",
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

  local pids=()

  cleanup_scheme() {
    local pid
    for pid in "${pids[@]:-}"; do
      kill "$pid" 2>/dev/null || true
    done
    wait 2>/dev/null || true
  }
  trap cleanup_scheme RETURN

  for i in $(seq 1 "$NODES"); do
    local id
    id=$(printf '%02d' "$i")
    local cfg="$configs/node-${id}.json"
    local log="$state/node-${id}.log"
    local readyf="$ready/node-${id}"

    ./bin/node \
      -config "$cfg" \
      -dump-dir "$dumps" \
      -dump-interval 2s \
      -ready-file "$readyf" \
      >"$log" 2>&1 &

    pids+=("$!")
    sleep 0.3
  done

  local dl=$(( $(date +%s) + READY_TIMEOUT ))
  while true; do
    local cnt=0
    for i in $(seq 1 "$NODES"); do
      [ -f "$ready/node-$(printf '%02d' "$i")" ] && cnt=$((cnt + 1))
    done
    if [ "$cnt" -eq "$NODES" ]; then
      break
    fi
    if [ "$(date +%s)" -ge "$dl" ]; then
      echo "WARNING: ${scheme}: only ${cnt}/${NODES} ready; continuing"
      break
    fi
    sleep 0.2
  done

  dl=$(( $(date +%s) + DUMP_TIMEOUT ))
  while true; do
    local cnt
    cnt=$(ls -1 "$dumps"/routing-*-initial.json 2>/dev/null | wc -l)
    if [ "$cnt" -ge "$NODES" ]; then
      break
    fi
    if [ "$(date +%s)" -ge "$dl" ]; then
      echo "WARNING: ${scheme}: only ${cnt}/${NODES} dumps; continuing"
      break
    fi
    sleep 0.2
  done

  SCHEME="$scheme" NODES="$NODES" RESULTS_DIR="$RESULTS_DIR" ./scripts/check_non_degenerate.sh || true

  cp -r "$configs" "$out/configs"
  cp -r "$dumps" "$out/dumps"

  local csv="$out/buckets.csv"
  echo "node_id,total_contacts,nonempty_buckets,bucket_index,contacts_in_bucket" > "$csv"
  for f in "$dumps"/routing-*-initial.json; do
    [ -f "$f" ] || continue
    local node_id total nonempty
    node_id=$(jq -r '.node_id' "$f")
    total=$(jq -r '.total_contacts' "$f")
    nonempty=$(jq -r '.buckets | length' "$f")
    jq -r '.buckets[] | "\(.index),\(.contacts | length)"' "$f" | while IFS=, read -r bidx clen; do
      echo "${node_id},${total},${nonempty},${bidx},${clen}"
    done >> "$csv"
  done

  local total_contacts_sum=0
  local nonempty_sum=0
  local count=0
  for f in "$dumps"/routing-*-initial.json; do
    [ -f "$f" ] || continue
    local tc nb
    tc=$(jq -r '.total_contacts' "$f")
    nb=$(jq -r '.buckets | length' "$f")
    total_contacts_sum=$((total_contacts_sum + tc))
    nonempty_sum=$((nonempty_sum + nb))
    count=$((count + 1))
  done

  local avg_contacts=0
  local avg_nonempty=0
  if [ "$count" -gt 0 ]; then
    avg_contacts=$(awk -v s="$total_contacts_sum" -v c="$count" 'BEGIN{printf "%.2f", s/c}')
    avg_nonempty=$(awk -v s="$nonempty_sum" -v c="$count" 'BEGIN{printf "%.2f", s/c}')
  fi

  local nd_pass="false"
  if [ -f "$RESULTS_DIR/${scheme}-nondegeneracy.json" ]; then
    nd_pass=$(jq -r '.pass' "$RESULTS_DIR/${scheme}-nondegeneracy.json")
  fi

  jq -n \
    --arg scheme "$scheme" \
    --argjson n "$NODES" \
    --argjson k "$K" \
    --argjson alpha "$ALPHA" \
    --argjson avg_contacts "$avg_contacts" \
    --argjson avg_nonempty "$avg_nonempty" \
    --argjson pass "$nd_pass" \
    '{
      scheme: $scheme,
      N: $n,
      K: $k,
      alpha: $alpha,
      avg_contacts: $avg_contacts,
      avg_nonempty_buckets: $avg_nonempty,
      nondegeneracy_pass: $pass
    }' > "$out/summary.json"

  echo "scheme=${scheme} avg_contacts=${avg_contacts} avg_nonempty_buckets=${avg_nonempty} nondegeneracy_pass=${nd_pass}"

  cleanup_scheme
  trap - RETURN
  rm -rf "$state"
}

build_once

for s in "${SCHEMES[@]}"; do
  run_scheme "$s"
done

echo "artifacts under $RESULTS_DIR"
ls -1 "$RESULTS_DIR"