# XorFS: Distributed File System with RAID-5 Defense Guide

## 1. Project Overview
**XorFS** is a fault-tolerant, distributed file system designed to store large files across multiple servers reliably. It implements **RAID-5 Erasure Coding** to achieve data redundancy with minimal storage overhead (1.5x vs 3x for replication). The system is built in Go using **gRPC** for communication, ensuring high-performance, low-latency data transfer.

---

## 2. Problem Statement
In the era of Big Data, storing massive amounts of data on a single machine is risky and inefficient:
1.  **Single Point of Failure**: If the disk crashes, data is lost forever.
2.  **Storage Costs**: Traditional replication (storing 3 copies) is expensive (300% storage cost).
3.  **Performance Bottlenecks**: Reading from a single server is slow for multiple concurrent users.

**Objective**: To build a storage system that is **reliable** (survives failures), **efficient** (low storage cost), and **fast** (parallel I/O).

---

## 3. Unique Aspects (Why is this cool?)

1.  **RAID-5 Erasure Coding (Smart Redundancy)**
    *   Instead of making 3 full copies of a file (Replication), we split files into "stripes" of 2 data chunks + 1 parity chunk.
    *   **Benefit:** Saves 50% storage space compared to 3-way replication while tolerating the loss of *any* single chunk server.

2.  **Client-Side Intelligence**
    *   The client calculates the parity (XOR) and checksums *locally* before uploading.
    *   Reduces load on the master server and distributes computation power (Edge Computing principle).

3.  **High-Performance Parallelism**
    *   Uploads and downloads happen in parallel.
    *   We stream data: We don't read the whole 1GB file into memory; we process it in small stripes (streaming pipeline), keeping memory usage low.

4.  **Atomic Metadata & Crash Recovery**
    *   Master server uses a **Write-Ahead Log (WAL)** and **Checkpointing**.
    *   If the master crashes (power cut), it replays the WAL on restart to restore the exact state of the system—zero metadata loss.

---

## 4. Technical Implementation Details

### A. The Core Logic: RAID-5 Striping
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

---

## 5. Defense Q&A Cheatsheet (Extended)

**Q1: Why RAID-5 and not RAID-1 (Mirroring)?**
**A:** Efficiency. RAID-1 requires 300% storage (3 copies) to tolerate 1 failure. RAID-5 requires only 150% storage (1.5 copies) to tolerate 1 failure. For large scale storage (Petabytes), 50% savings is improved cost efficiency.

**Q2: What happens if the Master node fails?**
**A:** The system temporarily pauses (CP in CAP theorem), but NO metadata is lost. We use a **Write-Ahead Log (WAL)**. When the Master restarts, it replays the log to restore the file map. In a production version, we would use a secondary backup master (Raft consensus) for high availability.

**Q3: Doesn't calculating parity on the Client slow down the upload?**
**A:** Not really. CPU XOR operations are extremely fast (nanoseconds). The bottleneck is usually the Network I/O. By calculating on the client, we avoid sending 2x data to the master to calculate parity there. It actually *saves* network bandwidth.

**Q4: How does your system handle "Odd Chunks" (e.g., 3MB file with 2MB stripe)?**
**A:** We implemented a special case logic. The last stripe has only 1 Data chunk. We treat the 2nd Data chunk as "Zero Padding" for XOR calculation, but we don't actually store the zero chunk. During download, if we see we only expect 1 data chunk, we just download it unless it's missing (then we use Parity).

**Q5: What is the "Consistency Model" of your system?**
**A:** We follow **Strong Consistency**. A file is only visible to other clients (ListFiles) *after* the client sends the final `ConfirmWrite` signal to the Master. This prevents users from seeing half-uploaded (corrupt) files.

**Q6: Why use gRPC and not REST API?**
**A:** gRPC uses **Protocol Buffers** which are binary (smaller payload than JSON) and strongly typed. It also supports **HTTP/2**, allowing multiplexing (multiple requests over one connection) and streaming, which is critical for our file upload pipeline performance.

**Q7: How do you handle Data Corruption (Bit rot)?**
**A:** We use **CRC32 Checksums**. The client calculates checksum on upload and stores it in the Master. On download, the client re-calculates the checksum of the received data. If they don't match, we know the data is corrupt and can try to reconstruct it from parity instead.

**Q8: Scalability - What if we add a 4th or 5th server?**
**A:** The Master server's allocation logic scans for "Healthy" servers. If new servers join, they send heartbeats. The Master will automatically start assigning new stripes to these new servers, balancing the load over time.

**Q9: What if TWO Chunk Servers fail at the same time?**
**A:** RAID-5 can only tolerate ONE failure per stripe. If two fail, that stripe is lost. To handle 2 failures, we would need **RAID-6** (Double Parity), which adds more complexity and storage overhead (2 parity chunks).

**Q10: Explain the "Pipeline" pattern you mentioned.**
**A:** Instead of `Read Whole File -> Upload Whole File` (which uses GBs of RAM), we use a Go Channel pipeline:
1.  **Producer**: Reads 2MB from disk -> sends to channel.
2.  **Consumer**: Reads from channel -> Uploads to servers.
This allows us to upload a 10GB file using only ~6MB of RAM!

**Q11: Why did you choose 1MB as chunk size?**
**A:** It's a trade-off.
*   **Too small (e.g., 4KB):** Too much metadata overhead on the Master (millions of chunk IDs).
*   **Too large (e.g., 1GB):** Moving chunks becomes slow; retry on failure is painful (re-uploading 1GB vs 1MB).
*   **1MB - 64MB** is the industry standard (GFS uses 64MB). We chose 1MB to make testing easy on local machines.

---

## 6. Future Scope & Innovation (The "What's New?" Section)

**Q12: What is new in this project vs existing systems (HDFS, GFS)?**
**A:**
1.  **Parity Implementation**: Most student versions of GFS implement straightforward **replication** (3 copies). We challenged ourselves to implement **RAID-5 (XOR Parity)**, which is mathematically more complex but storage-efficient.
2.  **Client-Side Parity**: Traditional GFS often does replication centrally. We moved the XOR logic to the **Client**. This is an architectural innovation that distributes the CPU load to the edge, making the central cluster more performant.
3.  **Odd-Chunk Handling**: Standard RAID maps cleanly to hardware Disks. Mapping logical file chunks to RAID stripes with variable file sizes (odd chunks) required custom algorithmic work (padding logic) that standard libraries don't solve for you.

**Q13: How is it different from existing world systems?**
**A:**
*   **HDFS**: Uses Replication by default (300% storage overhead). Only recently added Erasure Coding (similar to our system) in HDFS 3.0 as an advanced feature. We built our entire system *core* around this advanced feature.
*   **Google Drive/Dropbox**: These use massive object stores (S3-like). Our system is a **Block Store** pattern, giving us lower-level control over data placement and recovery.

**Q14: What would you implement next? (Future Work)**
**A:**
1.  **RAID-6 (Double Parity)**: To tolerate 2 simultaneous server failures (using Reed-Solomon codes instead of XOR).
2.  **Data Deduplication**: Hash chunks before uploading. If a chunk with same hash exists (e.g., duplicate email attachments), just point to existing chunk. Drastically saves space.
3.  **Geo-Replication**: extend the system so that Chunk Server 1 is in US, Chunk Server 2 in EU. This would require handling high-latency networks, which would require upgrading our Pipeline code.
4.  **Master High Availability**: Use **Raft Consensus Algorithm** to have 3 Master servers, so if one Master fails, another takes over automatically (removing the Single Point of Failure).

**Q15: Why are you doing this if GFS already exists?**
**A:** To solve the **Storage Efficiency vs Complexity** trade-off differently. GFS prioritizes simplicity (replication). We prioritized storage cost (Erasure Coding). In a resource-constrained environment (like a small startup or university cluster), buying 1.5TB of disk is cheaper than 3TB. Our architecture proves that we can get enterprise-grade reliability on cheaper hardware constraints.
