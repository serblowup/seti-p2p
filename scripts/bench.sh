#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

mkdir -p results/1

go test ./internal/protocol/ -run TestSerializationSizes -v 2>&1 \
  | tee results/1/serialization-sizes.txt

go test ./internal/protocol/ \
  -bench 'BenchmarkPing|BenchmarkFindNode' \
  -benchmem \
  -run '^$' \
  -count=3 \
  2>&1 | tee results/1/serialization-bench.txt