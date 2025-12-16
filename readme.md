<!-- # XorFS - Distributed File System with RAID-5

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

Runs on `localhost:50051` and logs to `master.log`

### 3. Start Chunk Servers (in separate terminals)

```bash
# Terminal 1
make make run-chunk_server1

# Terminal 2
make make run-chunk_server2

# Terminal 3
make make run-chunk_server1
```

Chunk servers run on ports `9001`, `9002`, `9003` with storage in `chunk_server1/`, `chunk_server2/`, `chunk_server3/`

### 4. Upload a File

```bash
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

## Makefile Commands

```bash
make build          # Build all binaries
make proto          # Regenerate protobuf files
make run-master     # Start master server
make run-chunk_server1     # Start chunk server 1 (port 9001)
make run-chunk_server2     # Start chunk server 2 (port 9002)
make run-chunk_server3     # Start chunk server 3 (port 9003)
make setup-master MASTER_IP= < IP at which master is running. eg:192.168.1.65 >
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
- Physical isolation: chunks stored in `chunk_server/client_id/` directories -->


# XorFS - Distributed File System with RAID-5

A distributed file system implementation in Go featuring RAID-5 erasure coding, client authentication, data integrity verification, crash recovery through WAL and checkpointing, and a RESTful web API.

## Features

- **RAID-5 Erasure Coding**: Files split into 2 data chunks + 1 parity chunk per stripe (XOR-based)
- **Client Authentication**: Persistent client IDs with ownership verification
- **Physical Isolation**: Client data stored in separate subdirectories on chunk servers
- **Data Integrity**: CRC32 checksums verified at upload, storage, and download
- **Crash Recovery**: Master state persists via Write-Ahead Log (WAL) and periodic checkpointing
- **Automatic Failover**: Parity-based reconstruction when chunks are missing
- **Health Monitoring**: Heartbeat-based chunk server health tracking
- **Auto IP Detection**: Master and chunk servers automatically detect their network IP
- **Remote Client Support**: Clients can connect from different machines via `.master_config`
- **RESTful Web API**: HTTP/JSON API for file operations and system monitoring

## Technical Specs

- **Chunk Size**: 1 MB
- **Stripe**: 2 data chunks + 1 parity chunk (3 MB per stripe)
- **Checksum**: CRC32 (4 bytes)
- **Protocols**: gRPC with Protocol Buffers, REST API with HTTP/JSON
- **Language**: Go 1.24+
- **Web Framework**: Gin

## Prerequisites

- Go 1.24+ installed
- Protocol Buffers compiler (`protoc`)
- gRPC tools for Go

## Quick Start

### Local Setup (All on one machine)

#### 1. Build Binaries

```bash
make build
```

This creates binaries in `bin/` directory:
- `bin/master` - Master server
- `bin/chunkserver` - Chunk server
- `bin/client` - Client CLI
- `bin/webserver` - Web API server

#### 2. Start Master Server

```bash
# Terminal 1
make run-master
```

Master auto-detects its IP and runs on port `50051`. Check logs:
```bash
tail -f master.log
# Look for: "Master server IP: <your-ip>"
```

#### 3. Start Chunk Servers (in separate terminals)

```bash
# Terminal 2
make run-chunk_server1

# Terminal 3
make run-chunk_server2

# Terminal 4
make run-chunk_server3
```

Chunk servers run on ports `9001`, `9002`, `9003` with storage in `chunk_server1/`, `chunk_server2/`, `chunk_server3/`

#### 4. (Optional) Start Web Server

```bash
# Terminal 5
go run cmd/webserver/*.go -port 8080 -master 127.0.0.1:50051
```

Web server runs on `http://localhost:8080` and provides REST API access.

---

### Remote Setup (Client on different machine)

#### On Master Server Machine:

```bash
# Terminal 1: Start master (auto-detects IP)
make run-master

# Check master.log to see detected IP
tail -f master.log
# You'll see: "Master server IP: 192.168.1.65"

# Terminal 2-4: Start chunk servers with master IP
make run-chunk_server1 MASTER=192.168.1.65:50051
make run-chunk_server2 MASTER=192.168.1.65:50051
make run-chunk_server3 MASTER=192.168.1.65:50051
```

#### On Client Machine (e.g., Kali VM):

```bash
# One-time setup: Configure master address
make setup-master MASTER_IP=192.168.1.65

# This creates .master_config file with the master's address

# Now use normally
make upload FILE=myfile.pdf
make download FILE=myfile.pdf
make ls
```

**Note:** If your network changes and master gets a new IP, just run `make setup-master MASTER_IP=<new-ip>` again.

---

## Usage

### CLI Client

#### Upload a File

```bash
make upload FILE=myfile.pdf
```

This will:
- Generate a client ID (saved to `.client_id`)
- Split file into stripes
- Calculate CRC32 checksums
- Upload chunks in parallel to chunk servers
- Verify integrity on server side

#### Download a File

```bash
make download FILE=myfile.pdf
```

Downloaded file saved as `downloaded_myfile.pdf`

#### List Files

```bash
make ls
```

#### Delete a File

```bash
make delete FILE=myfile.pdf
```

---

### Web API

Start the web server:
```bash
go run cmd/webserver/*.go -port 8080 -master 127.0.0.1:50051
```

Or for remote master:
```bash
go run cmd/webserver/*.go -port 8080 -master 192.168.1.65:50051
```

#### API Endpoints

##### 1. Health Check
```bash
curl http://localhost:8080/health
```

Response:
```json
{"status": "healthy"}
```

##### 2. Upload File
```bash
curl -F "file=@myfile.pdf" http://localhost:8080/api/upload
```

Response:
```json
{
  "success": true,
  "filename": "myfile.pdf",
  "size": 123456,
  "clientId": 1868501793863308935,
  "message": "File uploaded successfully"
}
```

##### 3. List Files
```bash
curl http://localhost:8080/api/files
```

Response:
```json
{
  "clientId": 1868501793863308935,
  "files": ["myfile.pdf", "another.pdf"],
  "count": 2
}
```

##### 4. Download File
```bash
curl http://localhost:8080/api/download/myfile.pdf -o downloaded_myfile.pdf
```

##### 5. Delete File
```bash
curl -X DELETE http://localhost:8080/api/delete/myfile.pdf
```

Response:
```json
{
  "success": true,
  "filename": "myfile.pdf",
  "message": "File deleted successfully"
}
```

##### 6. Get File Chunk Locations
```bash
curl http://localhost:8080/api/files/myfile.pdf/chunks
```

Response:
```json
{
  "filename": "myfile.pdf",
  "size": 123456,
  "stripes": [
    {
      "stripeNumber": 1,
      "chunks": [
        {"type": "data1", "id": "myfile.pdf_chunk1_0001", "server": "192.168.1.65:9001"},
        {"type": "data2", "id": "myfile.pdf_chunk2_0001", "server": "192.168.1.65:9002"},
        {"type": "parity", "id": "myfile.pdf_parity_0001", "server": "192.168.1.65:9003"}
      ]
    }
  ]
}
```

##### 7. System Status
```bash
curl http://localhost:8080/api/system/status
```

Response:
```json
{
  "master": {
    "address": "192.168.1.65:50051",
    "status": "online"
  },
  "chunkServers": [
    {"id": 1, "address": "192.168.1.65:9001", "status": "online"},
    {"id": 2, "address": "192.168.1.65:9002", "status": "online"},
    {"id": 3, "address": "192.168.1.65:9003", "status": "online"}
  ]
}
```

##### 8. List Clients
```bash
curl http://localhost:8080/api/clients
```

Response:
```json
{
  "clients": [
    {
      "id": 1868501793863308935,
      "fileCount": 2,
      "files": ["myfile.pdf", "another.pdf"],
      "current": true
    }
  ]
}
```

---

## Makefile Commands

### Basic Commands
```bash
make build                    # Build all binaries
make clean                    # Remove binaries, data, and config files
make proto                    # Regenerate protobuf files (after editing dfs.proto)
```

### Server Commands
```bash
make run-master               # Start master server (auto-detects IP)
make run-chunk_server1        # Start chunk server 1 on port 9001
make run-chunk_server2        # Start chunk server 2 on port 9002
make run-chunk_server3        # Start chunk server 3 on port 9003
```

For remote chunk servers, pass master IP:
```bash
make run-chunk_server1 MASTER=192.168.1.65:50051
```

### Client Commands
```bash
make upload FILE=<filename>   # Upload a file
make download FILE=<filename> # Download a file
make delete FILE=<filename>   # Delete a file
make ls                       # List all files uploaded by this client
```

Override master address temporarily:
```bash
make upload FILE=test.pdf MASTER=192.168.1.65:50051
```

### Configuration
```bash
make setup-master MASTER_IP=192.168.1.65   # Create .master_config for remote master
make setup-master                          # Create .master_config for local master (127.0.0.1)
```

---

## How It Works

### Upload Flow

1. **Client** contacts **Master** with filename and size
2. **Master** assigns client ID (first time) and allocates chunks
3. **Client** streams file in stripes:
   - Reads 2 MB chunks (data1, data2)
   - Calculates parity: `data1 ⊕ data2`
   - Computes CRC32 for each chunk
4. **Client** uploads 3 chunks per stripe in parallel
5. **ChunkServers** verify checksums before storing and acknowledge the client
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

---

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
│   │   ├── main.go              # ChunkServer startup
│   │   ├── chunkservertask.go   # WriteChunk, ReadChunk, DeleteChunks
│   │   ├── inventory.go         # Inventory reporting and reconstruction
│   │   └── checksum.go          # CRC32 calculation
│   ├── client/
│   │   ├── main.go              # Client CLI (upload/download)
│   │   ├── stripe_reader.go     # File streaming in stripes
│   │   ├── parallel_upload.go   # Parallel chunk uploads
│   │   ├── download_stripe.go   # Parallel downloads with reconstruction
│   │   ├── parity.go            # XOR parity calculation
│   │   └── checksum.go          # CRC32 checksums
│   └── webserver/
│       ├── main.go              # Web server startup
│       ├── handlers.go          # HTTP API handlers
│       ├── dfs_client.go        # DFS operations wrapper
│       └── checksum.go          # CRC32 calculation
├── dfspb/
│   ├── dfs.pb.go               # Generated protobuf code
│   └── dfs_grpc.pb.go          # Generated gRPC code
├── dfs.proto                   # Protocol definitions
├── Makefile                    # Build and run commands
└── README.md                   # This file
```

---

## Data Integrity Layers

1. **Client Upload**: Calculate CRC32, send with chunk
2. **ChunkServer Receive**: Verify CRC32 matches, reject if corrupted
3. **Client Download**: Verify CRC32 on received data

All three layers ensure end-to-end integrity.

---

## Client Authentication

- First upload generates unique client ID (saved to `.client_id`)
- Master tracks which client owns which files
- Downloads require ownership verification
- Physical isolation: chunks stored in `chunk_server/client_id/` directories

---

## Web API Architecture

```
Web Browser / curl
      ↓
  REST API Server (Port 8080)
      ↓ (uses same .client_id)
  DFS Client Library
      ↓ (gRPC)
  Master Server (Port 50051) → Chunk Servers (9001-9003)
```

The web server acts as a REST wrapper around the DFS client library, using the same `.client_id` file for authentication. This means CLI uploads are visible via the web API and vice versa.

---

## Example Workflow

### Complete test using Web API:

```bash
# 1. Check system status
curl http://localhost:8080/api/system/status

# 2. Upload files
curl -F "file=@document1.pdf" http://localhost:8080/api/upload
curl -F "file=@document2.pdf" http://localhost:8080/api/upload

# 3. List uploaded files
curl http://localhost:8080/api/files

# 4. Check where chunks are stored
curl http://localhost:8080/api/files/document1.pdf/chunks

# 5. Download a file
curl http://localhost:8080/api/download/document1.pdf -o retrieved.pdf

# 6. Verify integrity
md5 document1.pdf retrieved.pdf

# 7. Delete a file
curl -X DELETE http://localhost:8080/api/delete/document2.pdf

# 8. Verify deletion
curl http://localhost:8080/api/files
```

---

## Testing Upload/Download via CLI and Web API

```bash
# Upload via CLI
make upload FILE=test.pdf

# Verify via Web API
curl http://localhost:8080/api/files

# Download via Web API
curl http://localhost:8080/api/download/test.pdf -o web_download.pdf

# Both should have same checksum
md5 test.pdf web_download.pdf
```

---

## Dependencies

```bash
# Install Gin web framework
go get -u github.com/gin-gonic/gin

# Update dependencies
go mod tidy
```