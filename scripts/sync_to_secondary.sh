#!/usr/bin/env bash
# =============================================================
# scripts/sync_to_secondary.sh
# Rsyncs the master.checkpoint and master.wal files to the
# secondary master device so it can recover state if promoted.
#
# Run this on the PRIMARY MASTER device, either manually or
# add it as a cron job (e.g. every 5 minutes):
#
#   */5 * * * * /path/to/xorfs/scripts/sync_to_secondary.sh >> /var/log/xorfs_sync.log 2>&1
#
# Usage (manual):
#   ./scripts/sync_to_secondary.sh
# =============================================================

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ="$(dirname "$SCRIPT_DIR")"
cd "$PROJ"

# ── helpers ──────────────────────────────────────────────────
YELLOW='\033[1;33m'; GREEN='\033[0;32m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[SYNC]${NC} $(date '+%H:%M:%S') $*"; }
abort() { echo -e "${RED}[SYNC ERROR]${NC} $*"; exit 1; }

# ── load cluster.conf ─────────────────────────────────────────
conf_file="$PROJ/cluster.conf"
[[ -f "$conf_file" ]] || abort "cluster.conf not found."

_val() { grep -E "^$1=" "$conf_file" | cut -d= -f2 | tr -d '[:space:]'; }

SECONDARY_IP=$(_val SECONDARY_MASTER_IP)
SSH_USER=$(_val SECONDARY_SSH_USER)
SSH_USER=${SSH_USER:-$(whoami)}

[[ -z "$SECONDARY_IP" ]] && { info "No secondary configured, skipping sync."; exit 0; }

REMOTE="${SSH_USER}@${SECONDARY_IP}"
# Remote destination — same project root on secondary device
REMOTE_PROJ="~/xorfs"  # adjust this if needed

info "Syncing checkpoint and WAL to ${REMOTE}:${REMOTE_PROJ} ..."

rsync -avz \
  --timeout=10 \
  master.checkpoint \
  master.wal \
  "${REMOTE}:${REMOTE_PROJ}/" \
  && info "Sync complete." \
  || { echo -e "${YELLOW}[SYNC WARN]${NC} rsync failed (secondary may be down). Continuing."; true; }
