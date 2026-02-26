#!/usr/bin/env bash
# =============================================================
# scripts/start_master.sh
# Run this on the PRIMARY MASTER device.
#
# Usage:
#   ./scripts/start_master.sh
#
# The script reads cluster.conf to get the master's IP/Port.
# It writes .master_addr so chunkservers and the web API can
# discover the master dynamically.
# =============================================================

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ="$(dirname "$SCRIPT_DIR")"
cd "$PROJ"

# ── helpers ──────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[MASTER]${NC} $*"; }
warn()  { echo -e "${YELLOW}[MASTER]${NC} $*"; }
abort() { echo -e "${RED}[MASTER] ERROR:${NC} $*"; exit 1; }

# ── load cluster.conf ─────────────────────────────────────────
conf_file="$PROJ/cluster.conf"
[[ -f "$conf_file" ]] || abort "cluster.conf not found. Did you configure it?"

_val() { grep -E "^$1=" "$conf_file" | cut -d= -f2 | tr -d '[:space:]'; }

PRIMARY_IP=$(_val PRIMARY_MASTER_IP)
PRIMARY_PORT=$(_val PRIMARY_MASTER_PORT)
PRIMARY_PORT=${PRIMARY_PORT:-50051}

[[ -z "$PRIMARY_IP" ]] && abort "PRIMARY_MASTER_IP is not set in cluster.conf"

SECONDARY_IP=$(_val SECONDARY_MASTER_IP)
SECONDARY_PORT=$(_val SECONDARY_MASTER_PORT)
SECONDARY_PORT=${SECONDARY_PORT:-50052}

WEB_PORT=$(_val WEB_API_PORT)
WEB_PORT=${WEB_PORT:-8080}

FRONTEND_PORT=$(_val FRONTEND_PORT)
FRONTEND_PORT=${FRONTEND_PORT:-5173}

# ── build ─────────────────────────────────────────────────────
info "Building Go binaries..."
make build > /dev/null

mkdir -p log_files

# ── write .master_addr so everything can discover master ──────
echo "${PRIMARY_IP}:${PRIMARY_PORT}" > .master_addr
info "Wrote .master_addr → ${PRIMARY_IP}:${PRIMARY_PORT}"

# ── start master gRPC server ──────────────────────────────────
info "Starting Master gRPC on ${PRIMARY_IP}:${PRIMARY_PORT} ..."
MASTER_ADDR="${PRIMARY_IP}:${PRIMARY_PORT}" \
  ./bin/master -port "${PRIMARY_PORT}" -mode active \
  > log_files/master_stdout.log 2>&1 &
MASTER_PID=$!
echo "$MASTER_PID" >> .sys_pids
sleep 2

if ! kill -0 "$MASTER_PID" 2>/dev/null; then
  abort "Master failed to start. Check log_files/master_stdout.log"
fi
info "Master running  PID=$MASTER_PID"

# ── start web API ─────────────────────────────────────────────
info "Starting Web API on 0.0.0.0:${WEB_PORT} ..."
WEB_API_PORT="${WEB_PORT}" MASTER_ADDR="${PRIMARY_IP}:${PRIMARY_PORT}" \
  ./bin/webserver > log_files/webserver_stdout.log 2>&1 &
WEB_PID=$!
echo "$WEB_PID" >> .sys_pids
sleep 1
info "Web API running  PID=$WEB_PID"

# ── start frontend ────────────────────────────────────────────
if [[ ! -d "web/node_modules" ]]; then
  warn "Installing frontend node_modules (first time only)..."
  (cd web && npm install > /dev/null 2>&1)
fi

info "Starting Frontend on 0.0.0.0:${FRONTEND_PORT} ..."
export VITE_API_BASE="http://${PRIMARY_IP}:${WEB_PORT}"
(cd web && npm run dev -- --port "${FRONTEND_PORT}" --host 0.0.0.0 \
  > ../log_files/frontend_stdout.log 2>&1) &
FE_PID=$!
echo "$FE_PID" >> .sys_pids

# ── summary ───────────────────────────────────────────────────
echo ""
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo -e "${GREEN}  PRIMARY MASTER READY                  ${NC}"
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo -e "  gRPC  :  ${PRIMARY_IP}:${PRIMARY_PORT}"
echo -e "  Web API: http://${PRIMARY_IP}:${WEB_PORT}"
echo -e "  Web UI:  http://${PRIMARY_IP}:${FRONTEND_PORT}"
echo ""
echo -e "  Logs:    ./log_files/"
echo ""
if [[ -n "$SECONDARY_IP" ]]; then
  echo -e "${YELLOW}  HA: Secondary master expected at${NC} ${SECONDARY_IP}:${SECONDARY_PORT}"
  echo -e "${YELLOW}  Run   ./scripts/sync_to_secondary.sh   to push checkpoint/WAL${NC}"
fi
echo ""
echo -e "  Stop with Ctrl+C  or  make down"

wait
