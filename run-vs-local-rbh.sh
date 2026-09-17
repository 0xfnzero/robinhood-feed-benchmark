#!/usr/bin/env bash
# Start local rbh-feed relay, then compare against official Feed via robinhood-feed-benchmark.
set -euo pipefail

RBH_FEED_BIN="${RBH_FEED_BIN:-$HOME/WorkSpace/SolanaProjects/rbh-feed/target/release/rbh-feed}"
LISTEN="${LISTEN:-127.0.0.1:9642}"
CONNECTIONS="${CONNECTIONS:-8}"
DURATION="${DURATION:-45s}"
BENCH_DIR="$(cd "$(dirname "$0")" && pwd)"

if [[ ! -x "$RBH_FEED_BIN" ]]; then
  echo "error: rbh-feed binary not found: $RBH_FEED_BIN" >&2
  echo "build with: cargo build --release --features ultra-perf" >&2
  exit 1
fi

PID_FILE="${TMPDIR:-/tmp}/rbh-feed-relay-bench.pid"
LOG_FILE="${TMPDIR:-/tmp}/rbh-feed-relay-bench.log"

if [[ -f "$PID_FILE" ]]; then
  OLD="$(tr -d '[:space:]' <"$PID_FILE" || true)"
  if [[ -n "${OLD:-}" ]] && kill -0 "$OLD" 2>/dev/null; then
    echo "relay already running pid=$OLD"
  else
    rm -f "$PID_FILE"
  fi
fi

if [[ ! -f "$PID_FILE" ]]; then
  nohup "$RBH_FEED_BIN" relay --listen "$LISTEN" --connections "$CONNECTIONS" >"$LOG_FILE" 2>&1 &
  echo $! >"$PID_FILE"
  echo "started relay pid=$(cat "$PID_FILE") listen=$LISTEN connections=$CONNECTIONS"
  sleep 6
fi

if [[ ! -x "$BENCH_DIR/bin/feed-benchmark" ]]; then
  (cd "$BENCH_DIR" && make build)
fi

exec "$BENCH_DIR/bin/feed-benchmark" \
  --duration "$DURATION" \
  --feed "LocalRBH=ws://$LISTEN" \
  "$@"
