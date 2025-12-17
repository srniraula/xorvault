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
make run-chunk_server1 MASTER_ADDR=<master_addr:port>

# Terminal 2
make make run-chunk_server2 MASTER_ADDR=<master_addr:port>

# Terminal 3
make make run-chunk_server3 MASTER_ADDR=<master_addr:port>
```

Chunk servers run on ports `9001`, `9002`, `9003` with storage in `chunk_server1/`, `chunk_server2/`, `chunk_server3/`

### 4. Upload a File

```bash
make set-master MASTER_ADDR=<master_addr:port>
make upload FILE=myfile.pdf
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

## Makefile Commands

```bash
make build          # Build all binaries
make proto          # Regenerate protobuf files
make run-master     # Start master server
make run-chunk_server1     # Start chunk server 1 (port 9001)
make run-chunk_server2     # Start chunk server 2 (port 9002)
make run-chunk_server3     # Start chunk server 3 (port 9003)
make upload FILE=<filename>    # Upload file
make download FILE=<filename>  # Download file
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

