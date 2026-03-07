# 📖 XORFS — Distributed File System: The Complete Manual

Welcome to the comprehensive guide for **XorFS**, a high-performance, fault-tolerant distributed file system built from the ground up in Go. This manual covers the architecture, features, implementation logic, performance factors, and step-by-step deployment instructions.

---

## 1. 🚀 System Overview

**XorFS** (XOR File System) is a distributed storage solution designed for reliability and high availability. It uses **RAID-5 (XOR-based erasure coding)** to provide data redundancy with minimal storage overhead.

### Key Architecture Components
| Component | Role |
| :--- | :--- |
| **Primary Master** | The "brain": manages file metadata, user auth, chunk allocation, and WAL. |
| **Secondary Master** | Standby replica that monitors the primary and **auto-promotes** within ~6 seconds of failure. |
| **Chunk Servers** | Storage nodes where actual chunk data and RAID-5 parity are persisted on disk. |
| **Web API** | REST gateway (Gin) that bridges the React frontend to the gRPC master. |
| **React Frontend** | Browser-based dashboard for upload/download, user auth, and cluster health view. |
| **DFS Client (CLI)** | Command-line tool for `upload`, `download`, `ls`, `mkdir`, `rmdir`, `mv`, `cat`. |

---

## 2. ✨ Features

### Reliability & Fault Tolerance
- [x] **RAID-5 Erasure Coding** — Stripe-based storage (2 Data + 1 Parity). Survives the loss of **1 storage node** per stripe.
- [x] **CRC32 Checksums** — Data integrity verified at every step (client → server, server → disk, disk → client).
- [x] **Odd-Chunk Edge Case** — Correctly handles files where the last stripe has only one data chunk (zero-padded parity).

### High Availability (HA)
- [x] **Automatic Failover** — Secondary Master detects primary failure and promotes itself within **~6 seconds**.
- [x] **Incremental WAL Sync** — Standby master continuously polls and replays the primary's Write-Ahead Log to stay in-sync.
- [x] **Active Master Discovery** — Chunk servers re-read `.master_addr` every 5 s and automatically reconnect to the promoted secondary.
- [x] **Real LAN IP Advertisement** — Masters write their actual LAN IP to `.master_addr` / `.secondary_addr` (not `127.0.0.1`) so remote devices can discover them.

### Advanced File Management
- [x] **Hierarchical Folders** — Full directory support (`mkdir`, `rmdir`, `mv` into sub-folders).
- [x] **File Preview (`cat`)** — Stream file content without a full download.
- [x] **Detailed Metadata** — Tracks file sizes, ownership, and upload timestamps.

---

## 3. 🛠️ Technologies

| Technology | Role in XORFS |
| :--- | :--- |
| **Go (Golang)** | Core backend language. Goroutines handle massive concurrency cheaply. |
| **gRPC & Protobuf** | Communication backbone. Binary protocol for maximum speed and type safety. |
| **RAID-5 (XOR)** | Erasure coding: 1-node fault tolerance with only 50% storage overhead. |
| **WAL + Checkpoint** | Durability guarantee. Every write is journaled; snapshots speed up recovery. |
| **Gin Gonic** | Web framework powering the REST Web API. |
| **React + Vite** | Modern frontend stack for the user dashboard. |
| **Makefile** | Orchestration for multi-device LAN deployments. |

---

## 4. 🧠 Implementation Deep Dive

### 4.1 RAID-5 Implementation
XorFS splits files into **1 MB chunks**. For every two data chunks (`D1`, `D2`), a parity chunk (`P`) is computed:

```
P = D1 ⊕ D2
```

- **Upload**: Client stripes the file, calculates XOR in-memory, and uploads `D1`, `D2`, `P` to **three different chunk servers in parallel**.
- **Download**: Client fetches all three. If one server is offline, it reconstructs the missing chunk:
  `D1 = D2 ⊕ P`  or  `D2 = D1 ⊕ P`.

### 4.2 High Availability & Failover — How It Really Works

```
┌─ Secondary Master ────────────────────────────────────┐
│  1. Pings Primary every 2 s via gRPC Ping()           │
│  2. After 3 consecutive failures (~6 s) → Promote()   │
│  3. Performs final incremental WAL catch-up            │
│  4. Sets IsStandby = false → starts accepting writes  │
│  5. Writes own LAN IP:port to .master_addr            │
└───────────────────────────────────────────────────────┘

┌─ Chunk Servers ───────────────────────────────────────┐
│  Every 5 s heartbeat ticker:                          │
│    - If connection lost → re-read .master_addr        │
│    - Try secondary address as fallback                │
│    - Reconnect to whichever master responds           │
└───────────────────────────────────────────────────────┘
```

**Key WAL guarantee**: The secondary's WAL poller reads the shared WAL file incrementally. On promotion it does one final catch-up pass before flipping to active, so **no committed data is ever lost**.

### 4.3 Address Discovery Chain (LAN-aware)

When any component needs to find the master it walks this priority chain:

```
1. MASTER_ADDR env var  (set by Makefile — most reliable)
2. cluster.conf file    (optional static config)
3. .master_addr file    (written by active primary, updated on failover)
4. Auto-detected LAN IP (fallback for single-machine dev)
```

---

## 5. 📊 Performance Factors & Measurements

### 5.1 Performance Factors
1. **Stripe Size (1 MB)** — Larger chunks reduce metadata RPC overhead.
2. **Parallelism** — 3 chunks per stripe uploaded concurrently; bottleneck is NIC bandwidth.
3. **XOR Overhead** — Bitwise XOR is near-zero CPU cost even on large files.
4. **Network Latency** — In a LAN the primary bottleneck is link speed (100 Mbps Wi-Fi or 1 Gbps Ethernet).

### 5.2 How to Measure Performance
```bash
# Throughput — measure upload speed
time make upload FILE=big_file.iso
# Result: (File Size MB) / (seconds) = MB/s

# Failover Time
# Kill the primary master, then watch secondary log:
tail -f log_files/secondary_stdout.log
# Expected line: "Server is now ACTIVE and accepting write requests."
# Typical elapsed: 6 seconds

# Fault Tolerance Test
# Disconnect one chunk server while downloading — client auto-reconstructs.
```

---

## 6. 🏁 Step-by-Step LAN Deployment Guide

This section walks you through a **full 5-device deployment** (or fewer — just run multiple slots on the same machine).

```
Device A  →  Primary Master  (IP: 192.168.1.77)   ports: 50051 (gRPC), 8080 (Web API), 5173 (UI)
Device B  →  Secondary Master (IP: 192.168.1.75)   port:  50052 (gRPC)
Device C  →  Chunk Server slot 1  (any LAN device)  port:  9001
Device D  →  Chunk Server slot 2  (any LAN device)  port:  9002
Device E  →  Chunk Server slot 3  (any LAN device)  port:  9003
```

> **Find your device's LAN IP**: run `ip a` or `hostname -I | awk '{print $1}'`

---

### 6.1 Prerequisites
1. **Go 1.21+** installed on all devices.
2. **Node.js 18+** installed on Device A (frontend build).
3. All devices on the **same LAN / Wi-Fi network**.
4. Firewall ports open: `50051`, `50052`, `9001`, `9002`, `9003`, `8080`, `5173`.
5. Project cloned (`git clone …`) on **every device** that will run a service.

```bash
# Quick firewall open (Kali / Debian)
sudo ufw allow 50051/tcp 50052/tcp 9001/tcp 9002/tcp 9003/tcp 8080/tcp 5173/tcp
```

---

### 6.2 Step 1 — Start the Primary Master (Device A)

Run on **Device A** (IP `192.168.1.77`). This single command starts:
- Master gRPC server on port `50051`
- Web REST API on port `8080`
- React frontend on port `5173`

```bash
# On Device A:
make run-master-lan MASTER=192.168.1.77:50051 SECONDARY=192.168.1.75:50052
``` 

**What happens**:
- Builds all binaries.
- Writes `192.168.1.77:50051` to `.master_addr` (LAN-reachable, not `127.0.0.1`).
- Starts master, web API, and frontend in the background.
- Logs go to `log_files/master_stdout.log`, `log_files/webserver_stdout.log`, `log_files/frontend_stdout.log`.

**Verify**:
```bash
tail -f log_files/master_stdout.log
# Expected: "Master running on :50051 (Mode: active)"
cat .master_addr
# Expected: 192.168.1.77:50051
```

---

### 6.3 Step 2 — Start the Secondary (Standby) Master (Device B)

Run on **Device B** (IP `192.168.1.75`). The secondary **blocks in the foreground** and monitors the primary.

```bash
# On Device B:
make run-secondary-lan MASTER=192.168.1.77:50051 SECONDARY=192.168.1.75:50052
```

**What happens**:
- Builds binaries.
- Writes `192.168.1.75:50052` to `.secondary_addr`.
- Starts in **standby mode** — monitors primary with Ping every 2 s.
- Logs go to `log_files/secondary_stdout.log`.

**Verify**:
```bash
tail -f log_files/secondary_stdout.log
# Expected: "Starting monitoring of Primary Master at 192.168.1.77:50051"
```

---

### 6.4 Step 3 — Start Chunk Servers (Devices C, D, E)

Each chunk server registers itself with the master via heartbeat. Run one command per device. **Do not pass `MY_IP` — the server auto-detects its own LAN IP.**

```bash
# On Device C (Chunk Server slot 1):
make run-chunk-lan SLOT=1 MASTER=192.168.1.77:50051 SECONDARY=192.168.1.75:50052

# On Device D (Chunk Server slot 2):
make run-chunk-lan SLOT=2 MASTER=192.168.1.77:50051 SECONDARY=192.168.1.75:50052

# On Device E (Chunk Server slot 3):
make run-chunk-lan SLOT=3 MASTER=192.168.1.77:50051 SECONDARY=192.168.1.75:50052
```

> **If you only have 2 extra devices**: run slot 1 on Device A and slot 2 on Device B in new terminal tabs, with slot 3 on Device E.

**Verify** (on master, Device A):
```bash
tail -f log_files/master_stdout.log | grep "registered\|Heartbeat"
# Expected: "New chunkserver registered: 192.168.1.XX:900Y"
#           "Heartbeat received from 192.168.1.XX:900Y"
```
> ⚠️ If you see `Heartbeat received from :9001` (no IP), the chunk server auto-detected a wrong interface. Fix it by adding `MY_IP=<this-device-ip>` to the make command.

---

### 6.5 Step 4 — Register a Client & Use the System

From **any device with the project** cloned:

```bash
# Set the master address for this machine's client
echo "192.168.1.77:50051" > .master_addr

# Register yourself (creates a username + 6-digit PIN)
make register

# Upload a file
make upload FILE=myfile.pdf

# List your files  
make ls

# Create a folder and move a file into it
make mkdir FOLDER=docs
make mv SRC=myfile.pdf DEST=docs/myfile.pdf

# Preview a text file without downloading it
make cat FILE=docs/readme.txt

# Download a file
make download FILE=docs/myfile.pdf
```

### 6.6 Using the Web Dashboard

Open in any browser on the network:
```
http://192.168.1.77:5173
```
- **Login** with your username and PIN.
- **Upload / Download** files with drag-and-drop.
- **Dashboard** shows live chunk server health and storage stats.

---

## 7. 🔥 Failover Demo — Testing the Secondary Master

This is the most impressive demo to show your supervisor.

```bash
# Terminal 1 on Device A — watch the primary
tail -f log_files/master_stdout.log

# Terminal 2 on Device B — watch the secondary
tail -f log_files/secondary_stdout.log

# ── Kill the primary ──────────────────────────────────
# On Device A, find and kill the master process:
make down
# or: kill -9 $(cat .sys_pids | head -1)

# ── Watch Device B's log ──────────────────────────────
# Within ~6 seconds you should see:
#   "Primary ping failed (attempt 1/3)"
#   "Primary ping failed (attempt 2/3)"
#   "Primary ping failed (attempt 3/3)"
#   "!!! PRIMARY FAILURE DETECTED - PROMOTING TO ACTIVE !!!"
#   "Updated .master_addr → 192.168.1.75:50052"
#   "Server is now ACTIVE and accepting write requests."

# ── Chunk servers auto-reconnect ──────────────────────
# Within one heartbeat cycle (5 s) they re-read .master_addr
# and reconnect to 192.168.1.75:50052.

# ── Client auto-follows ───────────────────────────────
# Any subsequent make upload / download will succeed
# because the client re-reads .master_addr on every operation.
```

---

## 8. 🛠️ Troubleshooting

| Symptom | Cause | Fix |
| :--- | :--- | :--- |
| `Heartbeat received from :9003` (no IP) | `MY_IP` not passed; auto-detect picked loopback | Add `MY_IP=<this-device-ip>` to the `make run-chunk-lan` command |
| `no route to host` in inventory check | Old `cluster.conf` or stale `.master_addr` file | Delete `.master_addr` on remote device; the flags now take priority |
| `address already in use` on port 9003 | Previous process suspended (`Ctrl+Z`) still holds the port | `lsof -i :9003` then `kill -9 <PID>` |
| Secondary never promotes | Firewall blocking port `50051` on Device A from Device B | `sudo ufw allow 50051/tcp` on Device A |
| `bind: address already in use` on master | Master already running from a previous session | `make down` to kill all tracked processes |
| Client can't find master | `.master_addr` points to old IP | `echo "192.168.1.77:50051" > .master_addr` |
| Web UI shows no chunk servers | Chunk servers registered with `:PORT` (no IP) | Restart chunk servers with `MY_IP=<actual-ip>` flag |

---

## 9. 🔧 Stopping the System

```bash
# Stop all background services (master, web API, frontend) started by run-master-lan
make down

# Stop individual processes manually
cat .sys_pids          # List all tracked PIDs
kill -9 <pid>          # Kill a specific one
```

---

## 10. 📁 Project Structure

```
xorfs/
├── cmd/
│   ├── master/          # Primary & Secondary master server
│   ├── chunkserver/     # Chunk storage node (RAID-5 r/w + heartbeat)
│   ├── client/          # CLI client (upload/download/ls/mkdir/…)
│   └── webserver/       # REST Web API (Gin) → proxies to master gRPC
├── dfspb/               # Generated protobuf/gRPC stubs
├── pkg/config/          # LAN address discovery (env → cluster.conf → .master_addr → auto-IP)
├── web/                 # React + Vite frontend
├── log_files/           # Runtime logs (gitignored)
├── Makefile             # All deployment targets
└── MANUAL.md            # You are here
```

---

**XorFS Project — Distributed Systems 2025/2026**
