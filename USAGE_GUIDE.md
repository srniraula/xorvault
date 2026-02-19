# XorFS Project Usage Guide

This guide provides a complete walkthrough of the XorFS distributed file system, from initialization to advanced usage, using the simplified `make` commands.

## 🚀 1. Initialization
Before starting, ensure you have Go installed (for local run) or Docker (for containerized run).

### Step 1: Build the Project
Compiles the master, chunkserver, and client binaries.
```bash
make build
```

### Step 2: Setup Client Workspaces
Creates isolated workspaces for 3 simulated clients.
```bash
make setup-clients
```

---

## 🖥️ 2. Running the System

### Option A: Local Mode (Preferred for Development)
Run the entire cluster locally with a single command.

1.  **Start Cluster**:
    ```bash
    make start-cluster
    ```
    *This starts the Master and 3 Chunkservers in the background.*
    
    *To use a custom IP:*
    ```bash
    make start-cluster MASTER_ADDR=192.168.1.77:50051
    ```

2.  **View Logs**:
    ```bash
    make logs-local
    ```

3.  **Stop Cluster**:
    ```bash
    make stop-cluster
    ```

### Option B: Docker Mode
Run the entire cluster in isolated containers.

1.  **Start Cluster**:
    ```bash
    make docker-up
    ```
2.  **View Status**:
    ```bash
    make docker-logs
    ```
3.  **Stop Cluster**:
    ```bash
    make docker-down
    ```

---

## 📂 3. Client Operations
You can run these commands from the project root or inside a client directory.

### File Management
| Action | Command | Example |
| :--- | :--- | :--- |
| **Upload** | `make upload FILE=<path>` | `make upload FILE=my_report.pdf` |
| **Download** | `make download FILE=<name>` | `make download FILE=my_report.pdf` |
| **Delete** | `make delete FILE=<name>` | `make delete FILE=my_report.pdf` |
| **Preview** | `make cat FILE=<name>` | `make cat FILE=readme.txt` |

### Folder Management
| Action | Command | Example |
| :--- | :--- | :--- |
| **Create Folder** | `make mkdir FOLDER=<path>` | `make mkdir FOLDER=documents` |
| **Delete Folder** | `make rmdir FOLDER=<path>` | `make rmdir FOLDER=documents` |
| **List (Detailed)**| `make ls-detailed` | `make ls-detailed` |
| **List Folder** | `make ls-detailed FOLDER=<path>`| `make ls-detailed FOLDER=documents` |

### Organization (Move & Rename)
| Action | Command | Example |
| :--- | :--- | :--- |
| **Rename File** | `make mv SRC=<old> DEST=<new>` | `make mv SRC=old.txt DEST=new.txt` |
| **Move to Folder**| `make mv SRC=<file> DEST=<path>` | `make mv SRC=photo.jpg DEST=photos/vacation.jpg` |

---

## 🧪 4. Testing & Demos

### Run Integration Tests
Runs a full suite of automated tests checking upload, download, folder creation, and deletion.
```bash
make test
```

### Run Feature Demo
Runs a scripted demonstration of folder hierarchies, file organization, and listing.
```bash
make demo
```

---

## 🔌 5. Advanced: Dynamic Server Registration (Local Mode)
Since the master supports dynamic server registration, you can add new servers on the fly.

**To add a 4th Chunkserver:**
1.  Open a new terminal.
2.  Run the command:
    ```bash
    ./bin/chunkserver -port 9004 -storage chunk_server4 -master 127.0.0.1:50051
    ```

## 🛡️ 6. High Availability (Secondary Master)
You can run a **Secondary Master** in standby mode. It will mirror the Primary Master's state (via WAL tailing) and can take over if the Primary fails.

1.  **Start Primary Master**:
    (Already running if you used `make start-cluster`)

2.  **Start Secondary Master**:
    Run on a different port (e.g., 50052) in standby mode, pointing to the Primary.
    ```bash
    make run-secondary SEC_PORT=50052 SEC_MODE=standby
    ```
    *It will monitor the Primary (default 127.0.0.1:50051).*

3.  **Automatic Failover**:
    If the Primary Master stops responding for ~6 seconds:
    1.  The Secondary Master will detect the failure (via periodic Pings).
    2.  It will automatically **promote itself to ACTIVE**.
    3.  It will start accepting write requests on port 50052.

    *Note: You still need to update clients/chunkservers to point to the new address (50052) if they are not configured with failover logic.*

