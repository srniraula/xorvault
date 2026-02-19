# XorFS — Distributed File System: Complete Project Documentation

> **Project Language:** Go (Golang)  
> **Protocol:** gRPC + Protocol Buffers  
> **Core Storage Model:** RAID-5 (XOR-based Erasure Coding)  
> **High Availability:** Primary–Secondary Master with Automatic Failover  
> **Persistence:** Write-Ahead Log (WAL) + Periodic Checkpointing

---

## Table of Contents

1. [Project Overview](#1-project-overview)
2. [System Architecture](#2-system-architecture)
3. [Feature List](#3-feature-list)
4. [Component Deep Dive](#4-component-deep-dive)
   - 4.1 [Master Server](#41-master-server)
   - 4.2 [Chunk Server](#42-chunk-server)
   - 4.3 [Client](#43-client)
5. [RAID-5 Implementation](#5-raid-5-implementation)
6. [WAL and Crash Recovery](#6-wal-and-crash-recovery)
7. [High Availability — Secondary Master Failover](#7-high-availability--secondary-master-failover)
8. [Data Integrity — CRC32 Checksums](#8-data-integrity--crc32-checksums)
9. [Client Authentication and Ownership](#9-client-authentication-and-ownership)
10. [Folder and File Management](#10-folder-and-file-management)
11. [gRPC and Protocol Buffers](#11-grpc-and-protocol-buffers)
12. [Go Language Features Used](#12-go-language-features-used)
13. [Complete Data Flow Diagrams](#13-complete-data-flow-diagrams)
14. [Project File Structure](#14-project-file-structure)
15. [Build and Run Commands](#15-build-and-run-commands)

---

## 1. Project Overview

**XorFS** is a distributed file system built from scratch in Go. It allows multiple clients to store, retrieve, and manage files across a cluster of storage servers. The system is designed with fault tolerance, data integrity, and high availability as core requirements.

### Key Design Goals

| Goal | How Achieved |
|------|-------------|
| **Fault Tolerance** | RAID-5: can survive loss of 1 chunk server per stripe |
| **Data Integrity** | CRC32 checksum verified at upload, storage, and download |
| **Crash Recovery** | WAL (Write-Ahead Log) + Checkpointing on Master |
| **High Availability** | Secondary master auto-promotes when primary dies |
| **Multi-Client Isolation** | Each client gets a unique ID; data stored in per-client subdirectories |
| **Scalability** | Stateless chunk servers; master only tracks metadata |

### What the System Does NOT Do (Intentional Simplifications)
- Does not replicate chunks (RAID-5 parity provides single-fault tolerance instead)
- Does not use TLS (uses gRPC with insecure credentials, suitable for a trusted LAN)
- Does not shard the master (single-master design, HA via secondary)

---

## 2. System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        CLIENT (CLI)                              │
│  upload / download / delete / ls / mkdir / mv / cat / rmdir     │
└──────────────────────┬──────────────────────────────────────────┘
                       │ gRPC RPCs
        ┌──────────────▼──────────────┐
        │      PRIMARY MASTER         │ ←── monitors ──► SECONDARY MASTER
        │  port 50051                 │                   port 50052
        │  - file metadata            │                   - reads WAL (500ms)
        │  - chunk allocation         │                   - auto-promotes
        │  - WAL + checkpoint         │                   - rewrites .master_addr
        └───────┬────────┬────────────┘
                │        │ heartbeats (5s)
     ┌──────────▼──┐  ┌──▼──────────┐  ┌──────────────┐
     │ ChunkServer1│  │ ChunkServer2│  │  ChunkServer3│
     │  port 9001  │  │  port 9002  │  │  port 9003   │
     │  /data1     │  │  /data2     │  │  /parity     │
     └─────────────┘  └─────────────┘  └──────────────┘
```

### Component Roles

| Component | Role |
|-----------|------|
| **Master Server** | Brain of the system. Tracks all file metadata (which chunks are where). Does NOT store actual data. |
| **Chunk Server** | Stores actual file data on disk. Multiple chunk servers provide distributed storage. |
| **Client** | CLI tool that uploads/downloads files. Splits files into stripes, calculates parity, uploads in parallel. |
| **Secondary Master** | Standby replica of the master. Reads WAL continuously. Promotes itself if primary dies. |

---

## 3. Feature List

### Core Storage
- [x] **File Upload** — split file into 1 MB chunks, calculate XOR parity, upload 3 chunks per stripe in parallel
- [x] **File Download** — retrieve all stripes, reconstruct missing chunk using parity if one server is down
- [x] **File Delete** — removes metadata from master and physical chunks from all chunk servers
- [x] **File List** — simple listing of all files for a client

### Fault Tolerance
- [x] **RAID-5 Erasure Coding** — each stripe: 2 data chunks + 1 parity; any 1 chunk can be lost and recovered
- [x] **Odd-Chunk Fix** — last stripe with only 1 data chunk handled correctly (empty chunk2 padded to zero)
- [x] **Checksum verification** — CRC32 verified at upload, storage, and download
- [x] **Dead server detection** — master marks chunk server dead after 20s without heartbeat

### Crash Recovery
- [x] **Write-Ahead Log (WAL)** — every mutation logged to disk before applied to memory
- [x] **Checkpointing** — full master state snapshot every 5 minutes
- [x] **WAL Recovery** — replays WAL entries on startup after a crash

### High Availability
- [x] **Secondary Master** — runs in standby mode, polls WAL every 500ms to stay in sync
- [x] **Automatic Failover** — secondary detects primary failure (3 missed pings ≈ 6s) and promotes itself
- [x] **Client Failover** — client automatically probes both masters and routes to the active one
- [x] **Chunkserver Failover** — chunk servers re-read `.master_addr` on every heartbeat and follow promoted secondary

### File Management
- [x] **Folder creation** (`mkdir`) — hierarchical folder support, parent folders auto-created
- [x] **Folder deletion** (`rmdir`) — only empties folders allowed to be deleted
- [x] **File move/rename** (`mv`) — rename files or move to folders; validates destination folder exists
- [x] **Detailed listing** (`ls-detailed`) — shows type, path, size, upload timestamp
- [x] **File preview** (`cat`) — reads file content without full download (up to 64 KB preview)

---

## 4. Component Deep Dive

### 4.1 Master Server

**Files:** `cmd/master/master.go`, `main.go`, `wal_operation.go`, `wal_recovery.go`, `checkpoint.go`, `folder_operations.go`

The master server is the **metadata brain**. It holds all information about where files are stored, but never touches actual file data.

#### In-Memory State (MasterServer struct)

```go
type MasterServer struct {
    // Core metadata
    fileInfo    map[int64]map[string]map[int32]*dfspb.StripeMetadata
    //  └──clientID   └──filename    └──stripeNum   └──(chunkIDs + servers)
    
    clientIDs       map[int64][]string              // client → owned files
    fileSizes       map[int64]map[string]int64       // client → file → size
    chunkStatus     map[string]string                // chunkID → "PENDING"/"SUCCESS"
    chunkServers    []string                         // known chunk server addresses
    servers         map[string]*ServerInfo           // health status per server

    // Folder support
    clientFolders   map[int64]map[string]bool        // client → folder → exists
    fileUploadTimes map[int64]map[string]int64       // client → file → unix timestamp

    // WAL
    walFile   *os.File
    walWriter *bufio.Writer
    walMu     sync.Mutex

    // High Availability
    IsStandby  bool              // true = standby mode, rejects writes
    walOffset  int64             // byte position: how far standby has read the WAL
    listenAddr string            // own address, written to .master_addr on promotion
}
```

#### Key gRPC Handlers

| RPC | What it does |
|-----|-------------|
| `CreateFile` | Registers a new file, allocates stripe/chunk IDs, logs to WAL |
| `AllocateChunk` | Internal allocation: assigns chunks to healthy servers |
| `GetFileMetadata` | Returns stripe map for download; verifies client ownership |
| `ReceiveHeartbeat` | Marks chunk server alive, registers new servers |
| `ConfirmWrite` | Marks chunks SUCCESS after client confirms upload; logs to WAL |
| `DeleteFile` | Deletes metadata + sends DeleteChunks RPC to all chunk servers |
| `ListFiles` | Returns list of files owned by this client |
| `Ping` | Health check; returns `Active=false` if in standby mode |
| `CreateFolder` | Creates folder hierarchy in memory |
| `DeleteFolder` | Deletes empty folder; rejects if has subfolders or files |
| `MoveFile` | Renames/moves file in metadata maps |
| `ListFilesDetailed` | Filtered listing with metadata (size, timestamp, type) |
| `ReadFileContent` | Reads and returns file content for preview (streams from chunk servers) |

#### Dead Server Detection

A background goroutine runs every 10 seconds:
```go
go func() {
    for {
        time.Sleep(10 * time.Second)
        server.serversMu.Lock()
        for addr, info := range server.servers {
            if time.Since(info.LastHeartbeat) > 20*time.Second {
                if info.Alive {
                    info.Alive = false  // Mark dead
                }
            }
        }
        server.serversMu.Unlock()
    }
}()
```
Chunk servers send heartbeats every **5 seconds**. If 20 seconds pass with no heartbeat, the server is marked dead and excluded from future chunk allocations.

---

### 4.2 Chunk Server

**Files:** `cmd/chunkserver/chunkservertask.go`, `main.go`, `checksum.go`, `chunkserver_recovery.go`

The chunk server stores actual file bytes on disk. Each client's data is physically isolated in subdirectories.

#### Storage Layout
```
chunk_server1/
├── 793535179180736266/          ← Client ID directory
│   ├── myfile.pdf_chunk1_0001   ← data chunk
│   ├── myfile.pdf_chunk1_0001.checksum
│   ├── myfile.pdf_chunk1_0002
│   └── myfile.pdf_parity1_0001  ← parity chunk
└── 2494719681458031762/          ← Another client's data
    └── ...
```

#### Key gRPC Handlers

| RPC | What it does |
|-----|-------------|
| `WriteChunk` | Verifies CRC32 checksum, stores `data` and `.checksum` file to disk |
| `ReadChunk` | Reads chunk from disk, returns data and checksum |
| `DeleteChunks` | Batch-deletes chunk files and their .checksum files |

#### Heartbeat with Auto-Failover Support

```go
func SendHeartbeats(port string, masterAddr string, logger *log.Logger) {
    ticker := time.NewTicker(5 * time.Second)
    for range ticker.C {
        // Re-read .master_addr EVERY tick — follows promoted secondary automatically
        target := resolveActiveMaster(masterAddr)
        
        if target != currentTarget {
            conn.Close()
            conn = grpc.NewClient(target, ...)  // reconnect to new master
            currentTarget = target
        }
        masterClient.ReceiveHeartbeat(...)
    }
}
```
This is a key design: chunk servers **re-read `.master_addr` on every heartbeat**. When the secondary promotes and updates the file, all chunk servers automatically follow the new master without any restart.

---

### 4.3 Client

**Files:** `cmd/client/main.go`, `stripe_reader.go`, `parallel_upload.go`, `download_stripe.go`, `parity.go`, `ack_queue.go`, `checksum.go`, `folder_client.go`, `client_id.go`

#### Upload Pipeline

```
File on Disk
    │
    ▼ streamFileInStripes() — goroutine (producer)
channel<- Stripe  (buffered=2, max 6MB in memory at once)
    │
    ▼ uploadStripesStreaming() — consumer loop
for each stripe:
    ├── goroutine: uploadChunk(data1   → ChunkServer1)
    ├── goroutine: uploadChunk(data2   → ChunkServer2)
    └── goroutine: uploadChunk(parity  → ChunkServer3)
         │
         ▼  resultChan (buffered=6)
         collector goroutine: tracks successes, updates AckQueue
    │
    ▼ master.ConfirmWrite(successfulChunkIDs)
```

#### Download Pipeline

```
master.GetFileMetadata → stripe map
    │
for each stripe (sequential):
    ├── goroutine: downloadChunk(data1)   ─┐
    ├── goroutine: downloadChunk(data2)    ├── wg.Wait()
    └── goroutine: downloadChunk(parity) ─┘
         │
         ▼ reconstructMissingChunk() — XOR recovery if 1 chunk missing
         │
         ▼ writeStripeToFile() — trims padding from last stripe
```

#### Smart Master Discovery for Failover

```go
func getMasterAddr() string {
    // 1. Check MASTER_ADDR env var (highest priority)
    // 2. Read .master_addr file
    // 3. Probe primary with Ping() — returns Active=true/false
    // 4. If primary is down/standby → try .secondary_addr
    // 5. If secondary is active → update .master_addr and return it
    // 6. If both down → return primary anyway (error surfaced to caller)
}
```

---

## 5. RAID-5 Implementation

### Concept

RAID-5 provides **fault tolerance without full replication**. Instead of storing 3 copies of every file (300% overhead), it stores 2 data chunks + 1 parity chunk (150% overhead). Any single chunk can be reconstructed from the other two using XOR.

### XOR Property

XOR is its own inverse:
```
P = D1 ⊕ D2          (parity = XOR of both data chunks)
D1 = D2 ⊕ P          (recover data1 from data2 and parity)
D2 = D1 ⊕ P          (recover data2 from data1 and parity)
```

### Stripe Layout

```
File: big.pdf (3 MB)
                 ┌─────────────┐  ┌─────────────┐  ┌─────────────┐
Stripe 1:        │  chunk1_001 │  │  chunk1_002 │  │  parity1    │
                 │  (1MB dat1) │  │  (1MB dat2) │  │  D1 XOR D2  │
                 │  Server 1   │  │  Server 2   │  │  Server 3   │
                 └─────────────┘  └─────────────┘  └─────────────┘
                       
Stripe 2:        ┌─────────────┐  ┌─────────────┐  ┌─────────────┐
                 │  chunk2_003 │  │   (empty)   │  │  parity2    │
                 │  (1MB dat1) │  │  (padded 0s)│  │  D1 XOR 00s │
                 │  Server 1   │  │  Server 2   │  │  Server 3   │
                 └─────────────┘  └─────────────┘  └─────────────┘
```

### Code: Parity Calculation (`parity.go`)

```go
func calculateParity(chunk1, chunk2 []byte) []byte {
    maxLen := len(chunk1)
    if len(chunk2) > maxLen { maxLen = len(chunk2) }
    
    parity := make([]byte, maxLen)
    for i := 0; i < maxLen; i++ {
        var byte1, byte2 byte
        if i < len(chunk1) { byte1 = chunk1[i] }
        if i < len(chunk2) { byte2 = chunk2[i] }
        parity[i] = byte1 ^ byte2    // XOR operation
    }
    return parity
}
```

### Odd-Chunk Edge Case Fix

When a file has an **odd number of 1MB chunks** (e.g., 3 MB file = 3 chunks, last stripe has only 1 data chunk), the second data chunk is padded to all zeros, and parity = data1 ⊕ 0x00...00 = data1. During reconstruction:

```go
// padChunk pads with zeros if chunk is smaller than CHUNK_SIZE
func padChunk(chunk []byte, targetSize int) []byte {
    padded := make([]byte, targetSize)
    copy(padded, chunk)
    return padded
}
```

The master also skips uploading the empty chunk (chunk2 ID is empty string `""`), and the client checks:
```go
for _, task := range tasks {
    if len(task.ChunkID) == 0 {
        continue  // skip empty chunks (odd-file last stripe)
    }
    go uploadChunk(task, ...)
}
```

### Reconstruction During Download (`download_stripe.go`)

```go
func reconstructMissingChunk(stripe *StripeDownload, info DownloadStripeInfo) error {
    // Special case: all expected data chunks present (even if parity missing)
    if dataChunksAvailable == dataChunksExpected && dataChunksAvailable > 0 {
        return nil
    }
    if stripe.ChunksOK < 2 {
        return fmt.Errorf("insufficient chunks: only %d available", stripe.ChunksOK)
    }
    // Missing data1: recover from data2 XOR parity
    if stripe.DataChunk1 == nil && stripe.DataChunk2 != nil && stripe.ParityChunk != nil {
        stripe.DataChunk1 = calculateParity(stripe.DataChunk2, stripe.ParityChunk)
        return nil
    }
    // Missing data2: recover from data1 XOR parity
    if stripe.DataChunk2 == nil && stripe.DataChunk1 != nil && stripe.ParityChunk != nil {
        stripe.DataChunk2 = calculateParity(stripe.DataChunk1, stripe.ParityChunk)
        return nil
    }
    // Missing parity only: not needed for output
    if stripe.ParityChunk == nil && stripe.DataChunk1 != nil && stripe.DataChunk2 != nil {
        return nil
    }
    return fmt.Errorf("unexpected chunk combination")
}
```

---

## 6. WAL and Crash Recovery

### Problem

If the master crashes after updating in-memory state but before the data is needed again on restart, all file metadata is lost.

### Solution: Write-Ahead Log

**Principle:** Write to disk FIRST, update memory SECOND.

```
Client Request
    │
    ▼
AppendWAL(operation, data)     ← 1. Write to disk (fsync)
    │
    ▼
Update in-memory maps          ← 2. Only then update memory
    │
    ▼
Return response to client
```

### WAL Format (newline-delimited JSON)

```json
{"operation":"CREATE_FILE","timestamp":1708358400,"data":{"client_id":793535179,"filename":"big.pdf","total_size":3145728}}
{"operation":"ALLOCATE_CHUNK","timestamp":1708358400,"data":{"clientID":793535179,"filename":"big.pdf","stripes":{...},"status":"PENDING"}}
{"operation":"CONFIRM_WRITE","timestamp":1708358401,"data":{"filename":"big.pdf","chunk_ids":["big.pdf_chunk1_0001",...],"status":"SUCCESS"}}
```

### WAL Operations Tracked

| Operation | When Written |
|-----------|-------------|
| `CREATE_FILE` | When client registers a new file |
| `ALLOCATE_CHUNK` | When chunk IDs and servers are assigned |
| `CONFIRM_WRITE` | When client confirms all chunks uploaded successfully |
| `DELETE_FILE` | When a file is deleted |

### Code: WAL Append with fsync (`wal_operation.go`)

```go
func (m *MasterServer) AppendWAL(operation string, data interface{}) error {
    m.walMu.Lock()
    defer m.walMu.Unlock()

    entry := WALEntry{
        Operation: operation,
        Timestamp: time.Now().Unix(),
        Data:      json.Marshal(data),
    }
    m.walWriter.WriteString(json.Marshal(entry) + "\n")
    m.walWriter.Flush()    // flush buffer to OS
    m.walFile.Sync()       // fsync: force write to physical disk
    return nil
}
```

### Checkpointing

Every 5 minutes, the master writes a full JSON snapshot of its entire state:
```go
type Checkpoint struct {
    Timestamp   int64
    FileInfo    map[int64]map[string]map[int32]*StripeMetadataJSON
    ClientIDs   map[int64][]string
    FileSizes   map[int64]map[string]int64
    ChunkStatus map[string]string
}
```

After a successful checkpoint, the WAL is truncated (old WAL backed up as `.wal.old`). This prevents the WAL from growing indefinitely.

### Recovery Sequence on Startup

```
Master starts
    │
    ▼ LoadCheckpoint("master.checkpoint")
    │   → restore fileInfo, clientIDs, fileSizes, chunkStatus
    │   → if no checkpoint: start with empty maps
    │
    ▼ RecoverFromWAL("master.wal")
    │   → replay CREATE_FILE, ALLOCATE_CHUNK, CONFIRM_WRITE, DELETE_FILE
    │   → idempotent: duplicate entries are handled gracefully
    │
    ▼ seed walOffset = current WAL file size
    │   → standby will start reading from THIS point forward
    │
    ▼ Start serving
```

---

## 7. High Availability — Secondary Master Failover

### Architecture

```
Primary Master (port 50051) ←──── Secondary Master (port 50052)
     │                                    │
     │ Writes WAL to disk                 │ Reads WAL every 500ms
     │                                    │ Pings primary every 2s
     │ .master_addr = "127.0.0.1:50051"  │ .secondary_addr = "127.0.0.1:50052"
```

### How Secondary Stays in Sync

The secondary uses **incremental WAL polling** every 500ms:

```go
// PeriodicCheckpoint (runs in background on both primary and secondary)
case <-walPoller.C:
    if m.IsStandby {
        m.RecoverFromWALIncremental("master.wal")
    }
```

`RecoverFromWALIncremental` seeks to `walOffset` (where it left off), reads only new lines, replays them, and advances the offset. This is O(new entries) not O(all entries).

### Failure Detection

```go
func (m *MasterServer) MonitorPrimary(primaryAddr string) {
    ticker := time.NewTicker(2 * time.Second)
    failCount := 0
    maxFails := 3   // promote after ~6 seconds of silence

    for range ticker.C {
        conn, err := grpc.NewClient(primaryAddr, ...)
        _, err = client.Ping(ctx, &PingRequest{})
        
        if err != nil {
            failCount++
        } else {
            failCount = 0     // reset on success
        }

        if failCount >= maxFails {
            m.PromoteToActive()
            return
        }
    }
}
```

### Promotion Steps

```go
func (m *MasterServer) PromoteToActive() {
    // 1. Final WAL catch-up (read any entries written between last poll and now)
    m.RecoverFromWALIncremental("master.wal")
    
    // 2. Switch mode: accept write operations
    m.IsStandby = false
    
    // 3. Advertise as new primary — clients and chunk servers read this file
    os.WriteFile(".master_addr", []byte("127.0.0.1:50052\n"), 0644)
    
    // Log: "Server is now ACTIVE and accepting write requests"
}
```

### Client Auto-Failover

```go
func getMasterAddr() string {
    primaryAddr  := readFile(".master_addr")   // "127.0.0.1:50051"
    secondaryAddr := readFile(".secondary_addr") // "127.0.0.1:50052"

    if isActive(primaryAddr) {
        return primaryAddr                // normal path
    }
    // Primary is down or in standby
    if isActive(secondaryAddr) {
        writeFile(".master_addr", secondaryAddr)  // persist for future calls
        return secondaryAddr
    }
    return primaryAddr  // both down, let caller handle the error
}

func isActive(addr string) bool {
    resp, err := client.Ping(ctx, &PingRequest{})
    return err == nil && resp.Active  // Active=true means not in standby mode
}
```

### Chunk Server Auto-Failover

Chunk servers call `resolveActiveMaster(configuredAddr)` on every heartbeat tick (5s). They re-read `.master_addr` each time. When the secondary updates `.master_addr`, all chunk servers switch to the new master within 5 seconds automatically.

### Failover Timeline

```
t=0s   Primary master killed
t=2s   Secondary: ping attempt 1 → FAIL (failCount=1)
t=4s   Secondary: ping attempt 2 → FAIL (failCount=2)
t=6s   Secondary: ping attempt 3 → FAIL (failCount=3) → PromoteToActive()
t=6s   Secondary: final WAL catch-up
t=6s   Secondary: .master_addr updated to :50052
t=6s   Secondary: IsStandby = false → now accepts writes
t=11s  Chunk servers: re-read .master_addr → switch heartbeat target to :50052
t=?    Client: next operation → probes :50051 (no response) → probes :50052 → routes there
```

**Tested Result:** File uploaded before primary failure is downloadable through the promoted secondary with MD5 integrity verified. ✅

---

## 8. Data Integrity — CRC32 Checksums

### Three-Layer Verification

```
1. CLIENT UPLOAD:
   data → calculateChecksum(data) → send (data, checksum) to ChunkServer

2. CHUNKSERVER RECEIVE:
   receive (data, checksum)
   if calculateChecksum(data) != checksum → REJECT → return error
   else → store data + store checksum to separate .checksum file

3. CLIENT DOWNLOAD:
   receive (data, checksum) from ChunkServer
   if calculateChecksum(data) != checksum → error (data corrupted in transit)
```

### CRC32 Implementation (`checksum.go`)

```go
import "hash/crc32"

func calculateChecksum(data []byte) string {
    hash := crc32.NewIEEE()
    hash.Write(data)
    return fmt.Sprintf("%08x", hash.Sum32())  // 8-char hex string
}
```

CRC32 was chosen over MD5/SHA because:
- Extremely fast (hardware-accelerated on modern CPUs)
- Sufficient for detecting accidental corruption (not for security)
- 4-byte result → stored as 8-char hex string

---

## 9. Client Authentication and Ownership

### Client ID Generation

On first upload, the master generates a random 64-bit integer ID:

```go
func RandomID() int64 {
    lower := int64((1 << 32) - 1)  // 2^32 - 1
    upper := int64((1 << 63) - 1)  // 2^63 - 1
    diff := upper - lower + 1
    return rand.Int63n(diff) + lower
}
```

The client saves this ID to a `.client_id` file. All future operations use this ID.

### Ownership Enforcement

- **Download:** `GetFileMetadata` checks `clientIDs[req.ClientId]` — only the owning client can get metadata
- **Delete:** `DeleteFile` verifies `clientIDs[clientID]` contains the filename before proceeding
- **Physical isolation:** Chunks stored in `chunkserver/CLIENT_ID/chunkID` directories

### Physical Isolation on Disk

```
chunk_server1/
├── 7368816824647415197/    ← Client A's data
│   ├── big.pdf_chunk1_0001
│   └── big.pdf_parity1_0001
└── 2494719681458031762/    ← Client B's data
    ├── report.docx_chunk1_0001
    └── report.docx_parity1_0001
```

No client can accidentally access another client's data even if chunk IDs collide.

---

## 10. Folder and File Management

### Folder Hierarchy

Folders are stored purely as metadata on the master (no disk directories created on chunk servers). The master maintains:

```go
clientFolders map[int64]map[string]bool
// Example: clientFolders[12345]["documents"] = true
//          clientFolders[12345]["documents/reports"] = true
//          clientFolders[12345]["documents/reports/2024"] = true
```

### CreateFolder

- Validates path is not root (`.` or `/`)
- Checks for duplicate folder
- **Auto-creates all parent folders:** `documents/reports/2024` creates `documents`, `documents/reports`, and `documents/reports/2024`

### DeleteFolder

Safety checks:
1. Folder must exist
2. Folder must contain no files (`fileInfo` checked for any file with prefix `folder/`)
3. Folder must contain no subfolders (`clientFolders` checked for any key with prefix `folder/`)

### MoveFile

```go
func (m *MasterServer) MoveFile(...) {
    // Validate source exists
    stripes := m.fileInfo[clientId][sourcePath]  // get stripe metadata
    
    // Validate dest folder exists
    destDir := filepath.Dir(destPath)
    if !m.clientFolders[clientId][destDir] → error
    
    // Move all metadata maps
    m.fileInfo[clientId][destPath] = stripes
    delete(m.fileInfo[clientId], sourcePath)
    m.fileSizes[clientId][destPath] = m.fileSizes[clientId][sourcePath]
    // ... similar for uploadTimes and clientIDs list
}
```

### ReadFileContent (cat command)

Reads file content from chunk servers for preview WITHOUT requiring the client to download the entire file:
1. Master calculates which stripes cover the requested byte range
2. Downloads only those stripe chunks directly from chunk servers
3. Trims to exact offset and length requested

---

## 11. gRPC and Protocol Buffers

### Why gRPC?

- **Strong typing:** Protocol Buffers define the exact schema for every message
- **Performance:** Binary serialization (much smaller than JSON)
- **Code generation:** `protoc` generates Go structs and client/server boilerplate automatically
- **Streaming support:** gRPC supports streaming RPCs (used for future extensibility)

### Proto Services Defined

```protobuf
service MasterServer {
    rpc CreateFile(CreateFileRequest) returns (CreateFileResponse);
    rpc GetFileMetadata(GetFileMetadataRequest) returns (GetFileMetadataResponse);
    rpc ReceiveHeartbeat(HeartbeatRequest) returns (HeartbeatResponse);
    rpc ConfirmWrite(ConfirmWriteRequest) returns (ConfirmWriteResponse);
    rpc DeleteFile(DeleteFileRequest) returns (DeleteFileResponse);
    rpc ListFiles(ListFilesRequest) returns (ListFilesResponse);
    rpc CreateFolder(CreateFolderRequest) returns (CreateFolderResponse);
    rpc DeleteFolder(DeleteFolderRequest) returns (DeleteFolderResponse);
    rpc MoveFile(MoveFileRequest) returns (MoveFileResponse);
    rpc ListFilesDetailed(ListFilesDetailedRequest) returns (ListFilesDetailedResponse);
    rpc ReadFileContent(ReadFileContentRequest) returns (ReadFileContentResponse);
    rpc Ping(PingRequest) returns (PingResponse);
}

service ChunkServer {
    rpc WriteChunk(WriteChunkRequest) returns (WriteChunkResponse);
    rpc ReadChunk(ReadChunkRequest) returns (ReadChunkResponse);
    rpc DeleteChunks(DeleteChunksRequest) returns (DeleteChunksResponse);
    rpc ForwardChunk(ForwardChunkRequest) returns (WriteChunkResponse);
}
```

### Key Proto Messages

```protobuf
message StripeMetadata {
    int32 stripe_num = 1;
    repeated string chunk_ids = 2;  // [data1_id, data2_id, parity_id]
    repeated string servers   = 3;  // [server1_addr, server2_addr, server3_addr]
}

message CreateFileResponse {
    bool success = 1;
    int64 client_id = 2;
    map<int32, StripeMetadata> stripes = 3;  // stripe number → metadata
}
```

---

## 12. Go Language Features Used

This section highlights specific Go features and idioms used throughout the project.

### 12.1 Goroutines — Lightweight Concurrency

Goroutines are used extensively for parallel chunk uploads and downloads:

```go
// Upload 3 chunks of a stripe in parallel
for _, task := range tasks {
    wg.Add(1)
    go uploadChunk(task, &wg, resultChan, ackQueue)  // goroutine per chunk
}
wg.Wait()  // wait for all 3 to complete
```

Each goroutine is a lightweight thread (a few KB stack, managed by Go runtime). Thousands can run simultaneously.

### 12.2 Channels — Safe Communication Between Goroutines

```go
// Producer-Consumer pattern for streaming upload
stripeChan := make(chan Stripe, 2)  // buffered channel: 2 stripes max in memory
errChan := make(chan error, 1)

go streamFileInStripes(filePath, stripeMap, stripeChan, errChan)  // producer

for stripe := range stripeChan {  // consumer
    go uploadStripe(stripe, ...)
}
```

Channel directionality used in function signatures for safety:
```go
func uploadChunk(task UploadTask, wg *sync.WaitGroup, 
                 resultChan chan<- UploadResult,   // send-only
                 ackQueue *AckQueue)

func streamFileInStripes(..., 
                          stripeChan chan<- Stripe,  // send-only
                          errChan chan<- error)       // send-only
```

### 12.3 sync.WaitGroup — Wait for Multiple Goroutines

```go
var wg sync.WaitGroup

for _, task := range uploadTasks {
    wg.Add(1)
    go func(t UploadTask) {
        defer wg.Done()       // called even if panic occurs
        uploadChunk(t, ...)
    }(task)
}
wg.Wait()  // block until all goroutines call wg.Done()
```

### 12.4 sync.Mutex and sync.RWMutex — Concurrent Map Access

Go maps are **not thread-safe**. Every shared map access is protected:

```go
type MasterServer struct {
    mu        sync.Mutex    // protects fileInfo, fileSizes, chunkStatus
    serversMu sync.RWMutex  // protects servers map (allows concurrent reads)
    walMu     sync.Mutex    // protects WAL writes
}

// Read lock allows multiple simultaneous reads
m.serversMu.RLock()
for _, addr := range m.chunkServers {
    // safe to read concurrently
}
m.serversMu.RUnlock()

// Write lock for exclusive access
m.serversMu.Lock()
m.servers[addr].Alive = true
m.serversMu.Unlock()
```

### 12.5 defer — Guaranteed Cleanup

`defer` runs a function when the surrounding function returns, regardless of how it returns (normal return, error, or panic):

```go
func (m *MasterServer) CreateFile(ctx context.Context, req *CreateFileRequest) (*CreateFileResponse, error) {
    m.mu.Lock()
    defer m.mu.Unlock()  // guaranteed to unlock even if function panics or returns error early
    
    // ... rest of function
}

func uploadChunk(task UploadTask, wg *sync.WaitGroup, ...) {
    defer wg.Done()  // guaranteed to call Done even on error path
    
    conn, err := grpc.NewClient(...)
    defer conn.Close()  // guaranteed to close connection
}
```

### 12.6 Interfaces — gRPC Implementation Pattern

The gRPC framework uses Go interfaces. The generated code creates an interface, and our server struct must implement it:

```go
// Generated by protoc (we don't write this):
type MasterServerServer interface {
    CreateFile(context.Context, *CreateFileRequest) (*CreateFileResponse, error)
    // ... other RPCs
    mustEmbedUnimplementedMasterServerServer()
}

// We write this — implements the interface:
type MasterServer struct {
    dfspb.UnimplementedMasterServerServer  // embedded: provides default "not implemented" for all RPCs
    mu     sync.Mutex
    // ...our fields
}

func (m *MasterServer) CreateFile(...) (*CreateFileResponse, error) {
    // our implementation overrides the embedded default
}
```

### 12.7 Structs and Embedding

```go
type ServerInfo struct {
    LastHeartbeat time.Time
    Alive         bool
}

// Embedding: MasterServer "inherits" all methods of UnimplementedMasterServerServer
type MasterServer struct {
    dfspb.UnimplementedMasterServerServer  // embedded struct
    mu sync.Mutex
    fileInfo map[int64]map[string]map[int32]*dfspb.StripeMetadata
    // ...
}
```

### 12.8 Error Handling — Explicit Error Returns

Go does not have exceptions. Errors are return values:

```go
func (m *MasterServer) AppendWAL(operation string, data interface{}) error {
    dataBytes, err := json.Marshal(data)
    if err != nil {
        return fmt.Errorf("failed to marshal WAL data: %v", err)  // wrap error
    }
    
    _, err = m.walWriter.WriteString(string(dataBytes) + "\n")
    if err != nil {
        return fmt.Errorf("failed to write to WAL: %v", err)
    }
    
    return nil  // explicit success
}
```

### 12.9 Select Statement — Non-blocking Channel Operations

```go
func (m *MasterServer) PeriodicCheckpoint(intervalMinutes int, checkpointPath, walPath string) {
    ticker     := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
    walPoller  := time.NewTicker(500 * time.Millisecond)
    
    for {
        select {
        case <-ticker.C:      // fires every 5 minutes
            if !m.IsStandby {
                m.CreateCheckpoint(checkpointPath)
                m.TruncateWAL(walPath)
            }
        case <-walPoller.C:   // fires every 500ms
            if m.IsStandby {
                m.RecoverFromWALIncremental(walPath)
            }
        }
    }
}
```

`select` blocks until one of the channel operations is ready, then executes that case — similar to an event loop.

### 12.10 Closures — Goroutines Capturing Variables

```go
// Download 3 chunks in parallel using closures
wg.Add(1)
go func() {
    defer wg.Done()
    result := downloadChunkFromServer(
        stripeInfo.DataChunk1.ChunkID,    // captured from outer scope
        stripeInfo.DataChunk1.Server,
        clientID,
        true, false, false,
    )
    resultChan <- result
}()
```

> ⚠️ **Common Go gotcha:** Loop variable capture. In the upload code, each task is passed as a function argument (not captured directly) to avoid the loop-variable-aliasing bug where all goroutines might share the same variable.

### 12.11 JSON Encoding (encoding/json)

Used for WAL serialization with struct tags:

```go
type WALEntry struct {
    Operation string          `json:"operation"`  // lowercase in JSON
    Timestamp int64           `json:"timestamp"`
    Data      json.RawMessage `json:"data"`       // raw JSON blob, delay parsing
}
```

`json.RawMessage` is used for the `Data` field so the operation-specific payload can be serialized once and embedded without double-encoding.

### 12.12 io.ReadFull — Exact-Size Reads

```go
chunk1 := make([]byte, CHUNK_SIZE)           // allocate 1MB buffer
n1, err := io.ReadFull(file, chunk1)         // read exactly CHUNK_SIZE bytes
// Returns io.ErrUnexpectedEOF if file ends before filling buffer
// Returns io.EOF only if 0 bytes were read (file already at end)
chunk1 = chunk1[:n1]                         // trim to actual bytes read
```

### 12.13 filepath Package — Cross-Platform Path Handling

```go
import "path/filepath"

folderPath := filepath.Clean(req.FolderPath)   // normalize: remove .., double slashes
destDir    := filepath.Dir(destPath)            // get parent directory
chunPath   := filepath.Join(c.storagePath, fmt.Sprintf("%d", clientId), chunkID)
```

---

## 13. Complete Data Flow Diagrams

### Upload Flow

```
Client                      Master                     ChunkServer1/2/3
  │                            │                              │
  │─── CreateFile(name,size) ──►│                              │
  │                            │ AppendWAL(CREATE_FILE)        │
  │                            │ AppendWAL(ALLOCATE_CHUNK)     │
  │◄── {clientID, stripeMap} ──│                              │
  │                            │                              │
  │ [for each stripe]          │                              │
  │   calculate parity         │                              │
  │   calculate CRC32          │                              │
  │                            │                              │
  │──────────── WriteChunk(data1, checksum1) ────────────────►│ (ChunkServer1)
  │──────────── WriteChunk(data2, checksum2) ────────────────►│ (ChunkServer2)
  │──────────── WriteChunk(parity, checksumP) ───────────────►│ (ChunkServer3)
  │   (all 3 in parallel)      │                              │
  │◄─── {success} ─────────────────────────────────────────── │
  │                            │                              │
  │─── ConfirmWrite(chunkIDs) ─►│                              │
  │                            │ AppendWAL(CONFIRM_WRITE)      │
  │◄── {success} ──────────────│                              │
```

### Download Flow (with Reconstruction)

```
Client                      Master                     ChunkServer1/2/3
  │                            │                              │
  │─── GetFileMetadata(name) ──►│                              │
  │                            │ [verify ownership]           │
  │◄── {stripeMap, fileSize} ──│                              │
  │                            │                              │
  │ [for each stripe]          │                              │
  │──────── ReadChunk(data1) ──────────────────────────────── │ (ChunkServer1) ✓
  │──────── ReadChunk(data2) ──────────────────────────────── │ (ChunkServer2) ✗ (DOWN)
  │──────── ReadChunk(parity) ─────────────────────────────── │ (ChunkServer3) ✓
  │   (all 3 in parallel)      │                              │
  │                            │                              │
  │ data2 missing → data2 = data1 XOR parity  [reconstruct!]  │
  │ verify checksums           │                              │
  │ write data1 + data2 to file│                              │
```

### Failover Flow

```
Secondary                   Primary (DEAD)              Clients
  │                            │                              │
  │── Ping() ──────────────────►│ (no response, timeout 1s)   │
  │   failCount = 1             │                              │
  │── Ping() ──────────────────►│ (no response)               │
  │   failCount = 2             │                              │
  │── Ping() ──────────────────►│ (no response)               │
  │   failCount = 3 → PROMOTE   │                              │
  │                             │                              │
  │ RecoverFromWALIncremental() │                              │
  │ IsStandby = false           │                              │
  │ write ".master_addr" → :50052                             │
  │                             │                              │
  │                             │     ◄── upload(file) ────── │
  │ CreateFile response ─────────────────────────────────────►│
```

---

## 14. Project File Structure

```
xorfs/
├── cmd/
│   ├── master/
│   │   ├── main.go                ← startup, CLI flags (-port, -mode, -primary)
│   │   ├── master.go              ← MasterServer struct + gRPC handler implementations
│   │   ├── master_helper.go       ← RandomID() generator
│   │   ├── wal_operation.go       ← WALEntry types + AppendWAL() with fsync
│   │   ├── wal_recovery.go        ← RecoverFromWAL(), RecoverFromWALIncremental()
│   │   ├── checkpoint.go          ← CreateCheckpoint(), LoadCheckpoint(), TruncateWAL(),
│   │   │                            PeriodicCheckpoint() (also WAL polling for standby)
│   │   └── folder_operations.go   ← CreateFolder, DeleteFolder, MoveFile,
│   │                                ListFilesDetailed, ReadFileContent, downloadChunk
│   │
│   ├── chunkserver/
│   │   ├── main.go                ← startup, CLI flags (-port, -storage, -master)
│   │   ├── chunkservertask.go     ← WriteChunk, ReadChunk, DeleteChunks,
│   │   │                            SendHeartbeats (with auto-failover), resolveActiveMaster
│   │   ├── checksum.go            ← CRC32 calculation
│   │   └── chunkserver_recovery.go ← ReportInventory (chunk inventory to master)
│   │
│   └── client/
│       ├── main.go                ← CLI entry point (upload/download/delete/ls/mkdir/mv/cat)
│       │                            getMasterAddr() with failover probing
│       ├── client_id.go           ← loadClientID(), saveClientID() (.client_id file)
│       ├── stripe_reader.go       ← streamFileInStripes() producer goroutine
│       ├── parallel_upload.go     ← uploadStripesStreaming(), uploadChunk() goroutines
│       ├── download_stripe.go     ← downloadStripe() parallel, reconstructMissingChunk(),
│       │                            writeStripeToFile()
│       ├── parity.go              ← calculateParity() XOR, padChunk()
│       ├── checksum.go            ← calculateChecksum() CRC32
│       ├── ack_queue.go           ← AckQueue (thread-safe pending upload tracker)
│       ├── stripe.go              ← Stripe struct definition
│       └── folder_client.go       ← createFolder, deleteFolder, moveFile,
│                                    listFilesDetailed, catFile, formatSize
│
├── dfspb/
│   ├── dfs.pb.go                  ← Generated protobuf message structs
│   └── dfs_grpc.pb.go             ← Generated gRPC client/server interfaces
│
├── pkg/
│   └── config/
│       └── config.go              ← GetMasterAddr(), GetMyAddr(), GetLocalIP()
│
├── dfs.proto                      ← Protocol Buffer service and message definitions
├── Makefile                       ← Build, run, test, and file operation commands
├── go.mod                         ← Go module definition (module name, dependencies)
├── go.sum                         ← Checksum database for dependencies
├── master.wal                     ← Write-ahead log (runtime file)
├── master.checkpoint              ← Latest master state snapshot (runtime file)
├── .master_addr                   ← Current active master address (read by clients + chunkservers)
├── .secondary_addr                ← Secondary master address (written by secondary on startup)
├── .client_id                     ← Persistent client ID (written on first upload)
├── test_failover.sh               ← Automated failover test script
├── readme.md                      ← Quick start guide
└── PROJECT_DETAIL.md              ← This document
```

---

## 15. Build and Run Commands

### Build

```bash
make build
# Creates: bin/master, bin/chunkserver, bin/client
```

### Start Cluster (Local)

```bash
# Terminal 1: Primary Master
make run-master                               # listens on :50051

# Terminal 2: Secondary Master (High Availability)
make run-secondary                            # listens on :50052, monitors :50051

# Terminal 3,4,5: Chunk Servers
make run-chunk_server1 MASTER_ADDR=127.0.0.1:50051
make run-chunk_server2 MASTER_ADDR=127.0.0.1:50051
make run-chunk_server3 MASTER_ADDR=127.0.0.1:50051
```

### File Operations

```bash
make upload FILE=report.pdf             # Upload from files/ directory
make download FILE=report.pdf           # Download → downloaded_report.pdf
make delete FILE=report.pdf             # Delete from DFS
make ls                                 # List all owned files
make ls-detailed                        # List with size, timestamp, type
make ls-detailed FOLDER=documents       # List specific folder
```

### Folder Operations

```bash
make mkdir FOLDER=documents/reports    # Create folder (parents auto-created)
make rmdir FOLDER=documents/reports    # Delete empty folder
make mv SRC=report.pdf DEST=documents/report.pdf   # Move/rename
make cat FILE=readme.txt               # Preview file content (no full download)
```

### High Availability Test

```bash
bash test_failover.sh
# Tests: upload → kill primary → wait for secondary to promote → download → MD5 verify
# Expected: All 10 tests PASS
```

### Docker (Full Cluster in Containers)

```bash
make docker-build     # Build images
make docker-up        # Start master + 3 chunk servers in containers
make docker-upload FILE=big.pdf
make docker-ls
make docker-download FILE=big.pdf
make docker-down      # Stop all containers
```

---

## Summary for Supervisor Presentation

| Topic | Key Point |
|-------|-----------|
| **Architecture** | Master–ChunkServer–Client, metadata separated from data |
| **Storage Strategy** | RAID-5: 2 data + 1 parity per stripe, any 1 failure recoverable |
| **Fault Tolerance** | XOR reconstruction; dead server detection via heartbeat timeout |
| **Durability** | WAL with `fsync` + periodic checkpointing every 5 minutes |
| **High Availability** | Secondary master polls WAL (500ms), auto-promotes after 3 failed pings (~6s) |
| **Client HA** | Probes both masters with `Ping()`; rewrites `.master_addr` transparently |
| **Concurrency** | Go goroutines + channels for parallel upload/download; mutexes for shared state |
| **Communication** | gRPC + Protocol Buffers: binary, strongly-typed, code-generated |
| **Integrity** | CRC32 checksums at 3 layers: client-upload, server-store, client-download |
| **Multi-tenancy** | 64-bit random client IDs; ownership enforced at master; physical directory isolation |
| **File Management** | Hierarchical folders, move/rename, detailed listing, file preview (cat) |
| **Language** | Go — chosen for built-in concurrency primitives, simple deployment (single binary), strong standard library |

---

*Document generated: 2026-02-19 | XorFS Distributed File System Project*
