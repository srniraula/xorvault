# XorFS - Distributed File System with RAID-5

A distributed file system implementation in Go featuring RAID-5 erasure coding, client authentication, data integrity verification, and crash recovery through WAL and checkpointing.

## Features

- **RAID-5 Erasure Coding**: Files split into 2 data chunks + 1 parity chunk per stripe (XOR-based)
- **Client Authentication**: Persistent client IDs with ownership verification
- **Physical Isolation**: Client data stored in separate subdirectories on chunk servers
- **Data Integrity**: CRC32 checksums verified at upload, storage, and download
- **Crash Recovery**: Master state persists via Write-Ahead Log (WAL) and periodic checkpointing
- **Automatic Failover**: Parity-based reconstruction when chunks are missing
- **Health Monitoring**: Heartbeat-based chunk server health tracking


## Technical Specs

- **Chunk Size**: 1 MB
- **Stripe**: 2 data chunks + 1 parity chunk (3 MB per stripe)
- **Checksum**: CRC32 (4 bytes)
- **Protocols**: gRPC with Protocol Buffers
- **Language**: Go 1.24.9

## Prerequisites

- Go 1.24+ installed
- Protocol Buffers compiler (`protoc`)
- gRPC tools for Go

## ⭐ Important: Master & Chunkserver Failover

**As of March 2026, XorFS now supports automatic failover:**

✅ **Primary Master** → detects failure of secondary master → continues accepting client operations  
✅ **Secondary Master** → detects heartbeat loss → auto-promotes itself to primary master  
✅ **All Chunkservers** → detect unreachable primary → automatically switch to secondary within 15 seconds  
✅ **All Clients** → detect unreachable primary → automatically retry operations on secondary  

**The `-secondary-master` flag is REQUIRED for all chunkservers to enable failover.** Without it, chunkservers cannot switch to secondary if primary fails.

**Refer to "Master Failover" section below for exact setup and testing commands.**

## Quick Start

### 1. Build Binaries

```bash
make build
```

This creates binaries in `bin/` directory:
- `bin/master` - Master server
- `bin/chunkserver` - Chunk server
- `bin/client` - Client CLI

### 2. Start Primary Master Server

**Important: Start secondary master FIRST (if using failover), then start primary**

```bash
# Example: Mac primary at 192.168.1.87:50051, Kali secondary at 192.168.1.66:50052
make run-master-primary MY_ADDR=192.168.1.87:50051 SECONDARY_ADDR=192.168.1.66:50052
```

### 3. Start Secondary Master Server (for failover support)

**Run this FIRST on the secondary machine:**

```bash
# Example: Kali VM secondary at 192.168.1.66:50052
make run-master-secondary MY_ADDR=192.168.1.66:50052
```

### 4. Start Chunk Servers (in separate terminals)

**CRITICAL: All chunkservers MUST have `-secondary-master` flag, even for basic setup**

```bash
# Terminal 1 - Chunkserver1
make run-chunk_server1 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052

# Terminal 2 - Chunkserver2
make run-chunk_server2 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052

# Terminal 3 - Chunkserver3
make run-chunk_server3 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

Chunk servers run on ports `9001`, `9002`, `9003` with storage in `chunk_server1/`, `chunk_server2/`, `chunk_server3/`

**Verify setup:** Each chunkserver log should show:
```
CHUNKSERVER: ========== CRITICAL: Master failover ENABLED ==========
CHUNKSERVER: Primary master: 192.168.1.87:50051
CHUNKSERVER: Secondary master: 192.168.1.66:50052
```

### 5. Configure Master Addresses for Client (Automatic Failover)

**CRITICAL: This step enables automatic client failover to secondary master**

```bash
# Configure primary and secondary master addresses
# This writes addresses to .master_addr and .secondary_master_addr files
make set-master MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

Expected output:
```
✓ Primary master configured: 192.168.1.87:50051
✓ Secondary master configured: 192.168.1.66:50052

✓✓✓ FAILOVER ENABLED ✓✓✓
    Primary: 192.168.1.87:50051
    Secondary: 192.168.1.66:50052
    Client will auto-failover if primary becomes unreachable.
```

**After this, all client commands will use configured addresses:**
- If primary is reachable: connects to primary (normal operation)
- If primary is unreachable: automatically retries on secondary (transparent failover)

### 6. Upload a File

```bash
make upload FILE=big.pdf
```

This will:
- Generate a client ID (saved to `.client_id`)
- Split file into stripes (2 data + 1 parity per stripe)
- Calculate CRC32 checksums
- Upload chunks in parallel to chunk servers
- Verify integrity on server side

### 7. Download a File

```bash
make download FILE=myfile.pdf
```

Downloaded file saved as `downloaded_myfile.pdf`

### 8. Delete a file
```bash
make delete FILE=myfile.pdf
```

### 9. List uploaded files
```bash
make ls
```

## Makefile Commands Reference

```bash
make build          # Build all binaries
make proto          # Regenerate protobuf files
make run-master     # Start master server (NO failover - backward compatible)
make run-master-primary MY_ADDR=<ip:port> SECONDARY_ADDR=<ip:port>   # Start primary master WITH failover
make run-master-secondary MY_ADDR=<ip:port>                           # Start secondary/standby master
make run-chunk_server1 MASTER_ADDR=<ip:port> SECONDARY_MASTER_ADDR=<ip:port>  # REQUIRES secondary flag for failover
make run-chunk_server2 MASTER_ADDR=<ip:port> SECONDARY_MASTER_ADDR=<ip:port>  # REQUIRES secondary flag for failover
make run-chunk_server3 MASTER_ADDR=<ip:port> SECONDARY_MASTER_ADDR=<ip:port>  # REQUIRES secondary flag for failover
make set-master MASTER_ADDR=<ip:port> SECONDARY_MASTER_ADDR=<ip:port>  # Configure client for failover (CRITICAL!)
make upload FILE=<filename>    # Upload file (uses addresses from set-master)
make download FILE=<filename>  # Download file (uses addresses from set-master)
make delete FILE=<filename>    # Delete file (uses addresses from set-master)
make ls                        # List uploaded files (uses addresses from set-master)
make check-secondary SECONDARY_ADDR=<ip:port>  # Verify secondary master is reachable
make diagnose-failover         # Check if chunkservers have secondary configured
make clean          # Remove binaries and data
make test           # Run tests
```

**Important Notes:**
- `make set-master` MUST be run once before using client commands (upload/download/delete/ls)
- Without `SECONDARY_MASTER_ADDR` in `set-master`, failover is **DISABLED**
- With `SECONDARY_MASTER_ADDR` in `set-master`, client **automatically failovers** if primary unreachable

## How It Works

### Upload Flow

1. **Client** contacts **Master** with filename and size
2. **Master** assigns client ID (first time) and allocates chunks
3. **Client** streams file in stripes:
   - Reads 2 MB chunks (data1, data2)
   - Calculates parity: `data1 ⊕ data2`
   - Computes CRC32 for each chunk
4. **Client** uploads 3 chunks per stripe in parallel
5. **ChunkServers** verify checksums before storing and acknowledges the client.
6. **Client** confirms successful uploads to **Master**

### Download Flow

1. **Client** requests file metadata from **Master**
2. **Master** verifies client ownership
3. **Client** downloads chunks in parallel per stripe
4. If any chunk fails, reconstructs from parity:
   - Missing data1: `data1 = data2 ⊕ parity`
   - Missing data2: `data2 = data1 ⊕ parity`
   - Missing parity: `parity = data1 ⊕ data2`
5. **Client** verifies checksums on received data
6. **Client** writes stripes to output file

### Crash Recovery

**Master persistence:**
1. **WAL (Write-Ahead Log)**: Every operation logged before execution
   - `CreateFile`, `AllocateChunk`, `ConfirmWrite`
2. **Checkpointing**: Full state snapshot every 5 minutes
3. **Recovery**: Load checkpoint → replay WAL → resume operations

On restart, master recovers all metadata (files, stripes, clients, chunks).

## Flexible Deployment Architecture

**Key Design Principle**: All components (master, chunkserver, client) accept **IP addresses via command-line flags or environment variables**. This allows deployment on any number of machines with any configuration.

### Command-Line Arguments

Every binary accepts address arguments:

**Master servers:**
- `-addr <IP:PORT>`: Which address/port to listen on (default: `0.0.0.0:50051`)
- `-secondary <IP:PORT>`: Address of secondary master for replication (optional)

**Chunk servers:**
- `-port <PORT>`: Which port to listen on (default: `9001`)
- `-storage <PATH>`: Where to store chunks (default: `chunks`)
- `-master <IP:PORT>`: Primary master address (required)
- `-secondary-master <IP:PORT>`: Secondary master for failover (optional)

**Clients:**
- Read from `.master_addr` file or `MASTER_ADDR` environment variable
- Read from `.secondary_master_addr` file or `SECONDARY_MASTER_ADDR` environment variable

### Deployment Scenarios

#### Scenario 1: Two Machines - Mac + Kali VM (Testing Setup)
```
Machine 1 (Mac 192.168.1.87):
  Terminal 1: Primary Master on :50051
  Terminal 2: Chunkserver1 on :9001
  Terminal 3: Chunkserver3 on :9003
  Terminal 4: Client (configure with set-master, then upload/download)

Machine 2 (Kali VM 192.168.1.66):
  Terminal 1: Secondary Master on :50052
  Terminal 2: Chunkserver2 on :9002
```

**Exact Setup Commands:**

**Kali VM - Terminal 1 (Start this FIRST):**
```bash
make run-master-secondary MY_ADDR=192.168.1.66:50052
```

**Mac - Terminal 1:**
```bash
make run-master-primary MY_ADDR=192.168.1.87:50051 SECONDARY_ADDR=192.168.1.66:50052
```

**Mac - Terminal 2:**
```bash
make run-chunk_server1 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

**Mac - Terminal 3:**
```bash
make run-chunk_server3 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

**Kali VM - Terminal 2:**
```bash
make run-chunk_server2 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

**Mac - Terminal 4 (Client):**
```bash
# CRITICAL: Set master addresses for client failover
make set-master MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052

# Now all client commands use configured addresses and failover automatically
make upload FILE=myfile.pdf
make ls
make download FILE=myfile.pdf
```

**Verify Setup:**
- Each chunkserver should log: `CRITICAL: Master failover ENABLED`
- Each chunkserver should show: `Heartbeat sent to master 192.168.1.87:50051`
- Client should show: `✓✓✓ FAILOVER ENABLED ✓✓✓`

#### Scenario 2: Three Machines (Production Demo)
```
Machine 1 (Laptop A 192.168.1.87):     Primary Master + Chunkserver1
Machine 2 (Laptop B 192.168.1.66):     Secondary Master + Chunkserver2  
Machine 3 (Laptop C 192.168.1.100):    Chunkserver3 + Client
```

**Exact Setup Commands (change IPs to your machines):**

**Laptop B - Terminal 1 (Secondary Master - START THIS FIRST):**
```bash
make run-master-secondary MY_ADDR=192.168.1.66:50052
```

**Laptop A - Terminal 1 (Primary Master):**
```bash
make run-master-primary MY_ADDR=192.168.1.87:50051 SECONDARY_ADDR=192.168.1.66:50052
```

**Laptop A - Terminal 2 (Chunkserver1):**
```bash
make run-chunk_server1 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

**Laptop B - Terminal 2 (Chunkserver2):**
```bash
make run-chunk_server2 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

**Laptop C - Terminal 1 (Chunkserver3):**
```bash
make run-chunk_server3 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

**Laptop C - Terminal 2 (Client - CRITICAL for failover):**
```bash
# Configure client for automatic failover
make set-master MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052

# All subsequent commands automatically failover if primary fails
make upload FILE=myfile.pdf
make ls
make download FILE=myfile.pdf
```

**Key Points:**
- All IP addresses are configurable - use your actual machine IPs
- No code changes needed across different machines
- Change only the IP addresses in the `make` commands
- Secondary master MUST be started before primary master
- **Client MUST run `make set-master` to enable failover**

### Master Failover - Exact Setup & Testing

The system supports automatic master failover with a primary/secondary pair.

**How it works:**
1. Primary master sends WAL entries to secondary after every write (CreateFile, ConfirmWrite, DeleteFile)
2. Primary sends a heartbeat to secondary every 3 seconds
3. Secondary applies all WAL entries to keep its in-memory state in sync
4. If secondary receives no heartbeat for 10 seconds, it promotes itself to primary
5. Chunkservers auto-switch to secondary after 3 missed heartbeats (~15 seconds)
6. Clients auto-failover when primary is unreachable (using addresses from `make set-master`)

**Exact setup commands (Mac 192.168.1.87 + Kali 192.168.1.66):**

**Step 1: Start Secondary Master FIRST (Kali VM)**
```bash
make run-master-secondary MY_ADDR=192.168.1.66:50052
```
Expected log: 
```
[Secondary Master] Waiting for heartbeats from primary...
```

**Step 2: Start Primary Master (Mac)**
```bash
make run-master-primary MY_ADDR=192.168.1.87:50051 SECONDARY_ADDR=192.168.1.66:50052
```
Expected logs:
```
[Master] Starting primary master...
[Master] My address: 192.168.1.87:50051
[Master] Secondary master: 192.168.1.66:50052
```

**Step 3: Start All Chunkservers (with SECONDARY_MASTER_ADDR flag)**

Mac Terminal 2:
```bash
make run-chunk_server1 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

Mac Terminal 3:
```bash
make run-chunk_server3 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

Kali VM Terminal 2:
```bash
make run-chunk_server2 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

Expected log (all chunkservers):
```
========== CRITICAL: Master failover ENABLED ==========
Primary master: 192.168.1.87:50051
Secondary master: 192.168.1.66:50052
```

**Step 4: Configure Client for Failover (CRITICAL!)**

Mac Terminal 4:
```bash
make set-master MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

Expected output:
```
✓ Primary master configured: 192.168.1.87:50051
✓ Secondary master configured: 192.168.1.66:50052

✓✓✓ FAILOVER ENABLED ✓✓✓
    Primary: 192.168.1.87:50051
    Secondary: 192.168.1.66:50052
    Client will auto-failover if primary becomes unreachable.
```

**Step 5: Upload a File**
```bash
make upload FILE=myfile.pdf
```

**Step 6: Test Failover - Kill Primary Master**

In a new terminal (NOT the one running primary):
```bash
pkill -f "bin/master"
```

**Expected Timeline:**
- **T+5s**: Chunkservers log `failed (1/3)`
- **T+10s**: Chunkservers log `failed (2/3)`
- **T+15s**: Chunkservers log `FAILOVER: switching active master from 192.168.1.87:50051 to 192.168.1.66:50052`
- **T+16s**: Chunkservers log `Post-failover heartbeat to 192.168.1.66:50052 succeeded`
- **Secondary Master Terminal**: Logs `>>> THIS NODE IS NOW THE ACTIVE PRIMARY MASTER <<<`

**Step 7: Verify Client Failover**
```bash
make ls
```
Should succeed (client auto-detects primary failure and retries on secondary configured in step 4)

**Troubleshooting Failover Issues:**

| Symptom | Cause | Solution |
|---------|-------|----------|
| Client operations fail after `set-master` | Forgot to set SECONDARY_MASTER_ADDR | Run `make set-master` with both PRIMARY and SECONDARY addresses |
| Chunkservers log "NO SECONDARY MASTER" | Missing `-secondary-master` flag | Restart with `SECONDARY_MASTER_ADDR=192.168.1.66:50052` |
| Failover doesn't happen | Primary not actually killed (Ctrl+C kills make, not binary) | Use `pkill -f "bin/master"` instead |
| Client can't failover after killling primary | Client doesn't have secondary address from `set-master` | Must run `make set-master` with SECONDARY_MASTER_ADDR before any operations |
| Logs keep showing successful heartbeats | Primary still running on different terminal | Verify with `ps aux \| grep "bin/master"` |

### Client Auto-Failover

Clients automatically failover to secondary master when primary is unreachable.

**How it works:**
1. Client reads master addresses from `.master_addr` and `.secondary_master_addr` files (created by `make set-master`)
2. On first connection attempt, client probes primary master with 2-second timeout
3. If primary is unreachable, client automatically switches to secondary master
4. All subsequent commands use the same master addresses from the files

**To enable client failover - REQUIRED step:**

```bash
# Configure client with both primary and secondary master addresses
make set-master MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
```

This creates two files:
- `.master_addr` - contains primary master address (192.168.1.87:50051)
- `.secondary_master_addr` - contains secondary master address (192.168.1.66:50052)

**Example - Client Operation:**

```bash
# First, configure client (creates address files)
make set-master MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052

# Now all commands automatically use configured addresses and failover
make upload FILE=myfile.pdf
make ls
make download FILE=myfile.pdf
```

**Failover Behavior:**

| Scenario | Behavior |
|----------|----------|
| No `make set-master` run | Client fails immediately (error reading .master_addr file) |
| `set-master` run without SECONDARY_MASTER_ADDR | No failover - if primary fails, client operations fail |
| `set-master` run WITH SECONDARY_MASTER_ADDR | ✅ Client auto-failovers if primary unreachable (2-second timeout) |
| Primary alive | Client uses primary; zero overhead |
| Primary fails or slow | Client waits 2 seconds, logs failure, retries with secondary (transparent failover) |
| Secondary becomes new primary | Operations continue seamlessly with auto-promoted secondary |

**Important:** Without running `make set-master MASTER_ADDR=<primary> SECONDARY_MASTER_ADDR=<secondary>`, client failover is **NOT enabled** and client operations will fail if primary master is unavailable.

### Chunk Server Auto-Failover

Chunkservers automatically monitor the active master and failover when primary is unreachable.

**How it works:**
1. Each chunkserver sends heartbeat to active master every 5 seconds
2. If 3 consecutive heartbeats fail (~15 seconds), it switches to secondary master
3. An immediate post-failover heartbeat re-registers chunkserver with secondary
4. All subsequent heartbeats and operations go to secondary automatically
5. No chunkserver restart is required

**CRITICAL: The `-secondary-master` flag is REQUIRED for failover**

❌ **Without secondary flag (No failover):**
```bash
make run-chunk_server1 MASTER_ADDR=192.168.1.87:50051
# Log: "NO SECONDARY MASTER" - failover DISABLED
# If primary dies, chunkserver keeps retrying primary indefinitely
```

✅ **With secondary flag (Failover enabled):**
```bash
make run-chunk_server1 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.66:50052
# Log: "CRITICAL: Master failover ENABLED"
# If primary dies, failover happens automatically in ~15 seconds
```

**Expected Logs When Failover Happens:**

```
T+0s: Primary master killed
T+5s: CHUNKSERVER: Heartbeat to 192.168.1.87:50051 failed (1/3 consecutive failures)
T+10s: CHUNKSERVER: Heartbeat to 192.168.1.87:50051 failed (2/3 consecutive failures)
T+15s: CHUNKSERVER: Heartbeat to 192.168.1.87:50051 failed (3/3 consecutive failures)
T+15s: CHUNKSERVER: FAILOVER: switching active master from 192.168.1.87:50051 to 192.168.1.66:50052
T+15s: [CHUNKSERVER] FAILOVER: switching active master from 192.168.1.87:50051 to 192.168.1.66:50052
T+16s: CHUNKSERVER: Post-failover heartbeat to 192.168.1.66:50052 succeeded
T+20s+: CHUNKSERVER: Heartbeat sent to master 192.168.1.66:50052 (every 5 seconds)
```

**Failover Behavior Table:**

| Scenario | Behavior |
|---|---|
| No `-secondary-master` flag | Heartbeats retry primary indefinitely (backward compatible) |
| Primary alive | Heartbeats succeed to primary every 5 seconds |
| Primary fails (3+ misses) | Switches to secondary, sends immediate re-registration heartbeat |
| Secondary becomes primary | All operations continue seamlessly on secondary |
| Works on any machine | Failover works identically on Mac, Kali, laptop, or any physical location |

**Verification:** Check chunkserver logs for failover configuration:
```bash
tail log_files/chunkserver.log | grep -i "failover\|critical\|warning"
```

Should show:
```
========== CRITICAL: Master failover ENABLED ==========
Primary master: 192.168.1.87:50051
Secondary master: 192.168.1.66:50052
```

## File Structure

```
dfs-project/
├── cmd/
│   ├── master/
│   │   ├── main.go              # Master startup
│   │   ├── master.go            # Master logic (CreateFile, AllocateChunk, etc.)
│   │   ├── wal_operation.go     # WAL logging
│   │   ├── wal_recovery.go      # WAL replay on startup
│   │   └── checkpoint.go        # Checkpointing system
│   ├── chunkserver/
│   │   ├── main.go              # ChunkServer (WriteChunk, ReadChunk)
│   │   └── checksum.go          # CRC32 calculation
│   └── client/
│       ├── main.go              # Client CLI (upload/download)
│       ├── stripe_reader.go     # File streaming in stripes
│       ├── parallel_upload.go   # Parallel chunk uploads
│       ├── download_stripe.go   # Parallel downloads with reconstruction
│       ├── parity.go            # XOR parity calculation
│       └── checksum.go          # CRC32 checksums
├── dfspb/
│   ├── dfs.pb.go               # Generated protobuf code
│   └── dfs_grpc.pb.go          # Generated gRPC code
├── dfs.proto                   # Protocol definitions
├── Makefile                    # Build and run commands
└── README.md                   # This file
```

## Data Integrity Layers

1. **Client Upload**: Calculate CRC32, send with chunk
2. **ChunkServer Receive**: Verify CRC32 matches, reject if corrupted
3. **Client Download**: Verify CRC32 on received data

All three layers ensure end-to-end integrity.

## Client Authentication

- First upload generates unique client ID (saved to `.client_id`)
- Master tracks which client owns which files
- Downloads require ownership verification
- Physical isolation: chunks stored in `chunk_server/client_id/` directories




# run at client terminal
make set-master MASTER_ADDR=<master_addr:port>
make upload FILE=<filename_inside_files>

# run chunkserver like this

