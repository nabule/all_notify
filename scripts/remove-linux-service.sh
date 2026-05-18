#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="${SERVICE_NAME:-all-notify}"
DRY_RUN=0

usage() {
  cat <<USAGE
Usage: sudo $0 [options]

Options:
  --service-name NAME    systemd service name, default: $SERVICE_NAME
  --dry-run              print actions without changing systemd
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --service-name) SERVICE_NAME="$2"; shift 2 ;;
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

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "DRY RUN: systemctl stop $SERVICE_NAME"
  echo "DRY RUN: systemctl disable $SERVICE_NAME"
  echo "DRY RUN: rm -f $UNIT_PATH"
  echo "DRY RUN: systemctl daemon-reload"
  exit 0
fi

systemctl stop "$SERVICE_NAME" 2>/dev/null || true
systemctl disable "$SERVICE_NAME" 2>/dev/null || true
rm -f "$UNIT_PATH"
systemctl daemon-reload
systemctl reset-failed "$SERVICE_NAME" 2>/dev/null || true

echo "Linux systemd service removed: $SERVICE_NAME"
