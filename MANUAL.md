# 📖 XORFS — Distributed File System: The Complete Manual

Welcome to the comprehensive guide for **XorFS**, a high-performance, fault-tolerant, and distributed file system built from the ground up in Go. This manual explains the architecture, feature set, implementation logic, performance factors, and deployment instructions for your project.

---

## 1. 🚀 System Overview

**XorFS** (XOR File System) is a distributed storage solution designed for reliability and high availability. It uses **RAID-5 (XOR-based erasure coding)** to provide data redundancy with minimal storage overhead.

### Key Architecture Components
*   **Primary Master**: The "brain" that manages file metadata, user authentication, and chunk allocation.
*   **Secondary Master**: A standby replica that monitors the primary and automatically promotes itself to "Active" if the primary fails.
*   **Chunk Servers**: The storage nodes where actual file data (chunks) and parity information are persisted.
*   **DFS Client (CLI & Web API)**: The interface used by users to perform operations like `upload`, `download`, `ls`, `mkdir`, and `rmdir`.

---

## 2. ✨ Features

### Reliability & Fault Tolerance
- [x] **RAID-5 Erasure Coding**: Stripe-based storage (2 Data + 1 Parity). Survives the loss of 1 storage node per stripe.
- [x] **CRC32 Checksums**: Data integrity is verified at every step (client-to-server, server-to-disk, and disk-to-client).
- [x] **Odd-Chunk Edge Case Handling**: Correctly handles files with an odd number of blocks by using zero-padding for the last stripe's parity calculation.

### High Availability (HA)
- [x] **Automatic Failover**: Secondary Master detects primary failure and promotes itself within ~6 seconds.
- [x] **Live WAL Synchronization**: Standby master continuously replays the primary's Write-Ahead Log (WAL) to stay in-sync.
- [x] **Active Master Discovery**: Chunk servers and clients automatically re-route requests to the promoted secondary by reading a distributed `.master_addr` file.

### Advanced File Management
- [x] **Hierarchical Folders**: Full directory support (`mkdir`, `rmdir`, `mv` into folders).
- [x] **File Preview (`cat`)**: Preview file content without a full download by streaming specific chunks from chunk servers.
- [x] **Detailed Metadata**: Tracks file sizes, ownership, and upload timestamps.

---

## 3. 🛠️ Technologies & How They Work Together

| Technology | Role in XORFS |
| :--- | :--- |
| **Go (Golang)** | The core language used for the entire backend (Master, ChunkServer, Client). Chosen for its high concurrency support via Goroutines. |
| **gRPC & Protobuf** | The communication backbone. All services talk via gRPC using binary Protocol Buffers for maximum speed and type safety. |
| **RAID-5 (XOR)** | Our erasure coding strategy. Provides 1-node fault tolerance with only 50% storage overhead (compared to 200% for classic replication). |
| **Gin Gonic** | The web framework used for the Web API that serves the frontend. |
| **React & Vite** | The modern frontend stack providing the user dashboard. |
| **Bash/Makefile** | Orchestration scripts for managing multi-device LAN deployments. |

---

## 4. 🧠 Implementation Deep Dive

### 4.1 RAID-5 Implementation
XorFS splits files into **1 MB chunks**. For every two data chunks (`D1`, `D2`), we calculate a parity chunk (`P`) using the XOR operation:
`P = D1 ⊕ D2`

*   **Upload**: The client reads the file, stripes it, calculates XOR in-memory, and uploads `D1`, `D2`, and `P` to three different chunk servers in parallel.
*   **Download**: The client fetches all three. If any one chunk server is offline, it reconstructs the missing data:
    `D1 = D2 ⊕ P` or `D2 = D1 ⊕ P`.

### 4.2 High Availability & Failover
- **WAL (Write-Ahead Log)**: Every change to the filesystem is written to a disk log *before* updating memory. This ensures that if the master crashes, no data is lost.
- **Checkpointing**: Every 5 minutes, the master saves a full system state "snapshot" to `master.checkpoint` to speed up recovery.
- **Failover Logic**: 
    1. Secondary Master pings Primary every 2s.
    2. After 3 missed pings, Secondary assumes control.
    3. Secondary updates `.master_addr` with its own IP.
    4. ChunkServers re-read `.master_addr` every 5s and reconnect to the new Active Master.

---

## 5. 📊 Performance Factors & Measurements

### 5.1 Performance Factors
1.  **Stripe Size (1MB)**: Larger chunks reduce metadata overhead but increase the cost of small file updates.
2.  **Parallelism**: The client uploads 3 chunks of a stripe simultaneously. This saturates the network bandwidth effectively.
3.  **XOR Overhead**: XOR is a "bitwise" operation, meaning it is extremely fast and has near-zero impact on CPU performance.
4.  **Network Latency**: In a LAN, the primary bottleneck is the Wi-Fi/Ethernet speed (e.g., 100Mbps or 1Gbps).

### 5.2 How to Measure Performance
*   **Throughput (MB/s)**:
    ```bash
    # Measure upload speed
    time make upload FILE=big_file.iso
    # Calculation: (File Size in MB) / (Total seconds) = MB/s
    ```
*   **Latency (ms)**:
    - Check `log_files/master_stdout.log`. Every gRPC request is logged with its processing time.
*   **Failover Time**:
    - Kill the primary master and use a stopwatch to see how long it take for the secondary master log to say `"Server is now ACTIVE"`. (Expected: ~6 seconds).
*   **Fault Tolerance Test**:
    - Disconnect one chunk server while a file is downloading. The client should continue downloading without error by switching to "Reconstruction Mode".

---

## 6. 🏁 User Guide: Running the System

### 6.1 Prerequisites
*   Go 1.21 or higher installed.
*   All devices on the same Wi-Fi/LAN.

### 6.2 Multi-Device (LAN) Setup
1.  **Configure IPs**: Edit `cluster.conf` and input the LAN IPs of your devices.
2.  **Build**: Run `make build` on all devices.
3.  **Start Primary (Device 1)**: `make run-master-lan MASTER=192.168.1.10:50051 SECONDARY=192.168.1.11:50052`
4.  **Start Secondary (Device 2)**: `make run-secondary-lan MASTER=192.168.1.10:50051 SECONDARY=192.168.1.11:50052`
5.  **Start ChunkServers (Device 3+)**: `make run-chunk-lan SLOT=1 MASTER=192.168.1.10:50051 SECONDARY=192.168.1.11:50052`

### 6.3 Using the CLI
```bash
make register              # Create your 6-digit PIN
make ls                    # List uploaded files
make mkdir FOLDER=docs     # Create a folder
make upload FILE=f.txt     # Upload a file to DFS
make cat FILE=f.txt        # Preview file content
```

### 6.4 Using the Web UI
Open `http://<Device1-IP>:5173` in any browser on the network.
*   **Login**: Use your Username and PIN.
*   **Upload/Download**: Interactive drag-and-drop support.
*   **Dashboard**: Monitor chunk server health.

---

## 7. 🛠️ Troubleshooting
- **Firewall**: Ensure ports `50051`, `50052`, `9001-9003`, `8080`, and `5173` are open.
- **Master Unknown**: If the client can't find the master, delete `.master_addr` and restart the masters; they will recreate it correctly.
- **Sync Issues**: Use `make lan-sync` from the Primary to manualy push the latest metadata to the secondary.

---
**XorFS Project — Distributed Systems 2024**
