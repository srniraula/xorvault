# XORFS LAN Distributed Setup Guide

This guide explains how to run XORFS with each component on its own physical device
connected to the same LAN (e.g. university lab, home network, or hotspot).

---

## Architecture Overview

```
     ┌────────────────────────────────────────────────────────────┐
     │                        LAN  (192.168.1.x)                  │
     │                                                            │
     │  ┌──────────────────────┐   ┌──────────────────────────┐  │
     │  │  Device A (Master)   │   │  Device B (Secondary)    │  │
     │  │  192.168.1.100       │   │  192.168.1.101           │  │
     │  │                      │ ← │  Standby — promotes on   │  │
     │  │  bin/master (active) │   │  primary failure         │  │
     │  │  bin/webserver       │   │  bin/master (standby)    │  │
     │  │  web/frontend        │   │                          │  │
     │  └──────────┬───────────┘   └──────────────────────────┘  │
     │             │  gRPC heartbeats / chunks                    │
     │   ┌─────────┴──────────────────────────────┐              │
     │   │                                        │              │
     │  ┌▼───────────────┐  ┌────────────────┐  ┌▼─────────────┐│
     │  │  Device C       │  │  Device D      │  │  Device E    ││
     │  │  192.168.1.102  │  │  192.168.1.103 │  │ 192.168.1.104│
     │  │  Chunk Server 1 │  │  Chunk Srv 2   │  │  Chunk Srv 3 ││
     │  │  port 9001      │  │  port 9002     │  │  port 9003   ││
     │  └─────────────────┘  └────────────────┘  └──────────────┘│
     │                                                            │
     │  Any device on LAN → http://192.168.1.100:5173  (Web UI)  │
     └────────────────────────────────────────────────────────────┘
```

---

## Step 1 — Edit `cluster.conf` (once, on any device)

Open `cluster.conf` in the project root and set the **real LAN IPs** of each device:

```ini
PRIMARY_MASTER_IP=192.168.1.100      # Device A
PRIMARY_MASTER_PORT=50051

SECONDARY_MASTER_IP=192.168.1.101    # Device B (optional)
SECONDARY_MASTER_PORT=50052

CHUNK_SERVER_1_IP=192.168.1.102      # Device C (member 1)
CHUNK_SERVER_1_PORT=9001

CHUNK_SERVER_2_IP=192.168.1.103      # Device D (member 2)
CHUNK_SERVER_2_PORT=9002

CHUNK_SERVER_3_IP=192.168.1.104      # Device E (member 3)
CHUNK_SERVER_3_PORT=9003

WEB_API_IP=192.168.1.100
WEB_API_PORT=8080

FRONTEND_PORT=5173

SECONDARY_SSH_USER=nissan            # SSH user on Device B for rsync
```

---

## Step 2 — Distribute the project

Copy the **entire project folder** to every device that will run a component.

**Option A — via USB / file transfer:**
```bash
# On Device A (master), tar the project:
tar czf xorfs.tar.gz /path/to/xorfs/

# Copy xorfs.tar.gz to every device, then extract:
tar xzf xorfs.tar.gz
```

**Option B — via git:**
```bash
git clone <your-repo-url>
cd xorfs
# Then copy cluster.conf from Device A OR just re-edit it on each device
```

> **Important:** All devices must have the **same `cluster.conf`** with the correct IPs.

---

## Step 3 — Build binaries on each device

On **every device**, run once:
```bash
make build
```
This compiles `bin/master`, `bin/chunkserver`, `bin/client`, and `bin/webserver`.

> Pre-requisite: Go 1.21+ installed on all devices.

---

## Step 4 — Start each component

### Device A — Primary Master + Web API + Frontend
```bash
make lan-master
# or
./scripts/start_master.sh
```
The script starts:
- `bin/master` on port 50051
- `bin/webserver` on port 8080
- Vite frontend on port 5173

### Device B — Secondary Master (HA Standby)
```bash
make lan-secondary
# or
./scripts/start_secondary.sh
```
The secondary monitors Device A and **auto-promotes** itself if the primary dies.
Chunk servers automatically follow the new master (they re-read `.master_addr`).

### Device C — Chunk Server (Member 1)
```bash
make lan-chunk SLOT=1
# or
./scripts/start_chunkserver.sh 1
```

### Device D — Chunk Server (Member 2)
```bash
make lan-chunk SLOT=2
```

### Device E — Chunk Server (Member 3)
```bash
make lan-chunk SLOT=3
```

---

## Step 5 — Access the Web UI

From **any device on the LAN**, open a browser:
```
http://192.168.1.100:5173
```

Register an account with a **6-digit PIN** and start uploading files!

---

## High Availability (HA) — Keeping Secondary in Sync

The secondary master needs the primary's **checkpoint** and **WAL** files to recover state after a crash.

### Manual sync (run on Device A):
```bash
make lan-sync
# or
./scripts/sync_to_secondary.sh
```

### Automatic sync (cron on Device A):
```bash
# Add to crontab (runs every 5 minutes)
crontab -e

# Add this line:
*/5 * * * * /full/path/to/xorfs/scripts/sync_to_secondary.sh >> /var/log/xorfs_sync.log 2>&1
```

> **SSH key setup required** for passwordless rsync:
> ```bash
> # On Device A, generate a key and copy it to Device B:
> ssh-keygen -t ed25519 -N "" -f ~/.ssh/id_xorfs
> ssh-copy-id -i ~/.ssh/id_xorfs.pub nissan@192.168.1.101
> ```

---

## Firewall / Ports to Open

On each device, ensure these ports are reachable from other LAN devices:

| Device | Ports to open |
|--------|--------------|
| Master (A) | 50051 (gRPC), 8080 (API), 5173 (UI) |
| Secondary (B) | 50052 (gRPC) |
| Chunk 1 (C) | 9001 |
| Chunk 2 (D) | 9002 |
| Chunk 3 (E) | 9003 |

```bash
# Example (Ubuntu/ufw):
sudo ufw allow 50051/tcp
sudo ufw allow 8080/tcp
sudo ufw allow 5173/tcp
sudo ufw allow 9001/tcp   # on chunk server 1
sudo ufw allow 9002/tcp   # on chunk server 2
sudo ufw allow 9003/tcp   # on chunk server 3
```

---

## Quick Reference

| Command | Device | Description |
|---------|--------|-------------|
| `make lan-master` | Device A | Start primary master + web API + frontend |
| `make lan-secondary` | Device B | Start standby master |
| `make lan-chunk SLOT=1` | Device C | Start chunk server 1 |
| `make lan-chunk SLOT=2` | Device D | Start chunk server 2 |
| `make lan-chunk SLOT=3` | Device E | Start chunk server 3 |
| `make lan-sync` | Device A | Push checkpoint+WAL to secondary |
| `make up` | Dev machine | All-in-one local dev mode |
| `make down` | Dev machine | Stop local dev services |

---

## Troubleshooting

**Chunkservers not connected to master?**
- Verify `PRIMARY_MASTER_IP` in `cluster.conf` is correct on the chunkserver device.
- Check `log_files/cs1_stdout.log` for connection errors.
- Ensure port 50051 is open on the master device.

**Secondary not promoting after primary dies?**
- Verify `SECONDARY_MASTER_IP` and `PRIMARY_MASTER_IP` are correct.
- Check `log_files/secondary_stdout.log`.
- The secondary polls the primary every 5 seconds — promotion happens within ~10s.

**Web UI can't connect?**
- The `VITE_API_BASE` env var must point to the master's Web API.
  `start_master.sh` sets this automatically.
- If running `npm run dev` manually: `VITE_API_BASE=http://192.168.1.100:8080 npm run dev`

**"Authentication failed" on login?**
- Passwords are stored in the master's checkpoint. If you wiped `master.checkpoint`, re-register.
