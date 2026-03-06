.PHONY: all build clean run-master run-secondary run-chunk_server1 run-chunk_server2 run-chunk_server3 test proto \
        upload download delete ls ls-detailed mkdir rmdir mv cat set-master \
        docker-build docker-up docker-down docker-logs docker-clean docker-upload docker-download docker-delete docker-ls

# Get the directory where this Makefile is located (project root)
ROOT_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Default master address (use $(MASTER) if supplied, else fallback)
MASTER_ADDR ?= $(if $(MASTER),$(MASTER),127.0.0.1:50051)

# Build all binaries
all: build

build:
	@echo "Building binaries..."
	@go build -o $(ROOT_DIR)bin/master    $(ROOT_DIR)cmd/master
	@go build -o $(ROOT_DIR)bin/chunkserver $(ROOT_DIR)cmd/chunkserver
	@go build -o $(ROOT_DIR)bin/client    $(ROOT_DIR)cmd/client
	@go build -o $(ROOT_DIR)bin/webserver $(ROOT_DIR)cmd/webserver
	@echo "Build complete: binaries in bin/"

# Clean build artifacts and data
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -rf chunk_server1/ chunk_server2/ chunk_server3/
	@rm -rf log_files/*.log
	@rm -f downloaded_*
	@rm -f *.checkpoint *.wal *.wal.old
	@rm -rf clients/
	@echo "Clean complete"

# Run master server (local)
run-master: build
	@./bin/master

# ========== LAN / Distributed Deployment ==========
# ─────────────────────────────────────────────────────────────────────
# Usage: pass the master IP and (optionally) secondary IP on the command
# line. No need to edit cluster.conf before running.
#
# MASTER   = IP:port of the PRIMARY master  (default: 127.0.0.1:50051)
# SECONDARY= IP:port of the SECONDARY master (optional)
# SLOT     = chunk server slot number: 1, 2, or 3
# MY_IP    = this device's LAN IP (auto-detected if not set)
# WEB_PORT = web API port (default: 8080)
# UI_PORT  = frontend port (default: 5173)
#
# Examples — run each command on the relevant device:
#
#   Device A (Primary Master):
#     make run-master-lan MASTER=192.168.1.100:50051
#
#   Device B (Secondary Master):
#     make run-secondary-lan MASTER=192.168.1.100:50051 SECONDARY=192.168.1.101:50052
#
#   Device C (Chunk Server 1):
#     make run-chunk-lan SLOT=1 MASTER=192.168.1.100:50051 MY_IP=192.168.1.102
#
#   Device D (Chunk Server 2):
#     make run-chunk-lan SLOT=2 MASTER=192.168.1.100:50051 MY_IP=192.168.1.103
#
#   Device E (Chunk Server 3):
#     make run-chunk-lan SLOT=3 MASTER=192.168.1.100:50051 MY_IP=192.168.1.104
#
#   Any device (Web API, separate from master):
#     make run-web-lan MASTER=192.168.1.100:50051
#
#   Any device (Frontend only):
#     make run-ui-lan MASTER_WEB=192.168.1.100:8080
# ─────────────────────────────────────────────────────────────────────

# Defaults (local dev if user doesn't supply values)
MASTER    ?= 127.0.0.1:50051
SECONDARY ?=
MY_IP     ?=
WEB_PORT  ?= 8080
UI_PORT   ?= 5173

# ── Primary Master ────────────────────────────────────────────────────
# Starts master gRPC + Web API + Frontend all-in-one on this device.
# Run on Device A.
run-master-lan: build
	@if [ -z "$(MASTER)" ]; then echo "Usage: make run-master-lan MASTER=<this-device-ip>:50051"; exit 1; fi
	@echo ""
	@echo "════════════════════════════════════════"
	@echo "  Starting PRIMARY MASTER"
	@echo "  gRPC  : $(MASTER)"
	@echo "  WebAPI: http://$(word 1,$(subst :, ,$(MASTER))):$(WEB_PORT)"
	@echo "  UI    : http://$(word 1,$(subst :, ,$(MASTER))):$(UI_PORT)"
	@echo "════════════════════════════════════════"
	@echo ""
	@mkdir -p log_files
	@# Write .master_addr so chunkservers on localhost can also find it
	@echo "$(MASTER)" > .master_addr
	@# Start master gRPC
	MASTER_ADDR="$(MASTER)" \
	  ./bin/master -port "$(word 2,$(subst :, ,$(MASTER)))" -mode active \
	  > log_files/master_stdout.log 2>&1 & echo $$! >> .sys_pids
	@sleep 2
	@# Start Web API
	MASTER_ADDR="$(MASTER)" SECONDARY_MASTER_ADDR="$(SECONDARY)" WEB_API_PORT="$(WEB_PORT)" \
	  ./bin/webserver > log_files/webserver_stdout.log 2>&1 & echo $$! >> .sys_pids
	@sleep 1
	@# Start frontend
	@if [ ! -d "web/node_modules" ]; then (cd web && npm install > /dev/null 2>&1); fi
	cd web && VITE_API_BASE="http://$(word 1,$(subst :, ,$(MASTER))):$(WEB_PORT)" \
	  npm run dev -- --port $(UI_PORT) --host 0.0.0.0 \
	  > ../log_files/frontend_stdout.log 2>&1 & echo $$! >> ../.sys_pids
	@echo "All services started. Logs in log_files/. Stop with: make down"

# ── Secondary Master ──────────────────────────────────────────────────
# Standby that monitors the primary and auto-promotes on failure.
# Run on Device B.
# Usage: make run-secondary-lan MASTER=192.168.1.100:50051 SECONDARY=192.168.1.101:50052
run-secondary-lan: build
	@if [ -z "$(SECONDARY)" ]; then \
		echo "Usage: make run-secondary-lan MASTER=<primary-ip>:50051 SECONDARY=<this-device-ip>:50052"; exit 1; \
	fi
	@echo ""
	@echo "════════════════════════════════════════"
	@echo "  Starting SECONDARY (STANDBY) MASTER"
	@echo "  Listening : $(SECONDARY)"
	@echo "  Monitoring: $(MASTER)"
	@echo "════════════════════════════════════════"
	@echo ""
	@mkdir -p log_files
	@echo "$(SECONDARY)" > .secondary_addr
	MASTER_ADDR="$(MASTER)" \
	  SECONDARY_MASTER_ADDR="$(SECONDARY)" \
	  ./bin/master \
	    -port "$(word 2,$(subst :, ,$(SECONDARY)))" \
	    -mode standby \
	    -primary "$(MASTER)" \
	  > log_files/secondary_stdout.log 2>&1

# ── Chunk Server (explicit IP) ────────────────────────────────────────
# Run on the chunk server device.
# Usage: make run-chunk-lan SLOT=1 MASTER=192.168.1.100:50051 MY_IP=192.168.1.102
run-chunk-lan: build
	@if [ -z "$(SLOT)" ]; then \
		echo "Usage: make run-chunk-lan SLOT=<1|2|3> MASTER=<primary-ip>:50051 [MY_IP=<this-device-ip>]"; exit 1; \
	fi
	@echo ""
	@echo "════════════════════════════════════════"
	@echo "  Starting CHUNK SERVER slot $(SLOT)"
	@echo "  My IP  : $(MY_IP) (auto if blank)"
	@echo "  Port   : 900$(SLOT)"
	@echo "  Master : $(MASTER)"
	@echo "════════════════════════════════════════"
	@echo ""
	@mkdir -p log_files chunk_server$(SLOT)
	MASTER_ADDR="$(MASTER)" $(if $(MY_IP),CHUNKSERVER_ADDR="$(MY_IP):900$(SLOT)") \
	  ./bin/chunkserver \
	    -port "900$(SLOT)" \
	    -storage "chunk_server$(SLOT)" \
	    -master "$(MASTER)" \
	    -secondary "$(SECONDARY)" \
	  > log_files/cs$(SLOT)_stdout.log 2>&1

# ── Web API only (on a separate device) ──────────────────────────────
# Use this when you want the Web API running on a device that is NOT
# the master device (e.g. a dedicated load balancer or a member's PC).
# Usage: make run-web-lan MASTER=192.168.1.100:50051 [WEB_PORT=8080]
run-web-lan: build
	@echo ""
	@echo "════════════════════════════════════════"
	@echo "  Starting WEB API"
	@echo "  Master : $(MASTER)"
	@echo "  Port   : $(WEB_PORT)"
	@echo "════════════════════════════════════════"
	@echo ""
	@mkdir -p log_files
	MASTER_ADDR="$(MASTER)" WEB_API_PORT="$(WEB_PORT)" \
	  ./bin/webserver

# ── Frontend only (on any LAN device) ────────────────────────────────
# Lets any device on the LAN run the UI pointed at the web API.
# MASTER_WEB = IP:port of the web API (default: use MASTER device IP port 8080)
# Usage: make run-ui-lan MASTER_WEB=192.168.1.100:8080 [UI_PORT=5173]
MASTER_WEB ?= $(word 1,$(subst :, ,$(MASTER))):$(WEB_PORT)
run-ui-lan:
	@echo ""
	@echo "════════════════════════════════════════"
	@echo "  Starting FRONTEND"
	@echo "  API Base: http://$(MASTER_WEB)"
	@echo "  Port    : $(UI_PORT)"
	@echo "════════════════════════════════════════"
	@echo ""
	@if [ ! -d "web/node_modules" ]; then (cd web && npm install > /dev/null 2>&1); fi
	@cd web && VITE_API_BASE="http://$(MASTER_WEB)" \
	  npm run dev -- --port $(UI_PORT) --host 0.0.0.0

# Sync checkpoint+WAL to secondary (run on primary master device)
lan-sync:
	@chmod +x scripts/sync_to_secondary.sh
	@./scripts/sync_to_secondary.sh

# Older script-based targets (still work — reads cluster.conf)
lan-master: build
	@chmod +x scripts/start_master.sh
	@./scripts/start_master.sh

lan-secondary: build
	@chmod +x scripts/start_secondary.sh
	@./scripts/start_secondary.sh

lan-chunk: build
	@if [ -z "$(SLOT)" ]; then \
		echo "Error: SLOT not specified. Usage: make lan-chunk SLOT=1"; exit 1; \
	fi
	@chmod +x scripts/start_chunkserver.sh
	@./scripts/start_chunkserver.sh $(SLOT)

# ========== End LAN Commands ==========

# Run chunk server 1
run-chunk_server1: build
	@./bin/chunkserver -port 9001 -storage chunk_server1 -master $(MASTER_ADDR)

# Run chunk server 2
run-chunk_server2: build
	@./bin/chunkserver -port 9002 -storage chunk_server2 -master $(MASTER_ADDR)

# Run chunk server 3
run-chunk_server3: build
	@./bin/chunkserver -port 9003 -storage chunk_server3 -master $(MASTER_ADDR)

# Upload a file
# Usage:
#  - From project root: make upload FILE=myfile.pdf
#  - From client workspace (clients/client1): cd clients/client1 && make upload FILE=myfile.pdf
upload:
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make upload FILE=myfile.pdf"; exit 1; \
	fi; \
	# Prefer running from project root (./files), otherwise try client workspace relative path
	@if [ -f files/$(FILE) ]; then \
		MASTER_ADDR="$(MASTER_ADDR)" ./bin/client upload files/$(FILE); \
	elif [ -f ../../files/$(FILE) ]; then \
		MASTER_ADDR="$(MASTER_ADDR)" ../../bin/client upload ../../files/$(FILE); \
	else \
		echo "Error: file not found: files/$(FILE)"; exit 1; \
	fi

# Download a file (usage: make download FILE=myfile.pdf)
download:
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make download FILE=myfile.pdf"; exit 1; \
	fi
	@MASTER_ADDR="$(MASTER_ADDR)" ./bin/client download "$(FILE)"

# Delete a file (usage: make delete FILE=myfile.pdf)
delete:
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make delete FILE=myfile.pdf"; exit 1; \
	fi
	@MASTER_ADDR="$(MASTER_ADDR)" ./bin/client delete "$(FILE)"

# List all files uploaded by this client (usage: cd clients/client1 && make ls)
ls:
	@MASTER_ADDR="$(MASTER_ADDR)" ./bin/client ls

# Register a new client ID
register:
	@MASTER_ADDR="$(MASTER_ADDR)" ./bin/client register

# List files with details (usage: make ls-detailed [FOLDER=path])
ls-detailed:
	@if [ -z "$(FOLDER)" ]; then \
		MASTER_ADDR="$(MASTER_ADDR)" ./bin/client ls-detailed; \
	else \
		MASTER_ADDR="$(MASTER_ADDR)" ./bin/client ls-detailed $(FOLDER); \
	fi

# Create a folder (usage: make mkdir FOLDER=documents/photos)
mkdir:
	@if [ -z "$(FOLDER)" ]; then \
		echo "Error: FOLDER not specified. Usage: make mkdir FOLDER=path"; exit 1; \
	else \
		MASTER_ADDR="$(MASTER_ADDR)" ./bin/client mkdir "$(FOLDER)"; \
	fi

# Remove an empty folder (usage: make rmdir FOLDER=documents/photos)
rmdir:
	@if [ -z "$(FOLDER)" ]; then \
		echo "Error: FOLDER not specified. Usage: make rmdir FOLDER=path"; exit 1; \
	else \
		MASTER_ADDR="$(MASTER_ADDR)" ./bin/client rmdir "$(FOLDER)"; \
	fi

# Move/rename a file (usage: make mv SRC=file.pdf DEST=folder/file.pdf)
mv:
	@if [ -z "$(SRC)" ] || [ -z "$(DEST)" ]; then \
		echo "Error: SRC and DEST required. Usage: make mv SRC=source DEST=destination"; exit 1; \
	else \
		MASTER_ADDR="$(MASTER_ADDR)" ./bin/client mv "$(SRC)" "$(DEST)"; \
	fi

# Preview file content (usage: make cat FILE=readme.txt)
cat:
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make cat FILE=filename"; exit 1; \
	else \
		MASTER_ADDR="$(MASTER_ADDR)" ./bin/client cat "$(FILE)"; \
	fi

# Set the master address for this client workspace
# Usage (from client workspace): make set-master MASTER_ADDR=192.168.1.10:50051
.PHONY: set-master
set-master:
	@if [ -z "$(MASTER_ADDR)" ]; then \
		echo "Error: MASTER_ADDR not specified. Usage: make set-master MASTER_ADDR=host:port"; exit 1; \
	fi
	@echo "$(MASTER_ADDR)" > .master_addr
	@echo "Wrote .master_addr with $(MASTER_ADDR)"

.PHONY: set_master
set_master: set-master

# Generate protobuf code (if you modify dfs.proto)
proto:
	@echo "Generating protobuf code..."
	@protoc --go_out=dfspb --go_opt=paths=source_relative \
		--go-grpc_out=dfspb --go-grpc_opt=paths=source_relative \
		dfs.proto
	@echo "Protobuf generation complete"

# ========== Docker Commands ==========

# Build Docker images
docker-build:
	@echo "Building Docker images..."
	@docker-compose build
	@echo "Docker images built successfully"

# Start all containers (master + 3 chunk servers)
docker-up:
	@echo "Starting DFS cluster in Docker..."
	@mkdir -p master-data
# Create the required subdirectories inside the volume mount point
	@mkdir -p master-data/log_files
	@mkdir -p master-data/files
	@mkdir -p chunkserver2-data/log_files chunkserver2-data/files
	@mkdir -p chunkserver3-data/log_files chunkserver3-data/files
	@mkdir -p chunkserver1-data/log_files chunkserver1-data/files

	@docker-compose up -d
	@echo ""
	@echo "✓ DFS cluster started!"
	@echo ""
	@echo "Services:"
	@echo "  Master:         localhost:50051"
	@echo "  Chunkserver 1:  localhost:9001"
	@echo "  Chunkserver 2:  localhost:9002"
	@echo "  Chunkserver 3:  localhost:9003"
	@echo ""
	@echo "Data directories:"
	@echo "  Master logs:    ./master-data/log_files/master.log"
	@echo "  Master WAL:     ./master-data/master.wal"
	@echo "  Master checkpoint: ./master-data/master.checkpoint"
	@echo "  Chunkserver 1:  ./chunkserver1-data/log_files/chunkserver.log"
	@echo "  Chunkserver 2:  ./chunkserver2-data/log_files/chunkserver.log"
	@echo "  Chunkserver 3:  ./chunkserver3-data/log_files/chunkserver.log"
	@echo ""
	@echo "Quick commands:"
	@echo "  make docker-logs              - View all logs"
	@echo "  make docker-logs-master       - View master logs"
	@echo "  make docker-upload FILE=<file> - Upload file"
	@echo "  make docker-download FILE=<file> - Download file"
	@echo "  make docker-ls                - List files"
	@echo "  make docker-down              - Stop cluster"
	@echo ""

# Stop all containers
docker-down:
	@echo "Stopping DFS cluster..."
	@docker-compose down
	@echo "DFS cluster stopped"

# View logs from all containers
docker-logs: 
	@docker-compose logs -f

# View logs from specific container
docker-logs-master:
	@docker-compose logs -f master

docker-logs-chunkserver1:
	@docker-compose logs -f chunkserver1

docker-logs-chunkserver2:
	@docker-compose logs -f chunkserver2

docker-logs-chunkserver3:
	@docker-compose logs -f chunkserver3

# View master.log file directly
docker-view-master-log:
	@echo "=== Master Log ==="
	@cat master-data/log_files/master.log 2>/dev/null || echo "Log file not created yet"

# View master.wal file
docker-view-wal:
	@echo "=== Master WAL ==="
	@cat master-data/master.wal 2>/dev/null || echo "WAL file not created yet"

# View master.checkpoint file
docker-view-checkpoint:
	@echo "=== Master Checkpoint ==="
	@cat master-data/master.checkpoint 2>/dev/null || echo "Checkpoint file not created yet"

# View chunkserver log
docker-view-chunkserver-log:
	@echo "=== Chunkserver $(SERVER) Log ==="
	@cat chunkserver$(SERVER)-data/log_files/chunkserver.log 2>/dev/null || echo "Log file not created yet"

# Clean all Docker resources (containers, volumes, networks)
docker-clean:
	@echo "Cleaning Docker resources..."
	@docker-compose down -v
	@docker system prune -f
	@rm -rf clients/
	@rm -rf master-data/ chunkserver1-data/ chunkserver2-data/ chunkserver3-data/
	@echo "Docker cleanup complete"

# Upload file via Docker client
docker-upload:
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make docker-upload FILE=myfile.pdf"; \
		exit 1; \
	fi
	@echo "Uploading $(FILE)..."
	@docker-compose run --rm --entrypoint /usr/local/bin/client client upload /workspace/files/$(FILE)

# Download file via Docker client
docker-download:
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make docker-download FILE=myfile.pdf"; \
		exit 1; \
	fi
	@echo "Downloading $(FILE)..."
	@docker-compose run --rm --entrypoint /usr/local/bin/client client download $(FILE)

# List files via Docker client
docker-ls:
	@echo "Listing files..."
	@docker-compose run --rm --entrypoint /usr/local/bin/client client ls

# Delete file via Docker client
docker-delete:
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make docker-delete FILE=myfile.pdf"; \
		exit 1; \
	fi
	@echo "Deleting $(FILE)..."
	@docker-compose run --rm --entrypoint /usr/local/bin/client client delete $(FILE)

# Multi-client Docker commands (CLIENT=1|2|3, FILE=filename)
docker-client-upload:
	@if [ -z "$(CLIENT)" ] || [ -z "$(FILE)" ]; then \
		echo "Error: CLIENT and FILE required. Usage: make docker-client-upload CLIENT=1 FILE=test.pdf"; \
		exit 1; \
	fi
	@echo "Client $(CLIENT): Uploading $(FILE)..."
	@mkdir -p clients/client$(CLIENT)
	@docker-compose run --rm --entrypoint /usr/local/bin/client -v $(PWD):/workspace -w /workspace/clients/client$(CLIENT) client upload /workspace/files/$(FILE)

docker-client-download:
	@if [ -z "$(CLIENT)" ] || [ -z "$(FILE)" ]; then \
		echo "Error: CLIENT and FILE required. Usage: make docker-client-download CLIENT=1 FILE=test.pdf"; \
		exit 1; \
	fi
	@echo "Client $(CLIENT): Downloading $(FILE)..."
	@mkdir -p clients/client$(CLIENT)
	@docker-compose run --rm --entrypoint /usr/local/bin/client -v $(PWD):/workspace -w /workspace/clients/client$(CLIENT) client download $(FILE)

docker-client-ls:
	@if [ -z "$(CLIENT)" ]; then \
		echo "Error: CLIENT required. Usage: make docker-client-ls CLIENT=1"; \
		exit 1; \
	fi
	@echo "Client $(CLIENT): Listing files..."
	@mkdir -p clients/client$(CLIENT)
	@docker-compose run --rm --entrypoint /usr/local/bin/client -v $(PWD):/workspace -w /workspace/clients/client$(CLIENT) client ls

docker-client-delete:
	@if [ -z "$(CLIENT)" ] || [ -z "$(FILE)" ]; then \
		echo "Error: CLIENT and FILE required. Usage: make docker-client-delete CLIENT=1 FILE=test.pdf"; \
		exit 1; \
	fi
	@echo "Client $(CLIENT): Deleting $(FILE)..."
	@mkdir -p clients/client$(CLIENT)
	@docker-compose run --rm --entrypoint /usr/local/bin/client -v $(PWD):/workspace -w /workspace/clients/client$(CLIENT) client delete $(FILE)

# Restart a specific chunk server (simulates crash)
docker-restart-chunkserver1:
	@echo "Restarting chunk server 1..."
	@docker-compose restart chunkserver1

docker-restart-chunkserver2:
	@echo "Restarting chunk server 2..."
	@docker-compose restart chunkserver2

docker-restart-chunkserver3:
	@echo "Restarting chunk server 3..."
	@docker-compose restart chunkserver3

# Stop a specific chunk server (simulates failure)
docker-stop-chunkserver1:
	@echo "Stopping chunk server 1 (simulating failure)..."
	@docker-compose stop chunkserver1
	@echo "Chunkserver 1 stopped"

docker-stop-chunkserver2:
	@echo "Stopping chunk server 2 (simulating failure)..."
	@docker-compose stop chunkserver2
	@echo "Chunkserver 2 stopped"

docker-stop-chunkserver3:
	@echo "Stopping chunk server 3 (simulating failure)..."
	@docker-compose stop chunkserver3
	@echo "Chunkserver 3 stopped"

# Start a stopped chunk server
docker-start-chunkserver1:
	@echo "Starting chunk server 1..."
	@docker-compose start chunkserver1
	@echo "Chunkserver 1 started"

docker-start-chunkserver2:
	@echo "Starting chunk server 2..."
	@docker-compose start chunkserver2
	@echo "Chunkserver 2 started"

docker-start-chunkserver3:
	@echo "Starting chunk server 3..."
	@docker-compose start chunkserver3
	@echo "Chunkserver 3 started"

# ========== End Docker Commands ==========

# Help
help:
	@echo "DFS Project Makefile"
	@echo ""
	@echo "=== Local Development ==="
	@echo "  make build         - Build all binaries"
	@echo "  make clean         - Remove binaries and data"
	@echo "  make run-master    - Run master server"
	@echo "  make run-chunk_server1 - Run chunk server 1 (port 9001)"
	@echo "  make run-chunk_server2 - Run chunk server 2 (port 9002)"
	@echo "  make run-chunk_server3 - Run chunk server 3 (port 9003)"
	@echo ""
	@echo "=== Docker Commands ==="
	@echo "  make docker-build  - Build Docker images"
	@echo "  make docker-up     - Start DFS cluster"
	@echo "  make docker-down   - Stop DFS cluster"
	@echo "  make docker-clean  - Remove all Docker resources"
	@echo ""
	@echo "=== File Operations (Docker) ==="
	@echo "  make docker-upload FILE=<file>   - Upload file"
	@echo "  make docker-download FILE=<file> - Download file"
	@echo "  make docker-delete FILE=<file>   - Delete file"
	@echo "  make docker-ls                   - List files"
	@echo ""
	@echo "=== Multi-Client Operations ==="
	@echo "  make docker-client-upload CLIENT=1 FILE=test.pdf"
	@echo "  make docker-client-download CLIENT=1 FILE=test.pdf"
	@echo "  make docker-client-ls CLIENT=1"
	@echo "  make docker-client-delete CLIENT=1 FILE=test.pdf"
	@echo ""
	@echo "=== View Logs & Files ==="
	@echo "  make docker-logs               - View all container logs"
	@echo "  make docker-logs-master        - View master logs"
	@echo "  make docker-view-master-log    - View master.log file"
	@echo "  make docker-view-wal           - View master.wal file"
	@echo "  make docker-view-checkpoint    - View master.checkpoint file"
	@echo "  make docker-view-chunkserver-log SERVER=1 - View chunkserver log"
	@echo ""
	@echo "=== Failure Simulation ==="
	@echo "  make docker-stop-chunkserver1  - Stop chunkserver 1"
	@echo "  make docker-start-chunkserver1 - Start chunkserver 1"
	@echo "  make docker-restart-chunkserver1 - Restart chunkserver 1"
	@echo ""
	@echo "=== Example Workflow (Docker) ==="
	@echo "  make docker-build"
	@echo "  make docker-up"
	@echo "  make docker-upload FILE=mypic.jpg"
	@echo "  make docker-ls"
	@echo "  make docker-view-master-log"
	@echo "  make docker-view-wal"
	@echo "  make docker-download FILE=mypic.jpg"
	@echo "  make docker-down"
	@echo ""
# ========== Test Suite ==========

# Run integration tests for file system operations
# Replaces test_filesystem.sh
test: build
	@echo "=========================================="
	@echo "XorFS File System Operations Test Suite"
	@echo "=========================================="
	@echo ""
	
	@echo "Creating test files..."
	@echo "This is a test file for XorFS operations" > test_file.txt
	@echo "This is another test file" > test_file2.txt
	@mkdir -p files
	@cp test_file.txt files/
	@cp test_file2.txt files/

	@echo ""
	@echo "=================="
	@echo "Test 1: Folder Creation (mkdir)"
	@echo "=================="
	@$(MAKE) mkdir FOLDER=test_docs >/dev/null
	@$(MAKE) mkdir FOLDER=test_docs/reports/2024 >/dev/null
	@$(MAKE) mkdir FOLDER=test_photos >/dev/null
	@echo "✓ PASS: Folder creation"

	@echo ""
	@echo "=================="
	@echo "Test 2: View Folder Structure (ls-detailed)"
	@echo "=================="
	@$(MAKE) ls-detailed >/dev/null
	@$(MAKE) ls-detailed FOLDER=test_docs >/dev/null
	@echo "✓ PASS: View folder structure"

	@echo ""
	@echo "=================="
	@echo "Test 3: File Upload"
	@echo "=================="
	@$(MAKE) upload FILE=test_file.txt >/dev/null
	@$(MAKE) upload FILE=test_file2.txt >/dev/null
	@echo "✓ PASS: File upload"

	@echo ""
	@echo "=================="
	@echo "Test 4: Move/Rename Files (mv)"
	@echo "=================="
	@$(MAKE) mv SRC=test_file.txt DEST=test_docs/test_file.txt >/dev/null
	@$(MAKE) mv SRC=test_file2.txt DEST=renamed_file.txt >/dev/null
	@$(MAKE) mv SRC=renamed_file.txt DEST=test_docs/reports/2024/report.txt >/dev/null
	@echo "✓ PASS: Move/Rename files"

	@echo ""
	@echo "=================="
	@echo "Test 5: Verify File Organization"
	@echo "=================="
	@$(MAKE) ls-detailed FOLDER=test_docs >/dev/null
	@$(MAKE) ls-detailed FOLDER=test_docs/reports/2024 >/dev/null
	@echo "✓ PASS: Verify organization"

	@echo ""
	@echo "=================="
	@echo "Test 6: Preview File Content (cat)"
	@echo "=================="
	@$(MAKE) cat FILE=test_docs/test_file.txt >/dev/null
	@echo "✓ PASS: Preview file"

	@echo ""
	@echo "=================="
	@echo "Test 7: Error Handling"
	@echo "=================="
	@# Expect failure for duplicate folder
	@if $(MAKE) mkdir FOLDER=test_docs >/dev/null 2>&1; then \
		echo "✗ FAIL: Should have rejected duplicate folder creation"; exit 1; \
	else \
		echo "✓ PASS: Rejected duplicate folder creation"; \
	fi
	@# Expect failure for non-empty delete
	@if $(MAKE) rmdir FOLDER=test_docs >/dev/null 2>&1; then \
		echo "✗ FAIL: Should have rejected deletion of non-empty folder"; exit 1; \
	else \
		echo "✓ PASS: Rejected deletion of non-empty folder"; \
	fi

	@echo ""
	@echo "=================="
	@echo "Test 8: Delete Empty Folder (rmdir)"
	@echo "=================="
	@$(MAKE) rmdir FOLDER=test_photos >/dev/null
	@echo "✓ PASS: Delete empty folder"

	@echo ""
	@echo "=================="
	@echo "Test 9: Cleanup Operations"
	@echo "=================="
	@$(MAKE) delete FILE=test_docs/test_file.txt >/dev/null
	@$(MAKE) delete FILE=test_docs/reports/2024/report.txt >/dev/null
	@$(MAKE) rmdir FOLDER=test_docs/reports/2024 >/dev/null
	@$(MAKE) rmdir FOLDER=test_docs/reports >/dev/null
	@$(MAKE) rmdir FOLDER=test_docs >/dev/null
	@echo "✓ PASS: Cleanup operations"

	@echo ""
	@echo "=================="
	@echo "Test 10: Final Verification"
	@echo "=================="
	@$(MAKE) ls-detailed >/dev/null
	@echo "✓ PASS: Final listing"

	@echo ""
	@echo "=========================================="
	@echo "Test Summary"
	@echo "=========================================="
	@echo "All tests passed!"
	@echo "=========================================="
	@rm -f test_file.txt test_file2.txt

# ========== Setup & Demo ==========

# Setup client workspaces (replaces setup_clients.sh)
setup-clients:
	@echo "Setting up client workspaces..."
	@mkdir -p clients
	@for i in 1 2 3; do \
		CLIENT_DIR="clients/client$$i"; \
		mkdir -p "$$CLIENT_DIR"; \
		ln -sf ../../Makefile "$$CLIENT_DIR/Makefile"; \
		touch "$$CLIENT_DIR/.username"; \
		echo "# Client $$i Workspace" > "$$CLIENT_DIR/README.md"; \
		echo "This is an isolated workspace for Client $$i." >> "$$CLIENT_DIR/README.md"; \
		echo "Run 'make upload FILE=...' from here." >> "$$CLIENT_DIR/README.md"; \
		echo "Created $$CLIENT_DIR"; \
	done
	@echo "Setup complete! Created 3 client workspaces in clients/"

# Run demo sequence (replaces demo_filesystem.sh)
demo:
	@echo "=================================================="
	@echo "XorFS File System Operations Demo"
	@echo "=================================================="
	@echo ""
	@echo ">>> Demo 1: Create Folder Structure"
	@$(MAKE) mkdir FOLDER=documents >/dev/null
	@$(MAKE) mkdir FOLDER=documents/reports >/dev/null
	@$(MAKE) mkdir FOLDER=documents/reports/2024 >/dev/null
	@$(MAKE) mkdir FOLDER=photos >/dev/null
	@$(MAKE) mkdir FOLDER=photos/vacation >/dev/null
	@echo "Folders created."
	@echo ""
	
	@echo ">>> Demo 2: View Folder Structure"
	@$(MAKE) ls-detailed
	
	@echo ">>> Demo 3: Upload Files"
	@echo "For this demo, we assume files exist. Skipping actual upload if files missing."
	@# Simplified for demo target - just echo or try upload if files exist
	@echo "(Skipping upload in make-demo to avoid errors if files missing)"
	
	@echo ">>> Demo 5: View Organized Structure"
	@$(MAKE) ls-detailed FOLDER=documents
	@$(MAKE) ls-detailed FOLDER=photos
	
	@echo ">>> Demo 10: Final Structure"
	@$(MAKE) ls-detailed
	@echo "Demo Complete!"

# ========== Full System Management ==========

pid_file := .sys_pids

# Start everything: Master, Chunkservers, Web API, and Frontend
up: build
	@echo "Starting full XORFS system..."
	@mkdir -p log_files
	@# 1. Start Master
	@./bin/master > log_files/master_stdout.log 2>&1 & echo $$! >> $(pid_file)
	@echo "Master started."
	@sleep 2
	@# 2. Start Chunkservers (auto-detect master)
	@./bin/chunkserver -port 9001 -storage chunk_server1 > log_files/cs1_stdout.log 2>&1 & echo $$! >> $(pid_file)
	@./bin/chunkserver -port 9002 -storage chunk_server2 > log_files/cs2_stdout.log 2>&1 & echo $$! >> $(pid_file)
	@./bin/chunkserver -port 9003 -storage chunk_server3 > log_files/cs3_stdout.log 2>&1 & echo $$! >> $(pid_file)
	@echo "3 Chunkservers started."
	@# 3. Start Web API
	@./bin/webserver > log_files/webserver_stdout.log 2>&1 & echo $$! >> $(pid_file)
	@echo "Web API started (port 8080)."
	@# 4. Start Frontend
	@if [ ! -d "web/node_modules" ]; then (cd web && npm install); fi
	@cd web && npm run dev -- --port 5173 --host 0.0.0.0 > ../log_files/frontend_stdout.log 2>&1 & echo $$! >> ../$(pid_file)
	@echo "Frontend started (port 5173)."
	@echo ""
	@echo "System is UP! Access UI at http://localhost:5173"
	@echo "Run 'make down' to stop everything."

# Stop everything started via 'make up'
down:
	@if [ -f $(pid_file) ]; then \
		echo "Stopping XORFS system..."; \
		while read pid; do \
			if kill -0 $$pid 2>/dev/null; then \
				kill -9 $$pid; \
				echo "Stopped PID $$pid"; \
			fi; \
		done < $(pid_file); \
		rm $(pid_file); \
		echo "All processes stopped."; \
	else \
		echo "No system PID file found. Cleaning up by process name..."; \
		pkill -f "bin/master" || true; \
		pkill -f "bin/chunkserver" || true; \
		pkill -f "webserver/main.go" || true; \
		pkill -f "vite" || true; \
		echo "Cleanup complete."; \
	fi

start-cluster: up
stop-cluster: down

# View local logs
logs-local:
	@tail -f log_files/*.log

# Run Secondary Master (High Availability)
# Usage: make run-secondary [SEC_PORT=50052] [PRIMARY_ADDR=127.0.0.1:50051]
SEC_PORT ?= 50052
PRIMARY_ADDR ?= 127.0.0.1:50051

run-secondary: build
	@echo "Starting Secondary Master on port $(SEC_PORT) monitoring primary $(PRIMARY_ADDR)..."
	@./bin/master -port $(SEC_PORT) -mode standby -primary $(PRIMARY_ADDR)
