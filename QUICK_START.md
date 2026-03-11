# XorFS Quick Start (Mac + Kali VM)

## Copy-Paste Ready Commands

### Prerequisites
```bash
# On both Mac and Kali VM
make build
```

---

## Terminal Setup

Open 6 terminals total:
- **Mac**: 4 terminals (primary, chunkserver1, chunkserver2, client)
- **Kali VM**: 2 terminals (secondary, chunkserver3)

---

## Execution Order (CRITICAL: Secondary First!)

### Terminal 1: Kali VM - Secondary Master
```bash
make run-master-secondary MY_ADDR=192.168.1.20:50052
```
✓ Wait for: `Secondary watchdog started (timeout=10s)`

---

### Terminal 2: Mac - Primary Master
```bash
make run-master-primary MY_ADDR=192.168.1.87:50051 SECONDARY_ADDR=192.168.1.20:50052
```
✓ Wait for: `Primary mode: will send heartbeats to secondary`

---

### Terminal 3: Mac - Chunkserver 1
```bash
make run-chunk_server1 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
```
✓ Wait for: `ChunkServer running on 0.0.0.0:9001`

---

### Terminal 4: Mac - Chunkserver 2
```bash
make run-chunk_server2 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
```
✓ Wait for: `ChunkServer running on 0.0.0.0:9002`

---

### Terminal 5: Kali VM - Chunkserver 3
```bash
make run-chunk_server3 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
```
✓ Wait for: `ChunkServer running on 0.0.0.0:9003`

---

### Terminal 6: Mac - Client
```bash
make set-master MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
```

---

## Test Basic Operations

```bash
# Upload
make upload FILE=test.pdf

# List
make ls

# Download
make download FILE=test.pdf

# Delete
make delete FILE=test.pdf
```

---

## Test Failover

### Step 1: Verify everything works
```bash
make upload FILE=failover-test.pdf
make ls
```

### Step 2: Kill primary (Ctrl+C in Terminal 2)
Watch logs:
- Secondary logs: `>>> THIS NODE IS NOW THE ACTIVE PRIMARY MASTER <<<`
- Chunkservers: `FAILOVER: switching active master...`

### Step 3: Test still works on secondary
```bash
make ls
```
✓ Should succeed with ~2 second delay

### Step 4: Download from secondary
```bash
make download FILE=failover-test.pdf
```
✓ Verify downloaded file is identical

---

## Quick Logs

```bash
# View master logs
tail -f log_files/master.log

# View chunkserver logs
tail -f log_files/chunkserver.log

# View failover in action
grep -i failover log_files/*.log
grep -i promotion log_files/*.log
```

---

## Cleanup

```bash
# Remove all data and logs
make clean

# Or just logs
rm -f log_files/*.log *.wal *.checkpoint
```

---

## Network Verification

```bash
# Verify connectivity
ping 192.168.1.20           # From Mac
ping 192.168.1.87           # From Kali
nc -zv 192.168.1.87 50051  # Test master ports
nc -zv 192.168.1.20 50052
```

---

## If Something Goes Wrong

1. **Chunkserver can't connect**: Check primary/secondary are running
2. **Client hangs**: Primary down? Check: `make ls` fails immediately if secondary unreachable
3. **Failover doesn't trigger**: Make sure secondary started BEFORE primary
4. **Data lost after restart**: WAL files might be corrupted - remove .wal files and restart

