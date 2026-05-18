#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="${SERVICE_NAME:-all-notify}"
DISPLAY_NAME="${DISPLAY_NAME:-All Notify}"
EXE_PATH="${EXE_PATH:-/opt/all-notify/all-notify-linux-amd64}"
DATA_DIR="${DATA_DIR:-/var/lib/all-notify}"
ADDR="${ADDR:-:8080}"
SEND_TIMEOUT="${SEND_TIMEOUT:-10s}"
LOG_MAX_BYTES="${LOG_MAX_BYTES:-10485760}"
LOG_MAX_BACKUPS="${LOG_MAX_BACKUPS:-5}"
USER_NAME="${USER_NAME:-all-notify}"
DRY_RUN=0
RESTART=0

usage() {
  cat <<USAGE
Usage: sudo $0 [options]

Options:
  --service-name NAME       systemd service name, default: $SERVICE_NAME
  --display-name TEXT       systemd description, default: $DISPLAY_NAME
  --exe PATH                executable path, default: $EXE_PATH
  --data-dir PATH           data directory, default: $DATA_DIR
  --addr ADDR               listen address, default: $ADDR
  --send-timeout VALUE      send timeout, default: $SEND_TIMEOUT
  --log-max-bytes VALUE     app log max bytes, default: $LOG_MAX_BYTES
  --log-max-backups N       app log backup count, default: $LOG_MAX_BACKUPS
  --user USER               run as user, default: $USER_NAME
  --restart                 enable and restart service after install
  --dry-run                 print actions without writing files
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --service-name) SERVICE_NAME="$2"; shift 2 ;;
    --display-name) DISPLAY_NAME="$2"; shift 2 ;;
    --exe) EXE_PATH="$2"; shift 2 ;;
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --addr) ADDR="$2"; shift 2 ;;
    --send-timeout) SEND_TIMEOUT="$2"; shift 2 ;;
    --log-max-bytes) LOG_MAX_BYTES="$2"; shift 2 ;;
    --log-max-backups) LOG_MAX_BACKUPS="$2"; shift 2 ;;
    --user) USER_NAME="$2"; shift 2 ;;
    --restart) RESTART=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

UNIT_PATH="/etc/systemd/system/${SERVICE_NAME}.service"

if [[ "$DRY_RUN" -ne 1 && "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "please run as root, or use --dry-run" >&2
  exit 1
fi
if [[ "$DRY_RUN" -ne 1 && ! -x "$EXE_PATH" ]]; then
  echo "executable not found or not executable: $EXE_PATH" >&2
  exit 1
fi

unit_content="$(cat <<UNIT
[Unit]
Description=$DISPLAY_NAME
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$USER_NAME
WorkingDirectory=$DATA_DIR
ExecStart=$EXE_PATH -addr=$ADDR -data-dir=$DATA_DIR -send-timeout=$SEND_TIMEOUT -log-max-bytes=$LOG_MAX_BYTES -log-max-backups=$LOG_MAX_BACKUPS
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
UNIT
)"

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "DRY RUN: create user if missing: $USER_NAME"
  echo "DRY RUN: mkdir -p $DATA_DIR"
  echo "DRY RUN: write $UNIT_PATH"
  printf '%s\n' "$unit_content"
  echo "DRY RUN: systemctl daemon-reload"
  [[ "$RESTART" -eq 1 ]] && echo "DRY RUN: systemctl enable --now $SERVICE_NAME"
  exit 0
fi

if ! id "$USER_NAME" >/dev/null 2>&1; then
  useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin "$USER_NAME"
fi
mkdir -p "$DATA_DIR"
chown -R "$USER_NAME":"$USER_NAME" "$DATA_DIR"
printf '%s\n' "$unit_content" >"$UNIT_PATH"
systemctl daemon-reload

if [[ "$RESTART" -eq 1 ]]; then
  systemctl enable --now "$SERVICE_NAME"
else
  systemctl enable "$SERVICE_NAME"
fi

echo "Linux systemd service installed: $SERVICE_NAME"
echo "Unit file: $UNIT_PATH"
echo "Executable: $EXE_PATH"
echo "Data dir: $DATA_DIR"
