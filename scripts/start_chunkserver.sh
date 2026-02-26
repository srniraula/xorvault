#!/usr/bin/env bash
# =============================================================
# scripts/start_chunkserver.sh
# Run this on a CHUNKSERVER device (one per team member).
#
# Usage:
#   ./scripts/start_chunkserver.sh <slot>
#
# <slot> is 1, 2, or 3 — which chunk server entry in cluster.conf
# this device should use.
#
# Example (member 1 device):
#   ./scripts/start_chunkserver.sh 1
#
# Pre-requisites:
#   1. Copy the project folder to this device (or clone from git).
#   2. Edit cluster.conf — fill in the correct IP for this slot.
#   3. Ensure the primary master IP is also correct in cluster.conf.
#   4. Run this script.
# =============================================================

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ="$(dirname "$SCRIPT_DIR")"
cd "$PROJ"

# ── helpers ──────────────────────────────────────────────────
CYAN='\033[0;36m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${CYAN}[CHUNK-$SLOT]${NC} $*"; }
warn()  { echo -e "${YELLOW}[CHUNK-$SLOT]${NC} $*"; }
abort() { echo -e "${RED}[CHUNK-$SLOT] ERROR:${NC} $*"; exit 1; }

# ── slot argument ─────────────────────────────────────────────
SLOT="${1:-}"
if [[ -z "$SLOT" || "$SLOT" -lt 1 || "$SLOT" -gt 9 ]]; then
  echo "Usage: $0 <slot>   (slot = 1, 2, or 3)"
  exit 1
fi

# ── load cluster.conf ─────────────────────────────────────────
conf_file="$PROJ/cluster.conf"
[[ -f "$conf_file" ]] || abort "cluster.conf not found."

_val() { grep -E "^$1=" "$conf_file" | cut -d= -f2 | tr -d '[:space:]'; }

PRIMARY_IP=$(_val PRIMARY_MASTER_IP)
PRIMARY_PORT=$(_val PRIMARY_MASTER_PORT)
PRIMARY_PORT=${PRIMARY_PORT:-50051}
[[ -z "$PRIMARY_IP" ]] && abort "PRIMARY_MASTER_IP not set in cluster.conf"

SECONDARY_IP=$(_val SECONDARY_MASTER_IP)
SECONDARY_PORT=$(_val SECONDARY_MASTER_PORT)
SECONDARY_PORT=${SECONDARY_PORT:-50052}
# Secondary is optional, so we don't abort if it's missing

MY_IP=$(_val "CHUNK_SERVER_${SLOT}_IP")
MY_PORT=$(_val "CHUNK_SERVER_${SLOT}_PORT")
[[ -z "$MY_IP"   ]] && abort "CHUNK_SERVER_${SLOT}_IP not set in cluster.conf"
[[ -z "$MY_PORT" ]] && abort "CHUNK_SERVER_${SLOT}_PORT not set in cluster.conf"

# ── build ─────────────────────────────────────────────────────
info "Building Go binaries..."
make build > /dev/null

mkdir -p log_files
STORAGE_DIR="chunk_server${SLOT}"
mkdir -p "$STORAGE_DIR"

# ── start chunkserver ─────────────────────────────────────────
info "Starting Chunk Server ${SLOT} on ${MY_IP}:${MY_PORT} ..."
info "Reporting to master: ${PRIMARY_IP}:${PRIMARY_PORT}"

# CHUNKSERVER_ADDR tells config.GetMyAddr() to use our real LAN IP
SECONDARY_ARG=""
if [[ -n "$SECONDARY_IP" ]]; then
  SECONDARY_ARG="-secondary ${SECONDARY_IP}:${SECONDARY_PORT}"
fi

CHUNKSERVER_ADDR="${MY_IP}:${MY_PORT}" \
  ./bin/chunkserver \
    -port "${MY_PORT}" \
    -storage "${STORAGE_DIR}" \
    -master "${PRIMARY_IP}:${PRIMARY_PORT}" \
    ${SECONDARY_ARG} \
  > "log_files/cs${SLOT}_stdout.log" 2>&1 &

CS_PID=$!
echo "$CS_PID" >> .sys_pids
sleep 1

if ! kill -0 "$CS_PID" 2>/dev/null; then
  abort "Chunk Server ${SLOT} failed to start. Check log_files/cs${SLOT}_stdout.log"
fi

echo ""
echo -e "${CYAN}════════════════════════════════════════${NC}"
echo -e "${CYAN}  CHUNK SERVER ${SLOT} READY                 ${NC}"
echo -e "${CYAN}════════════════════════════════════════${NC}"
echo -e "  Listening: ${MY_IP}:${MY_PORT}"
echo -e "  Storage:   ./${STORAGE_DIR}/"
echo -e "  Master:    ${PRIMARY_IP}:${PRIMARY_PORT}"
echo ""
echo -e "  Logs: ./log_files/cs${SLOT}_stdout.log"
echo -e "  Stop with Ctrl+C"

wait
