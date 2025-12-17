.PHONY: all build clean run-master run-chunk_server1 run-chunk_server2 run-chunk_server3 test proto docker-build docker-up docker-down docker-logs docker-clean docker-upload docker-download docker-delete docker-ls

# Get the directory where this Makefile is located (project root)
ROOT_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Build all binaries
all: build

build:
	@echo "Building binaries..."
	@go build -o $(ROOT_DIR)bin/master $(ROOT_DIR)cmd/master
	@go build -o $(ROOT_DIR)bin/chunkserver $(ROOT_DIR)cmd/chunkserver
	@go build -o $(ROOT_DIR)bin/client $(ROOT_DIR)cmd/client
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

# Run master server
run-master: build
	@./bin/master

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
		./bin/client upload files/$(FILE); \
	elif [ -f ../../files/$(FILE) ]; then \
		../../bin/client upload ../../files/$(FILE); \
	else \
		echo "Error: file not found: files/$(FILE)"; exit 1; \
	fi

# Download a file (usage: cd clients/client1 && make download FILE=myfile.pdf)
download:
	./bin/client download $(FILE)

# Delete a file (usage: cd clients/client1 && make delete FILE=myfile.pdf)
delete:
	./bin/client delete $(FILE)

# List all files uploaded by this client (usage: cd clients/client1 && make ls)
ls:
	./bin/client ls

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