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
make run-master-primary MY_ADDR=<ip:port> SECONDARY_ADDR=<ip:port>   # Start primary master with failover
make run-master-secondary MY_ADDR=<ip:port>                           # Start secondary/standby master
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
make run-master-secondary MY_ADDR=192.168.1.20:50052

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
5. Within 10 seconds the secondary prints: `>>> THIS NODE IS NOW THE ACTIVE PRIMARY MASTER <<<`
6. Within 15 seconds each chunk server prints: `[CHUNKSERVER] FAILOVER: switching active master from <primary> to <secondary>`
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

