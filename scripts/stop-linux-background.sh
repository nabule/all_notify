#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
DATA_DIR="${DATA_DIR:-$REPO_ROOT/data}"
PID_FILE="${PID_FILE:-$DATA_DIR/all-notify.pid}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-30}"
FORCE=0

usage() {
  cat <<USAGE
Usage: $0 [options]

Options:
  --pid-file PATH      pid file, default: $PID_FILE
  --timeout SECONDS    graceful stop timeout, default: $TIMEOUT_SECONDS
  --force              send SIGKILL if process does not stop in time
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pid-file) PID_FILE="$2"; shift 2 ;;
    --timeout) TIMEOUT_SECONDS="$2"; shift 2 ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ ! -f "$PID_FILE" ]]; then
  echo "PID file not found, background process may not be running: $PID_FILE"
  exit 0
fi

pid="$(cat "$PID_FILE" || true)"
if [[ ! "$pid" =~ ^[0-9]+$ ]]; then
  rm -f "$PID_FILE"
  echo "invalid PID file removed: $PID_FILE" >&2
  exit 1
fi

if ! kill -0 "$pid" 2>/dev/null; then
  rm -f "$PID_FILE"
  echo "process does not exist, PID file removed: $PID_FILE"
  exit 0
fi

kill "$pid"
deadline=$((SECONDS + TIMEOUT_SECONDS))
while kill -0 "$pid" 2>/dev/null && [[ $SECONDS -lt $deadline ]]; do
  sleep 1
done

if kill -0 "$pid" 2>/dev/null; then
  if [[ "$FORCE" -ne 1 ]]; then
    echo "process did not stop in ${TIMEOUT_SECONDS}s. Re-run with --force to send SIGKILL. PID: $pid" >&2
    exit 1
  fi
  kill -9 "$pid" 2>/dev/null || true
fi

rm -f "$PID_FILE"
echo "All Notify background process stopped, PID: $pid"
