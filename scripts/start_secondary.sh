#!/usr/bin/env bash
# =============================================================
# scripts/start_secondary.sh
# Run this on the SECONDARY MASTER device.
#
# The secondary stays in standby mode, tailing the WAL from the
# primary.  If the primary dies, this process promotes itself
# and writes .master_addr so chunk servers follow automatically.
#
# Pre-requisites:
#   1. Copy the project folder (or ssh/rsync it) to this device.
#   2. Edit cluster.conf with the real LAN IPs.
#   3. Run this script.
#
# HA sync:  checkpoint & WAL files should be rsynced from the
#           primary periodically (see scripts/sync_to_secondary.sh).
# =============================================================

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ="$(dirname "$SCRIPT_DIR")"
cd "$PROJ"

# ── helpers ──────────────────────────────────────────────────
BLUE='\033[0;34m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${BLUE}[SECONDARY]${NC} $*"; }
warn()  { echo -e "${YELLOW}[SECONDARY]${NC} $*"; }
abort() { echo -e "${RED}[SECONDARY] ERROR:${NC} $*"; exit 1; }

# ── load cluster.conf ─────────────────────────────────────────
conf_file="$PROJ/cluster.conf"
[[ -f "$conf_file" ]] || abort "cluster.conf not found."

_val() { grep -E "^$1=" "$conf_file" | cut -d= -f2 | tr -d '[:space:]'; }

PRIMARY_IP=$(_val PRIMARY_MASTER_IP)
PRIMARY_PORT=$(_val PRIMARY_MASTER_PORT)
PRIMARY_PORT=${PRIMARY_PORT:-50051}

SECONDARY_IP=$(_val SECONDARY_MASTER_IP)
SECONDARY_PORT=$(_val SECONDARY_MASTER_PORT)
SECONDARY_PORT=${SECONDARY_PORT:-50052}

[[ -z "$SECONDARY_IP" ]] && abort "SECONDARY_MASTER_IP is not set in cluster.conf"
[[ -z "$PRIMARY_IP"   ]] && abort "PRIMARY_MASTER_IP is not set in cluster.conf"

# ── build ─────────────────────────────────────────────────────
info "Building Go binaries..."
make build > /dev/null

mkdir -p log_files

# ── write our own address for discovery ───────────────────────
echo "${SECONDARY_IP}:${SECONDARY_PORT}" > .secondary_addr
info "Wrote .secondary_addr → ${SECONDARY_IP}:${SECONDARY_PORT}"

# ── start secondary master in standby mode ────────────────────
info "Starting Secondary (Standby) Master on port ${SECONDARY_PORT} ..."
info "Monitoring primary at ${PRIMARY_IP}:${PRIMARY_PORT} ..."

export SECONDARY_MASTER_IP="${SECONDARY_IP}"
MASTER_ADDR="${SECONDARY_IP}:${SECONDARY_PORT}" \
  ./bin/master \
    -port "${SECONDARY_PORT}" \
    -mode standby \
    -primary "${PRIMARY_IP}:${PRIMARY_PORT}" \
  > log_files/secondary_stdout.log 2>&1 &

SEC_PID=$!
echo "$SEC_PID" >> .sys_pids
sleep 2

if ! kill -0 "$SEC_PID" 2>/dev/null; then
  abort "Secondary master failed to start. Check log_files/secondary_stdout.log"
fi

echo ""
echo -e "${BLUE}════════════════════════════════════════${NC}"
echo -e "${BLUE}  SECONDARY MASTER (STANDBY) READY      ${NC}"
echo -e "${BLUE}════════════════════════════════════════${NC}"
echo -e "  Listening:  ${SECONDARY_IP}:${SECONDARY_PORT}"
echo -e "  Monitoring: ${PRIMARY_IP}:${PRIMARY_PORT}"
echo ""
echo -e "  Logs: ./log_files/secondary_stdout.log"
echo -e "  On primary failure, this node promotes itself automatically."
echo ""
echo -e "  Stop with Ctrl+C"

wait
