#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

mkdir -p results

echo "==> sizes"
go test ./internal/protocol/ -run TestSerializationSizes -v 2>&1 \
  | tee results/serialization-sizes.txt

echo
echo "==> benchmarks (3 runs)"
go test ./internal/protocol/ \
  -bench 'BenchmarkPing|BenchmarkFindNode' \
  -benchmem \
  -run '^$' \
  -count=3 \
  2>&1 | tee results/serialization-bench.txt

echo
echo "==> artifacts"
echo "  results/serialization-sizes.txt"
echo "  results/serialization-bench.txt"
