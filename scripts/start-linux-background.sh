#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

EXE_PATH="${EXE_PATH:-$REPO_ROOT/dist/all-notify-linux-amd64}"
DATA_DIR="${DATA_DIR:-$REPO_ROOT/data}"
ADDR="${ADDR:-:8080}"
SEND_TIMEOUT="${SEND_TIMEOUT:-10s}"
LOG_MAX_BYTES="${LOG_MAX_BYTES:-10485760}"
LOG_MAX_BACKUPS="${LOG_MAX_BACKUPS:-5}"
PID_FILE="${PID_FILE:-$DATA_DIR/all-notify.pid}"
STDOUT_LOG="${STDOUT_LOG:-$DATA_DIR/logs/stdout.log}"
STDERR_LOG="${STDERR_LOG:-$DATA_DIR/logs/stderr.log}"

usage() {
  cat <<USAGE
Usage: $0 [options]

Options:
  --exe PATH              executable path, default: $EXE_PATH
  --data-dir PATH         data directory, default: $DATA_DIR
  --addr ADDR             listen address, default: $ADDR
  --send-timeout VALUE    send timeout, default: $SEND_TIMEOUT
  --log-max-bytes VALUE   app log max bytes, default: $LOG_MAX_BYTES
  --log-max-backups N     app log backup count, default: $LOG_MAX_BACKUPS
  --pid-file PATH         pid file, default: $PID_FILE
  --stdout-log PATH       stdout log, default: $STDOUT_LOG
  --stderr-log PATH       stderr log, default: $STDERR_LOG
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --exe) EXE_PATH="$2"; shift 2 ;;
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --addr) ADDR="$2"; shift 2 ;;
    --send-timeout) SEND_TIMEOUT="$2"; shift 2 ;;
    --log-max-bytes) LOG_MAX_BYTES="$2"; shift 2 ;;
    --log-max-backups) LOG_MAX_BACKUPS="$2"; shift 2 ;;
    --pid-file) PID_FILE="$2"; shift 2 ;;
    --stdout-log) STDOUT_LOG="$2"; shift 2 ;;
    --stderr-log) STDERR_LOG="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ ! -x "$EXE_PATH" ]]; then
  echo "executable not found or not executable: $EXE_PATH" >&2
  exit 1
fi

mkdir -p "$DATA_DIR" "$(dirname -- "$PID_FILE")" "$(dirname -- "$STDOUT_LOG")" "$(dirname -- "$STDERR_LOG")"

if [[ -f "$PID_FILE" ]]; then
  old_pid="$(cat "$PID_FILE" || true)"
  if [[ "$old_pid" =~ ^[0-9]+$ ]] && kill -0 "$old_pid" 2>/dev/null; then
    echo "All Notify is already running, PID: $old_pid"
    exit 0
  fi
fi

nohup "$EXE_PATH" \
  -addr="$ADDR" \
  -data-dir="$DATA_DIR" \
  -send-timeout="$SEND_TIMEOUT" \
  -log-max-bytes="$LOG_MAX_BYTES" \
  -log-max-backups="$LOG_MAX_BACKUPS" \
  >"$STDOUT_LOG" 2>"$STDERR_LOG" &
pid="$!"
echo "$pid" >"$PID_FILE"

echo "All Notify started in background, PID: $pid"
echo "Listen address: $ADDR"
echo "Data dir: $DATA_DIR"
echo "PID file: $PID_FILE"
echo "Stdout log: $STDOUT_LOG"
echo "Stderr log: $STDERR_LOG"
