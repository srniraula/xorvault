# XorFS: Distributed File System with RAID-5 Defense Guide

## 1. Project Overview
**XorFS** is a distributed file system implemented in Go that provides fault-tolerant file storage using **RAID-4 Erasure Coding**. Unlike traditional systems that simply replicate data (taking up 3x storage), XorFS uses XOR-based parity to ensure data survivability with only **1.5x storage overhead**, while maintaining high performance through parallel I/O.

---

## 2. Unique Aspects (Why is this cool?)

1.  **RAID-4 Erasure Coding (Smart Redundancy)**
    *   Instead of making 3 full copies of a file (Replication), we split files into "stripes" of 2 data chunks + 1 parity chunk.
    *   **Benefit:** Saves 50% storage space compared to 3-way replication while tolerating the loss of *any* single chunk server.

2.  **Client-Side Intelligence**
    *   The client calculates the parity (XOR) and checksums *locally* before uploading.
    *   Reduces load on the master server and distributes computation power.
    *   **Smart Reconstruction**: If a server is down during download, the *client* automatically rebuilds the missing data on the fly using available chunks.

3.  **High-Performance Parallelism**
    *   Uploads and downloads happen in parallel.
    *   We stream data: We don't read the whole 1GB file into memory; we process it in small stripes (streaming pipeline), keeping memory usage low.

4.  **Atomic Metadata & Crash Recovery**
    *   Master server uses a **Write-Ahead Log (WAL)** and **Checkpointing**.
    *   If the master crashes (power cut), it replaying the WAL on restart to restore the exact state of the system—zero metadata loss.

---

## 3. Technical Implementation Details

### A. The Core Logic: RAID-4 Striping
For a file `big.pdf` (3 MB):
*   **Stripe 1**:
    *   `Chunk 1` (Data): Sent to Server A
    *   `Chunk 2` (Data): Sent to Server B
    *   `Parity 1` (XOR): `Chunk 1 ⊕ Chunk 2` → Sent to Server C
*   **Stripe 2** (Odd number of chunks handling):
    *   `Chunk 3` (Data): Sent to Server A
    *   `Chunk 4` (Empty/Padding): Virtual
    *   `Parity 2` (XOR): `Chunk 3 ⊕ 0` → Sent to Server B (or C)

**The Math:** `A ⊕ B = P`.
*   If A is lost: `P ⊕ B = A`
*   If B is lost: `P ⊕ A = B`
*   If P is lost: Ignore (we have A and B)

### B. Upload Workflow (Streaming Pipeline)
1.  **Handshake**: Client asks Master to create file `big.pdf`. Master assigns a unique Client ID.
2.  **Allocation**: Master checks heartbeat of active ChunkServers and returns an allocation map (which chunk goes where).
3.  **Streaming**:
    *   Client reads file in 2MB blocks (Stripe size).
    *   Calculates Parity in memory.
    *   Calculates CRC32 Checksum for integrity.
4.  **Parallel Upload**: Client uploads Data1, Data2, and Parity to 3 different servers *simultaneously* using gRPC.
5.  **Confirmation**: Once all 3 acknowledge receipt, Client signals Master to "Commit" the chunk info.

### C. Download & Fault Tolerance (The "Self-Healing" Read)
1.  **Fetch Metadata**: Client asks Master where the chunks for `big.pdf` are.
2.  **Parallel Fetch**: Client requests chunks from all 3 servers at once.
3.  **Failure Handling**:
    *   **Scenario 1: All Servers Up**: Client downloads Data1 & Data2. Puts them together. Done.
    *   **Scenario 2: One Server Down**:
        *   Client detects connection failure (or file missing).
        *   It downloads the remaining Data chunk + Parity chunk.
        *   **Reconstruction**: Performs `AvailableData ⊕ Parity` to instantly regenerate the missing data.
        *   To the user, this is seamless.
4.  **Integrity Check**: Client verifies CRC32 checksum of downloaded data against the one stored during upload. If it mismatches, it reports data corruption.

### D. Master Server Internals
*   **In-Memory Maps**: Stores file metadata `filename -> [stripes] -> [chunk locations]`.
*   **Heartbeat Monitor**: Every 5 seconds, ChunkServers ping the Master ("I am alive"). Master maintains a "Active Servers" list to ensure we don't assign chunks to dead servers.
*   **WAL (Write Ahead Log)**:
    *   Before updating memory, every operation (Create, Allocate, Delete) is written to `master.wal` on disk.
    *   On startup, Master reads `master.wal` line-by-line to rebuild its memory.

---

## 4. Key Challenges & How We Solved Them

1.  **The "Odd Chunk" Problem**
    *   *Challenge*: When a file has an odd number of chunks (e.g., 3 chunks), the last stripe only has 1 Data chunk. Reconstruction logic was failing because it expected 2 Data chunks.
    *   *Solution*: Modified logic to track "Expected Chunks". If we expect 1 and have 1, we treat it as success, ignoring the missing 2nd chunk.

2.  **Server Failure during Download**
    *   *Challenge*: What if a server dies mid-download?
    *   *Solution*: Implemented a smart `download_stripe` function that catches connection errors, logs them, and triggers the reconstruction algorithm only if needed.

3.  **Ensuring Data Integrity**
    *   *Challenge*: Network glitches might corrupt bits.
    *   *Solution*: Added CRC32 checksums. The ChunkServer verifies checksum on receipt, and Client verifies it again on download.

---

## 5. Defense Q&A Cheatsheet

**Q: Why RAID-5 and not RAID-1 (Mirroring)?**
**A:** Efficiency. RAID-1 requires 300% storage (3 copies) to tolerate 2 failures. RAID-5 requires only 150% storage (1.5 copies) to tolerate 1 failure. For large scale storage, 50% savings is massive.

**Q: What happens if the Master node fails?**
**A:** The system pauses. However, no data is lost. We restart the Master, and it recovers its state from the `master.wal` and `checkpoint` files instantly.

**Q: What happens if 2 Chunk Servers fail simultaneously?**
**A:** RAID-5 cannot recover from 2 simultaneous failures in the same stripe. We would report data loss for that specific file. (To fix this, we'd need RAID-6).

**Q: Is the system scalable?**
**A:** Yes. We can add more ChunkServers dynamically. The Master will start assigning new file chunks to the new servers immediately (based on the Heartbeat/Healthy list).
