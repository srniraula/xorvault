# XorFS Deployment Guide

## Quick Reference: Two-Machine Setup (Mac + Kali VM)

### Your Network Setup
- **Mac**: 192.168.1.87 (primary master + chunkservers 1, 2)
- **Kali VM**: 192.168.1.20 (secondary master + chunkserver 3)
- **Network**: Both on 192.168.1.0/24 subnet ✓ (connectivity verified)

### Prerequisites
- Both machines on same subnet ✓ (verified: `nc -zv 192.168.1.66 50052` works)
- Go installed on both
- Build binaries: `make build` on both machines

### Startup Sequence

**IMPORTANT: Start secondary BEFORE primary!**

#### Step 1: Start Secondary Master on Kali VM
```bash
ssh kali@192.168.1.20
cd /path/to/xorfs
make run-master-secondary MY_ADDR=192.168.1.20:50052
```

Expected output:
```
MASTER: Secondary watchdog started (timeout=10s)
MASTER: Waiting for heartbeats from primary...
```

#### Step 2: Start Primary Master on Mac (Terminal 1)
```bash
cd /path/to/xorfs
make run-master-primary MY_ADDR=192.168.1.87:50051 SECONDARY_ADDR=192.168.1.20:50052
```

Expected output:
```
MASTER: Primary mode: will send heartbeats to secondary at 192.168.1.20:50052
Master running on 192.168.1.87:50051 — Logs to master.log
```

#### Step 3: Start Chunkserver 1 on Mac (Terminal 2)
```bash
make run-chunk_server1 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
```

#### Step 4: Start Chunkserver 2 on Mac (Terminal 3)
```bash
make run-chunk_server2 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
```

#### Step 5: Start Chunkserver 3 on Kali VM (Terminal 2)
```bash
ssh kali@192.168.1.20
cd /path/to/xorfs
make run-chunk_server3 MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
```

#### Step 6: Configure Client on Mac (Terminal 4)
```bash
make set-master MASTER_ADDR=192.168.1.87:50051 SECONDARY_MASTER_ADDR=192.168.1.20:50052
```

Expected output:
```
Wrote .master_addr with 192.168.1.87:50051
Wrote .secondary_master_addr with 192.168.1.20:50052
```

#### Step 7: Test Operations
```bash
# Upload
make upload FILE=test.pdf

# List files
make ls

# Download
make download FILE=test.pdf

# Delete
make delete FILE=test.pdf
```

---

## Testing Failover

### Test 1: Primary Master Failure
1. All services running (from Steps 1-5 above)
2. Upload a file: `make upload FILE=test.pdf`
3. Kill primary master (Ctrl+C in primary terminal)
4. Wait 10 seconds
5. Check secondary logs: `PROMOTION: THIS NODE IS NOW THE ACTIVE PRIMARY MASTER`
6. Check chunkserver logs: `FAILOVER: switching active master from 192.168.1.87:50051 to 192.168.1.20:50052`
7. Try client operation: `make ls` (should work on secondary)

### Test 2: Chunkserver Failure
1. Kill any chunkserver (Ctrl+C)
2. Storage is isolated per chunkserver — no data loss
3. RAID-5 reconstruction kicks in: missing chunks are reconstructed from parity
4. Downloads still succeed using remaining chunks

### Test 3: Network Partition
1. Disconnect primary master from network (unplug eth/wifi)
2. Within 10 seconds: secondary promotes itself
3. Chunkservers detect primary unreachable after ~15 seconds
4. All operations continue on secondary

---

## Troubleshooting

### Chunkservers can't reach master
**Symptoms**: `MasterTracker: heartbeat to 192.168.1.87:50051 failed`

**Fix**: 
- Check IPs are correct: `ifconfig` on Mac, `hostname -I` on Kali
- Verify network: `ping 192.168.1.87` from Kali, `ping 192.168.1.20` from Mac
- Check firewall: `sudo ufw allow 50051:50052` on Kali
- Check master is running: `ps aux | grep bin/master`

### Client commands hang
**Symptoms**: `make ls` takes 2+ seconds or times out

**Cause**: Primary master unreachable, trying secondary fallback (or secondary also down)

**Fix**:
- Check primary is running: `ps aux | grep bin/master` on Mac
- Check secondary is running: `ssh kali@192.168.1.20 "ps aux | grep bin/master"`
- Verify connectivity: `nc -zv 192.168.1.87 50051` and `nc -zv 192.168.1.20 50052` from Mac
- Check client config: `cat .master_addr` and `cat .secondary_master_addr`

### WAL/Checkpoint not syncing
**Symptoms**: Data lost after primary restarts

**Fix**:
- Ensure secondary applied WAL entries: check logs for `Secondary: applied WAL seq`
- Wait 5 minutes for checkpoint: `ls -la master.checkpoint`
- Check replication lag: compare `wal_seq` in logs on primary vs secondary
- Check primary is sending entries: look for `Primary: WAL entry sent to secondary` in logs

### Failover doesn't happen
**Symptoms**: Secondary doesn't promote even after primary dies

**Cause**: Secondary not receiving heartbeats or not configured as watchdog

**Fix**:
- Start secondary BEFORE primary (critical!)
- Verify secondary started without `-secondary` flag (watchdog mode)
- Check logs for `Secondary watchdog started (timeout=10s)`
- Increase timeout in code if network is slow: change `10` to `30` in `secondary.go`

---

## Deployment Checklist

- [ ] Both machines on same subnet / can ping each other
- [ ] Go 1.24+ installed on both
- [ ] `make build` successful on both
- [ ] Secondary master started FIRST
- [ ] Primary master started with correct SECONDARY_ADDR
- [ ] All chunkservers have both MASTER_ADDR and SECONDARY_MASTER_ADDR
- [ ] Client configured with both addresses
- [ ] Test upload → download cycle works
- [ ] Test failover (kill primary)
- [ ] Verify secondary promoted automatically

---

## For Three-Machine Demo

Replace IP addresses in commands:
- Laptop A: `192.168.1.100` (Primary Master + Chunkserver1)
- Laptop B: `192.168.1.101` (Secondary Master + Chunkserver2)
- Laptop C: `192.168.1.102` (Chunkserver3 + Client)

**Same sequence**, just distribute the processes across all 3 machines.

Example for Laptop A:
```bash
make run-master-primary MY_ADDR=192.168.1.100:50051 SECONDARY_ADDR=192.168.1.101:50052
make run-chunk_server1 MASTER_ADDR=192.168.1.100:50051 SECONDARY_MASTER_ADDR=192.168.1.101:50052
```

Example for Laptop B:
```bash
make run-master-secondary MY_ADDR=192.168.1.101:50052
make run-chunk_server2 MASTER_ADDR=192.168.1.100:50051 SECONDARY_MASTER_ADDR=192.168.1.101:50052
```

Example for Laptop C:
```bash
make run-chunk_server3 MASTER_ADDR=192.168.1.100:50051 SECONDARY_MASTER_ADDR=192.168.1.101:50052
make set-master MASTER_ADDR=192.168.1.100:50051 SECONDARY_MASTER_ADDR=192.168.1.101:50052
make upload FILE=test.pdf
```
