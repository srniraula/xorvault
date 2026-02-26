#!/bin/bash

# run_all.sh - Start the entire XORFS system (DFS Cluster + Web API + Frontend)

# Color variables for pretty output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

PID_FILE=".sys_pids"

# Function to kill all started processes on exit
cleanup() {
    echo -e "\n${RED}Stopping all XORFS processes...${NC}"
    if [ -f "$PID_FILE" ]; then
        while read pid; do
            if kill -0 "$pid" 2>/dev/null; then
                kill -9 "$pid" 2>/dev/null
                echo "Stopped process $pid"
            fi
        done < "$PID_FILE"
        rm "$PID_FILE"
    fi
    echo -e "${GREEN}Cleanup complete.${NC}"
    exit
}

# Trap SIGINT (Ctrl+C) and SIGTERM
trap cleanup SIGINT SIGTERM

echo -e "${BLUE}=======================================${NC}"
echo -e "${BLUE}      XORFS FULL SYSTEM STARTUP         ${NC}"
echo -e "${BLUE}=======================================${NC}"

# 1. Build project
echo -e "${YELLOW}[1/5] Building Go binaries...${NC}"
make build > /dev/null
if [ $? -ne 0 ]; then
    echo -e "${RED}Build failed! Please check your Go environment.${NC}"
    exit 1
fi

mkdir -p log_files

# 2. Start Master
echo -e "${YELLOW}[2/5] Starting Master Server...${NC}"
./bin/master > log_files/master_stdout.log 2>&1 &
echo $! >> "$PID_FILE"
sleep 2 # Wait for master to write .master_addr

# 3. Start Chunkservers
echo -e "${YELLOW}[3/5] Starting 3 Chunkservers...${NC}"
# Note: they auto-discover master via .master_addr now
./bin/chunkserver -port 9001 -storage chunk_server1 > log_files/cs1_stdout.log 2>&1 &
echo $! >> "$PID_FILE"
./bin/chunkserver -port 9002 -storage chunk_server2 > log_files/cs2_stdout.log 2>&1 &
echo $! >> "$PID_FILE"
./bin/chunkserver -port 9003 -storage chunk_server3 > log_files/cs3_stdout.log 2>&1 &
echo $! >> "$PID_FILE"

# 4. Start Web API
echo -e "${YELLOW}[4/5] Starting Web API (Go Server)...${NC}"
./bin/webserver > log_files/webserver_stdout.log 2>&1 &
echo $! >> "$PID_FILE"

# 5. Start Web Frontend
echo -e "${YELLOW}[5/5] Starting Frontend (React/Vite)...${NC}"
if [ ! -d "web/node_modules" ]; then
    echo -e "${YELLOW}Installing node_modules...${NC}"
    (cd web && npm install > /dev/null 2>&1)
fi

cd web
npm run dev -- --port 5173 --host 0.0.0.0 > ../log_files/frontend_stdout.log 2>&1 &
echo $! >> "../$PID_FILE"
cd ..

echo -e "${GREEN}=======================================${NC}"
echo -e "${GREEN}      SYSTEM READY AND RUNNING!         ${NC}"
echo -e "${GREEN}=======================================${NC}"
echo -e ""
echo -e "${BLUE}  Frontend UI:   ${NC} http://localhost:5173"
echo -e "${BLUE}  Web API:       ${NC} http://localhost:8080"
echo -e "${BLUE}  Master gRPC:   ${NC} localhost:50051"
echo -e ""
echo -e "${YELLOW}  Logs:          ${NC} ./log_files/"
echo -e "${YELLOW}  Stop:          ${NC} Press Ctrl+C to close everything"
echo -e ""

# Keep script alive to monitor PIDs
wait
