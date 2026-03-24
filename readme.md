<!-- # XorFS - Distributed File System with RAID-4

A distributed file system implementation in Go featuring RAID-4 erasure coding, client authentication, data integrity verification, and crash recovery through WAL and checkpointing.

## Features

- **RAID-4 Erasure Coding**: Files split into 2 data chunks + 1 parity chunk per stripe (XOR-based)
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

## Quick Start

### 1. Build Binaries

```bash
make build
```

This creates binaries in `bin/` directory:
- `bin/master` - Master server
- `bin/chunkserver` - Chunk server
- `bin/client` - Client CLI

### 2. Start Master Server

```bash
make run-master
```

Runs on `masterIP:50051` and logs to `master.log`

### 3. Start Chunk Servers (in separate terminals)

```bash
# Terminal 1
make run-chunk_server1 MASTER_ADDR=192.168.1.77:50051

# Terminal 2
make make run-chunk_server2 MASTER_ADDR=<master_addr:port>

# Terminal 3
make make run-chunk_server3 MASTER_ADDR=<master_addr:port>
```

Chunk servers run on ports `9001`, `9002`, `9003` with storage in `chunk_server1/`, `chunk_server2/`, `chunk_server3/`

### 4. Upload a File

```bash
# Point to primary (and optionally secondary) master
make set-master MASTER_ADDR=192.168.1.71:50051 SECONDARY_MASTER_ADDR=192.168.1.75:50052
make upload FILE=big.pdf
```

This will:
- Generate a client ID (saved to `.client_id`)
- Split file into stripes
- Calculate CRC32 checksums
- Upload chunks in parallel to chunk servers
- Verify integrity on server side

### 5. Download a File

```bash
make download FILE=myfile.pdf
```

Downloaded file saved as `downloaded_myfile.pdf`

### 6. Delete a file
```bash
make delete FILE=myfile.pdf
```
### 7. List uploaded files
```bash
make ls
```
### 8. Create a folder
```bash
make mkdir FOLDER=documents/photos
```
### 9. List files with details (sizes, timestamps, folders)
```bash
make ls-detailed
# Or list specific folder:
make ls-detailed FOLDER=documents
```
### 10. Move or rename a file
```bash
make mv SRC=myfile.pdf DEST=documents/myfile.pdf
# Or rename:
make mv SRC=oldname.pdf DEST=newname.pdf
```
### 11. Preview file content (cat)
```bash
make cat FILE=readme.txt
```
### 12. Remove empty folder
```bash
make rmdir FOLDER=documents/photos
```

## Makefile Commands

```bash
make build          # Build all binaries
make proto          # Regenerate protobuf files
make run-master     # Start master server
make run-master-primary MY_ADDR=<ip:port> SECONDARY_ADDR=<ip:port>    # Start primary master with failover
make run-master-secondary MY_ADDR=<ip:port> PRIMARY_ADDR=<ip:port>    # Start secondary/standby master (PRIMARY_ADDR = primary's address)
make run-chunk_server1 MASTER_ADDR=<ip:port> [SECONDARY_MASTER_ADDR=<ip:port>]  # port 9001
make run-chunk_server2 MASTER_ADDR=<ip:port> [SECONDARY_MASTER_ADDR=<ip:port>]  # port 9002
make run-chunk_server3 MASTER_ADDR=<ip:port> [SECONDARY_MASTER_ADDR=<ip:port>]  # port 9003
make set-master MASTER_ADDR=<ip:port> [SECONDARY_MASTER_ADDR=<ip:port>] # Configure master addresses
make upload FILE=<filename>    # Upload file
make download FILE=<filename>  # Download file
make delete FILE=<filename>    # Delete file
make ls                        # List uploaded files
make clean          # Remove binaries
make test           # Run tests
```


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

### Master Failover

The system supports automatic master failover with a primary/secondary pair.

**How it works:**
1. Primary master sends WAL entries to secondary after every write (CreateFile, ConfirmWrite, DeleteFile)
2. Primary sends a heartbeat to secondary every 3 seconds
3. Secondary applies all WAL entries to keep its in-memory state in sync
4. If secondary receives no heartbeat for 10 seconds, it promotes itself to primary
5. After promotion, secondary starts accepting all client and chunk server RPCs directly

**How to run with failover (LAN example):**
```bash
# On the secondary machine (e.g. Kali at 192.168.1.20) — start this FIRST
# PRIMARY_ADDR tells the standby who to watch for heartbeats
make run-master-secondary MY_ADDR=192.168.1.20:50052 PRIMARY_ADDR=192.168.1.10:50051

# On the primary machine (e.g. Mac at 192.168.1.10)
make run-master-primary MY_ADDR=192.168.1.10:50051 SECONDARY_ADDR=192.168.1.20:50052

# Chunk servers — pass BOTH master addresses so they auto-failover too
make run-chunk_server1 MASTER_ADDR=192.168.1.10:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
make run-chunk_server2 MASTER_ADDR=192.168.1.10:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
make run-chunk_server3 MASTER_ADDR=192.168.1.10:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052

# Clients — point to primary and secondary so they auto-failover
make set-master MASTER_ADDR=192.168.1.10:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
make upload FILE=myfile.pdf
```

**To test failover:**
1. Start secondary first, then primary
2. Start chunk servers with both master addresses (see above)
3. Upload a file
4. Kill the primary (`Ctrl+C`)
5. Within 10 seconds the secondary terminal prints a failover banner and `[STATUS] ... → ✅ ACTIVE PRIMARY`
6. Within 15 seconds each chunk server terminal prints a `🔴 CHUNKSERVER FAILOVER` banner and its `[STATUS]` lines switch to show the secondary as `SECONDARY (failed-over)`
7. **Client remains connected** — subsequent commands (like `make ls`) will auto-detect the primary failure and retry against the secondary address automatically.

**Note:** Start the secondary BEFORE the primary, so it is ready to receive heartbeats and WAL entries immediately.

### Client Auto-Failover

Clients use a "Retry-on-Failure" strategy to handle master crashes:

- When you run `make set-master MASTER_ADDR=192.168.1.71:50051 SECONDARY_MASTER_ADDR=192.168.1.75:50052`, both addresses are stored.
- On every command (`upload`, `ls`, etc.), the client first attempts to connect to the **Primary** master (192.168.1.71).
- It performs a lightweight connectivity probe (`GetActiveMaster`).
- If the probe fails (unreachable/timed out), it logs the failure and automatically retries the operation against the **Secondary** address (192.168.1.75).
- This ensures seamless operation even if the primary master is completely down, without needing to re-point the client.

| Scenario | Behavior |
204: |---|---|
205: | No `SECONDARY_MASTER_ADDR` | Client fails immediately if primary is down (backward compatible) |
206: | Primary alive | Client connects to primary; zero overhead |
207: | Primary fails | Client logs failover and proceeds with secondary seamlessly |

### Chunk Server Auto-Failover

Chunk servers use a `MasterTracker` to monitor the active master:

- Each chunk server sends a heartbeat to its **active master** every 5 seconds
- If **3 consecutive heartbeats fail** (~15 seconds), it automatically switches
  `activeAddr` to the secondary master address
- An immediate post-failover heartbeat is sent to re-register with the secondary
- All subsequent heartbeats and inventory reports go to the secondary
- No chunk server restart is required

| Scenario | Behaviour |
|---|---|
| No `-secondary-master` flag | Heartbeats keep retrying primary indefinitely (backward compatible) |
| Primary alive | Heartbeats go to primary; counter stays at 0 |
| Primary fails (3 misses) | Switches to secondary, sends immediate registration heartbeat |
| Secondary becomes new primary | Operations continue seamlessly |

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
 -->


# XorFS - Distributed File System with RAID-4

A distributed file system implementation in Go featuring RAID-4 erasure coding, client authentication, data integrity verification, and crash recovery through WAL and checkpointing.

---

## Features

- **RAID-4 Erasure Coding**: Files split into 2 data chunks + 1 parity chunk per stripe (XOR-based)
- **Client Authentication**: Persistent client IDs with ownership verification
- **Physical Isolation**: Client data stored in separate subdirectories on chunk servers
- **Data Integrity**: CRC32 checksums verified at upload, storage, and download
- **Crash Recovery**: Master state persists via Write-Ahead Log (WAL) and periodic checkpointing
- **Automatic Chunk Reconstruction**: Parity-based reconstruction when a chunk server goes down
- **Master Failover**: Primary/secondary master pair with automatic promotion
- **Health Monitoring**: Heartbeat-based chunk server and master health tracking

---

## Technical Specs

- **Chunk Size**: 1 MB
- **Stripe**: 2 data chunks + 1 parity chunk (3 MB per stripe)
- **Checksum**: CRC32 (4 bytes)
- **Protocols**: gRPC with Protocol Buffers
- **Language**: Go 1.24+

---

## Prerequisites

- Go 1.24+ installed
- Protocol Buffers compiler (`protoc`)
- gRPC tools for Go

---

## Quick Start (Single Machine / No Failover)

### 1. Build Binaries

```bash
make build
```

Creates binaries in `bin/`:
- `bin/master` — Master server
- `bin/chunkserver` — Chunk server
- `bin/client` — Client CLI

### 2. Start Master Server

```bash
make run-master
```

Runs on `0.0.0.0:50051` and logs to `log_files/`.

### 3. Start Chunk Servers (3 separate terminals)

```bash
# Terminal 1
make run-chunk_server1 MASTER_ADDR=<master_ip>:50051

# Terminal 2
make run-chunk_server2 MASTER_ADDR=<master_ip>:50051

# Terminal 3
make run-chunk_server3 MASTER_ADDR=<master_ip>:50051
```

Chunk servers run on ports `9001`, `9002`, `9003` with storage in `chunk_server1/`, `chunk_server2/`, `chunk_server3/`.

### 4. Configure Client and Upload a File

```bash
make set-master MASTER_ADDR=<master_ip>:50051
make upload FILE=myfile.pdf
```

This will:
- Generate a client ID (saved to `.client_id`)
- Split file into 1 MB chunks and stripes
- Calculate CRC32 checksums
- Upload chunks in parallel to chunk servers
- Verify integrity on the server side

### 5. Download a File

```bash
make download FILE=myfile.pdf
```

Downloaded file saved as `downloaded_myfile.pdf`.

---

## File Operations

### Delete a file
```bash
make delete FILE=myfile.pdf
```

### List uploaded files
```bash
make ls
```

### Create a folder
```bash
make mkdir FOLDER=documents/photos
```

### List files with details (sizes, timestamps, folders)
```bash
make ls-detailed
# Or list a specific folder:
make ls-detailed FOLDER=documents
```

### Move or rename a file
```bash
make mv SRC=myfile.pdf DEST=documents/myfile.pdf
# Or rename:
make mv SRC=oldname.pdf DEST=newname.pdf
```

### Preview file content
```bash
make cat FILE=readme.txt
```

### Remove an empty folder
```bash
make rmdir FOLDER=documents/photos
```

---

## Master Failover (Primary + Secondary)

XorFS supports automatic master failover using a primary/secondary pair.

### How It Works

1. **Primary** sends WAL entries to the secondary after every write (`CreateFile`, `ConfirmWrite`, `DeleteFile`)
2. **Primary** sends a heartbeat to secondary every 3 seconds
3. **Secondary** applies all WAL entries to keep its in-memory state in sync
4. If secondary receives **no heartbeat for 30 seconds**, it promotes itself to primary
5. After promotion, secondary accepts all client and chunk server RPCs directly
6. **Chunk servers and clients automatically detect failover** and switch to the secondary

### ⚠️ Important: Start Order

**Always start the secondary BEFORE the primary.**

The secondary starts in **standby mode** unconditionally when launched with `run-master-secondary`. It waits for heartbeats from the primary and never self-promotes until the primary goes silent. If you start the primary first and the secondary is not up yet, heartbeats will warn but nothing breaks — the secondary will enter standby correctly once it starts.

### Running with Failover (LAN Example)

**Step 1 — Start the secondary master first** (e.g. Kali VM at `192.168.1.20:50052`):
```bash
make run-master-secondary MY_ADDR=192.168.1.20:50052 PRIMARY_ADDR=192.168.1.10:50051
```

You will see:
```
╔══════════════════════════════════════════════╗
║  ⏳  STANDBY MASTER: 192.168.1.20:50052      ║
║     Watching primary: 192.168.1.10:50051      ║
╚══════════════════════════════════════════════╝
[STATUS] 192.168.1.20:50052 → ⏳ STANDBY  (primary: 192.168.1.10:50051, ...)
```

**Step 2 — Start the primary master** (e.g. Mac at `192.168.1.10:50051`):
```bash
make run-master-primary MY_ADDR=192.168.1.10:50051 SECONDARY_ADDR=192.168.1.20:50052
```

You will see:
```
╔══════════════════════════════════════════════╗
║  ✅  ACTIVE PRIMARY MASTER: 192.168.1.10:50051  ║
║     Standby peer: 192.168.1.20:50052            ║
╚══════════════════════════════════════════════╝
[STATUS] 192.168.1.10:50051 → ✅ ACTIVE PRIMARY  (gen=1, wal_seq=0)
```

**Step 3 — Start chunk servers with both master addresses:**
```bash
make run-chunk_server1 MASTER_ADDR=192.168.1.10:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
make run-chunk_server2 MASTER_ADDR=192.168.1.10:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
make run-chunk_server3 MASTER_ADDR=192.168.1.10:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
```

**Step 4 — Configure client with both addresses:**
```bash
make set-master MASTER_ADDR=192.168.1.10:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
make upload FILE=myfile.pdf
```

### Testing Failover

1. Follow the startup steps above
2. Upload a file: `make upload FILE=myfile.pdf`
3. Kill the primary (`Ctrl+C` on the primary terminal)
4. Within **30 seconds** the secondary promotes itself:
   ```
   [STATUS] 192.168.1.20:50052 → ✅ ACTIVE PRIMARY  (gen=2, wal_seq=...)
   ```
5. Within **~15 seconds** each chunk server switches to the secondary
6. Run `make ls` or `make download FILE=myfile.pdf` — the client auto-retries against the secondary seamlessly

---

## Makefile Commands Reference

Run `make help` to see this interactive guide:

### 📦 BUILD

```bash
make build         # Build all Go binaries (master, chunkserver, client, webserver)
make clean         # Remove binaries, data, and logs
make proto         # Regenerate protobuf files (if you modify dfs.proto)
```

### 🚀 SETUP FOR MULTI-MACHINE DEPLOYMENT

This distributed file system is designed to run across **6 machines** for full redundancy and scalability:

#### MACHINE 1 - Primary Master (Replication + Failover)
```bash
make run-master-primary MY_ADDR=<this-machine-ip>:50051 SECONDARY_ADDR=<secondary-master-ip>:50052
```

**Example:**
```bash
make run-master-primary MY_ADDR=192.168.100.1:50051 SECONDARY_ADDR=192.168.100.2:50052
```

#### MACHINE 2 - Secondary Master (Standby + Auto-Promotion)
> **Important:** Start this FIRST before the primary master
```bash
make run-master-secondary MY_ADDR=<this-machine-ip>:50052 PRIMARY_ADDR=<primary-master-ip>:50051
```

**Example:**
```bash
make run-master-secondary MY_ADDR=192.168.100.2:50052 PRIMARY_ADDR=192.168.100.1:50051
```

#### MACHINE 3 - Chunk Server 1
```bash
make run-chunk_server1 MASTER_ADDR=<primary-master-ip>:50051 SECONDARY_MASTER_ADDR=<secondary-master-ip>:50052 CHUNK_HOST=<this-machine-ip>
```

**Example:**
```bash
make run-chunk_server1 MASTER_ADDR=192.168.100.1:50051 SECONDARY_MASTER_ADDR=192.168.100.2:50052 CHUNK_HOST=192.168.100.3
```

#### MACHINE 4 - Chunk Server 2
```bash
make run-chunk_server2 MASTER_ADDR=<primary-master-ip>:50051 SECONDARY_MASTER_ADDR=<secondary-master-ip>:50052 CHUNK_HOST=<this-machine-ip>
```

**Example:**
```bash
make run-chunk_server2 MASTER_ADDR=192.168.100.1:50051 SECONDARY_MASTER_ADDR=192.168.100.2:50052 CHUNK_HOST=192.168.100.4
```

#### MACHINE 5 - Chunk Server 3
```bash
make run-chunk_server3 MASTER_ADDR=<primary-master-ip>:50051 SECONDARY_MASTER_ADDR=<secondary-master-ip>:50052 CHUNK_HOST=<this-machine-ip>
```

**Example:**
```bash
make run-chunk_server3 MASTER_ADDR=192.168.100.1:50051 SECONDARY_MASTER_ADDR=192.168.100.2:50052 CHUNK_HOST=192.168.100.5
```

#### MACHINE 6 - Webserver (REST API)

1. Create configuration files with master addresses:
```bash
echo 'PRIMARY_MASTER_IP:50051' > .master_addr
echo 'SECONDARY_MASTER_IP:50052' > .secondary_master_addr
```

2. Run the webserver:
```bash
make run-webserver
```

**Example:**
```bash
echo '192.168.100.1:50051' > .master_addr
echo '192.168.100.2:50052' > .secondary_master_addr
make run-webserver
```

#### MACHINE 6 (or separate) - Frontend (Vite Dev Server)
```bash
cd web && npm run dev -- --host 0.0.0.0
```

### 🌐 ACCESS POINTS

| Component | Address |
|-----------|---------|
| Web UI | `http://<webserver-machine>:5173` |
| REST API | `http://<webserver-machine>:8080` |
| Primary Master | `<primary-master-ip>:50051` |
| Secondary Master | `<secondary-master-ip>:50052` |
| Chunk Server 1 | `<chunk-server-1-ip>:9001` |
| Chunk Server 2 | `<chunk-server-2-ip>:9002` |
| Chunk Server 3 | `<chunk-server-3-ip>:9003` |

### 📋 FILE OPERATIONS (Client Commands)

#### Client Configuration
```bash
make set-master MASTER_ADDR=<ip:port> [SECONDARY_MASTER_ADDR=<ip:port>]
```

#### File Operations
```bash
make upload FILE=<filename>              # Upload file
make download FILE=<filename>            # Download file
make delete FILE=<filename>              # Delete file
make ls                                  # List uploaded files
make ls-detailed [FOLDER=<path>]         # List with details
make mkdir FOLDER=<path>                 # Create folder
make rmdir FOLDER=<path>                 # Remove empty folder
make mv SRC=<path> DEST=<path>           # Move or rename file
make cat FILE=<filename>                 # Preview file content
```

### 🧹 CLEANUP

```bash
make clean         # Remove all binaries, data, and logs
```

---

## How It Works

### Upload Flow

1. **Client** contacts **Master** with filename and size
2. **Master** assigns a client ID (first time) and allocates chunks across chunk servers
3. **Client** streams file in stripes:
   - Reads 2 × 1 MB chunks (`data1`, `data2`)
   - Calculates parity: `data1 ⊕ data2`
   - Computes CRC32 checksum for each chunk
4. **Client** uploads all 3 chunks per stripe in parallel to their assigned chunk servers
5. **ChunkServers** verify CRC32 checksums before storing; reject corrupted data
6. **Client** confirms successful uploads to **Master** (updates chunk status from PENDING → SUCCESS)

### Download Flow

1. **Client** requests file metadata from **Master**
2. **Master** verifies client ownership and returns stripe/chunk locations
3. **Client** downloads all chunks in parallel per stripe
4. If any chunk is missing, reconstructs from parity using XOR:
   - **Missing data1**: `data1 = data2 ⊕ parity`
   - **Missing data2**: `data2 = data1 ⊕ parity`
   - **Missing parity**: not needed if both data chunks are present
5. **Client** verifies CRC32 checksums on all received data
6. **Client** writes stripes sequentially to the output file, stripping padding from the last chunk

### Crash Recovery

**Master persistence:**
1. **WAL (Write-Ahead Log)**: Every operation is logged to disk before updating in-memory state — `CreateFile`, `AllocateChunk`, `ConfirmWrite`, `DeleteFile`
2. **Checkpointing**: Full in-memory state snapshot written every 5 minutes
3. **Recovery on restart**: Load latest checkpoint → replay WAL entries after checkpoint → resume normal operation

On restart, the master recovers all metadata: files, stripes, chunk locations, client IDs, and chunk statuses.

### Master Failover

- **Primary** replicates every WAL entry to the secondary synchronously (2s timeout, best-effort)
- **Primary** sends a `SendMasterHeartbeat` RPC to the secondary every 3 seconds
- **Secondary** (started with `run-master-secondary`) begins in **standby mode** unconditionally — it does not try to contact the primary at startup and will never self-promote unless the primary goes silent
- If no heartbeat is received for **30 seconds**, the secondary promotes itself (increments generation counter, sets `isPrimary = true`, starts accepting writes)
- When the old primary restarts, it contacts the peer, detects that the secondary is now primary, pulls a full state sync, and resumes as standby

### Client Auto-Failover

- Both primary and secondary addresses are stored via `make set-master`
- On every command, client probes the primary with a lightweight `GetActiveMaster` RPC
- If the probe fails (unreachable or timed out), client automatically retries the full operation against the secondary
- No client restart or reconfiguration needed

| Scenario | Behavior |
|---|---|
| No `SECONDARY_MASTER_ADDR` set | Client fails if primary is down (backward compatible) |
| Primary alive | Connects to primary with zero overhead |
| Primary unreachable | Logs failover, retries against secondary seamlessly |

### Chunk Server Auto-Failover

- Each chunk server sends a heartbeat to its active master every **5 seconds**
- After **3 consecutive failed heartbeats** (~15 seconds), it switches `activeAddr` to the secondary
- Sends an immediate re-registration heartbeat to the new active master
- All subsequent heartbeats and inventory reports go to the new master
- No chunk server restart needed

| Scenario | Behavior |
|---|---|
| No `-secondary-master` flag | Retries primary indefinitely (backward compatible) |
| Primary alive | Heartbeats go to primary; failure counter stays at 0 |
| Primary fails (3 misses) | Switches to secondary, sends immediate registration heartbeat |
| Secondary becomes new primary | Operations continue seamlessly |

---

## Data Integrity Layers

1. **Client Upload**: CRC32 computed locally and sent alongside each chunk
2. **ChunkServer Receive**: CRC32 verified before writing to disk; rejects corrupted data
3. **ChunkServer Startup Scan**: All stored chunks re-verified; corrupted or unverifiable chunks deleted before inventory report
4. **Client Download**: CRC32 verified on every received chunk before writing to output file

---

## Client Authentication

- First upload generates a unique client ID (saved to `.client_id` in the working directory)
- Master tracks which client owns which files
- Downloads and deletes require matching client ID (ownership verification)
- **Physical isolation**: chunks stored under `chunk_server<N>/<username_or_client_id>/` directories

---

## File Structure

```
dfs-project/
├── cmd/
│   ├── master/
│   │   ├── main.go              # Master startup + role-based failover logic
│   │   ├── master.go            # CreateFile, AllocateChunk, DeleteFile, etc.
│   │   ├── secondary.go         # Standby logic, WatchdogLoop, promotion
│   │   ├── master_helper.go     # Utility helpers
│   │   ├── wal_operation.go     # WAL entry types and AppendWAL
│   │   ├── wal_recovery.go      # WAL replay on startup
│   │   └── checkpoint.go        # Checkpoint save/load + periodic checkpointing
│   ├── chunkserver/
│   │   ├── main.go              # ChunkServer startup + MasterTracker failover
│   │   ├── chunkservertask.go   # WriteChunk, ReadChunk, DeleteChunks
│   │   ├── inventory.go         # Startup inventory scan and report to master
│   │   ├── reconstruction.go    # XOR-based chunk reconstruction from peers
│   │   └── checksum.go          # CRC32 calculation
│   └── client/
│       ├── main.go              # Client CLI — upload, download, ls, mkdir, mv, cat, etc.
│       ├── stripe_reader.go     # File streaming in stripes
│       ├── parallel_upload.go   # Parallel chunk uploads with ACK queue
│       ├── download_stripe.go   # Parallel downloads with parity reconstruction
│       ├── parity.go            # XOR parity calculation and chunk padding
│       └── checksum.go          # CRC32 checksums
├── dfspb/
│   ├── dfs.pb.go               # Generated protobuf code
│   └── dfs_grpc.pb.go          # Generated gRPC stubs
├── pkg/
│   └── config/                 # Shared config helpers (master addr, chunk servers)
├── dfs.proto                   # Protocol definitions
├── Makefile                    # Build and run commands
└── README.md                   # This file
```