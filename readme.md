
#XORVAULT: CLIENT-ISOLATED PARITY-BASED DFS

A distributed file storage system implementation in Go featuring RAID-4 erasure coding, client authentication, data integrity verification, and crash recovery through WAL and checkpointing.

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

### Important: Start Order

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
[STATUS] 192.168.1.10:50051 →  ACTIVE PRIMARY  (gen=1, wal_seq=0)
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

### BUILD

```bash
make build         # Build all Go binaries (master, chunkserver, client, webserver)
make clean         # Remove binaries, data, and logs
make proto         # Regenerate protobuf files (if you modify dfs.proto)
```

### SETUP FOR MULTI-MACHINE DEPLOYMENT

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

### ACCESS POINTS

| Component | Address |
|-----------|---------|
| Web UI | `http://<webserver-machine>:5173` |
| REST API | `http://<webserver-machine>:8080` |
| Primary Master | `<primary-master-ip>:50051` |
| Secondary Master | `<secondary-master-ip>:50052` |
| Chunk Server 1 | `<chunk-server-1-ip>:9001` |
| Chunk Server 2 | `<chunk-server-2-ip>:9002` |
| Chunk Server 3 | `<chunk-server-3-ip>:9003` |

###  FILE OPERATIONS (Client Commands)

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

###  CLEANUP

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
xorfs/
├── cmd/
│   ├── master/
│   │   ├── main.go              # Master startup + role-based failover logic
│   │   ├── master.go            # CreateFile, AllocateChunk, DeleteFile, etc.
│   │   ├── secondary.go         # Standby logic, WatchdogLoop, promotion
│   │   ├── master_helper.go     # Utility helpers
│   │   ├── chunkserver_recovery.go  # Chunk server recovery logic
│   │   ├── folder_operations.go # Folder CRUD operations
│   │   ├── wal_operation.go     # WAL entry types and AppendWAL
│   │   ├── wal_recovery.go      # WAL replay on startup
│   │   └── checkpoint.go        # Checkpoint save/load + periodic checkpointing
│   ├── chunkserver/
│   │   ├── main.go              # ChunkServer startup + MasterTracker failover
│   │   ├── chunkservertask.go   # WriteChunk, ReadChunk, DeleteChunks
│   │   ├── inventory.go         # Startup inventory scan and report to master
│   │   ├── reconstruction.go    # XOR-based chunk reconstruction from peers
│   │   └── checksum.go          # CRC32 calculation + verification
│   ├── client/
│   │   ├── main.go              # Client CLI — upload, download, ls, mkdir, mv, cat, etc.
│   │   ├── stripe_reader.go     # File streaming in stripes
│   │   ├── stripe.go            # Stripe data structure and utilities
│   │   ├── parallel_upload.go   # Parallel chunk uploads with ACK queue
│   │   ├── download_stripe.go   # Parallel downloads with parity reconstruction
│   │   ├── parity.go            # XOR parity calculation and chunk padding
│   │   ├── checksum.go          # CRC32 checksums
│   │   ├── metrics.go           # Performance metrics collection
│   │   ├── ack_queue.go         # ACK queue management for uploads
│   │   └── client_id.go         # Client ID management
│   └── webserver/
│       ├── main.go              # REST API server and static file serving
│       ├── main_test.go         # API tests
│       └── metrics.go           # Metrics collection and reporting (JSON/CSV)
├── pkg/
│   ├── auth/
│   │   ├── handlers.go          # Authentication HTTP handlers
│   │   ├── jwt.go               # JWT token generation and validation
│   │   ├── middleware.go        # Auth middleware
│   │   ├── password.go          # Password hashing and verification
│   │   ├── storage.go           # User credential storage
│   │   └── types.go             # Auth type definitions
│   ├── config/
│   │   └── config.go            # Configuration helpers (master addr, etc.)
│   ├── dfsclient/
│   │   ├── dfsclient.go         # High-level DFS client wrapper
│   │   ├── ack_queue.go         # ACK queue for upload acknowledgments
│   │   ├── ack_queue_test.go    # ACK queue tests
│   │   ├── checksum.go          # CRC32 verification
│   │   ├── checksum_test.go     # Checksum tests
│   │   ├── conn_pool.go         # Connection pooling for gRPC
│   │   ├── stripe.go            # Stripe structure and operations
│   │   ├── stripe_test.go       # Stripe tests
│   │   ├── stripe_reader.go     # Stripe reader implementation
│   │   ├── stripe_reader_test.go # Stripe reader tests
│   │   ├── parallel_upload.go   # Parallel upload logic
│   │   ├── parallel_upload_test.go # Upload tests
│   │   ├── download_stripe.go   # Stripe download with reconstruction
│   │   ├── download_stripe_test.go # Download tests
│   │   ├── failover_client.go   # Master failover client logic
│   │   ├── stats.go             # Statistics collection
│   │   └── userlogger.go        # User action logging
│   └── webserver/
│       ├── analysis.go          # Metrics analysis and aggregation
│       ├── webserver_logger.go  # Logging for web requests
│       ├── webserver_logger_test.go # Logger tests
│       └── types.go             # Type definitions for web API
├── web/
│   ├── package.json             # Frontend dependencies
│   ├── index.html               # HTML entry point
│   ├── main.jsx                 # Vite entry point
│   ├── App.jsx                  # React app component
│   ├── components/
│   │   ├── Auth.jsx             # Authentication component
│   │   ├── Auth.css             # Auth styles
│   │   └── FileUpload.jsx       # File upload component
│   ├── pages/
│   │   ├── Files.jsx            # File management page
│   │   └── MetricsDashboard.jsx # Performance metrics dashboard
│   └── utils/
│       └── ChunkedUploader.js   # Chunked file upload utility
├── dfspb/
│   ├── dfs.pb.go               # Generated protobuf code
│   └── dfs_grpc.pb.go          # Generated gRPC stubs
├── data/
│   └── users.json              # User credentials (local storage)
├── files/
│   └── README.md               # Sample files directory
├── log_files/
│   └── readme.md               # Logs directory
├── dfs.proto                   # Protocol definitions
├── go.mod                      # Go module definition
├── Makefile                    # Build and run commands
└── readme.md                   # This file
```
