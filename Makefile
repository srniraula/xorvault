.PHONY: all build clean run-master run-secondary run-chunk_server1 run-chunk_server2 run-chunk_server3 run-webserver run-master-primary run-master-secondary help test proto \
        upload download delete ls ls-detailed mkdir rmdir mv cat set-master
 
# Get the directory where this Makefile is located (project root)
ROOT_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
 
# Default master address for local development (can be overridden)
MASTER_ADDR ?= 127.0.0.1:50051
 
# Build all binaries
all: build
 
build:
	@echo "Building binaries..."
	@go build -o $(ROOT_DIR)bin/master $(ROOT_DIR)cmd/master
	@go build -o $(ROOT_DIR)bin/chunkserver $(ROOT_DIR)cmd/chunkserver
	@go build -o $(ROOT_DIR)bin/client $(ROOT_DIR)cmd/client
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
	@rm -rf client_logs/
	@rm -rf webserver_logs/
	@echo "Clean complete"
 
# Run master with defaults (no failover) — backward compatible
run-master: build
	@./bin/master -addr 0.0.0.0:50051
 
# Run chunk server 1
# Usage: make run-chunk_server1 MASTER_ADDR=<primary:port> [SECONDARY_MASTER_ADDR=<secondary:port>] [CHUNK_HOST=<ip>]
# CHUNK_HOST: the IP this chunkserver advertises to the master and clients.
# Set this when the machine has multiple network interfaces (e.g. both WiFi and
# a host-only interface) so the correct reachable IP is registered.
# Example (host-only network): make run-chunk_server1 MASTER_ADDR=192.168.128.1:50051 SECONDARY_MASTER_ADDR=192.168.128.2:50052 CHUNK_HOST=192.168.128.1:9001
run-chunk_server1: build
	@if [ -z "$(SECONDARY_MASTER_ADDR)" ]; then \
		echo "WARNING: SECONDARY_MASTER_ADDR not set — chunkserver1 will NOT fail over if primary master dies!"; \
	fi
	@./bin/chunkserver -port 9001 -storage chunk_server1 -master $(MASTER_ADDR) -secondary-master $(SECONDARY_MASTER_ADDR) $(if $(CHUNK_HOST),-addr $(CHUNK_HOST):9001)
 
# Run chunk server 2
# Usage: make run-chunk_server2 MASTER_ADDR=<primary:port> SECONDARY_MASTER_ADDR=<secondary:port>
run-chunk_server2: build
	@if [ -z "$(SECONDARY_MASTER_ADDR)" ]; then \
		echo "WARNING: SECONDARY_MASTER_ADDR not set — chunkserver2 will NOT fail over if primary master dies!"; \
	fi
	@./bin/chunkserver -port 9002 -storage chunk_server2 -master $(MASTER_ADDR) -secondary-master $(SECONDARY_MASTER_ADDR) $(if $(CHUNK_HOST),-addr $(CHUNK_HOST):9002)
 
# Run chunk server 3
# Usage: make run-chunk_server3 MASTER_ADDR=<primary:port> SECONDARY_MASTER_ADDR=<secondary:port> 
run-chunk_server3: build
	@if [ -z "$(SECONDARY_MASTER_ADDR)" ]; then \
		echo "WARNING: SECONDARY_MASTER_ADDR not set — chunkserver3 will NOT fail over if primary master dies!"; \
	fi
	@./bin/chunkserver -port 9003 -storage chunk_server3 -master $(MASTER_ADDR) -secondary-master $(SECONDARY_MASTER_ADDR) $(if $(CHUNK_HOST),-addr $(CHUNK_HOST):9003)
 
# Run primary master (with secondary address)
# Usage: make run-master-primary SECONDARY_ADDR=192.168.1.20:50052 MY_ADDR=192.168.1.10:50051
 
# run-master-primary: build
# 	@if [ -z "$(MY_ADDR)" ]; then \
# 		echo "Error: MY_ADDR not specified. Usage: make run-master-primary MY_ADDR=192.168.1.10:50051 SECONDARY_ADDR=192.168.1.20:50052"; exit 1; \
# 	fi
# 	@./bin/master -addr $(MY_ADDR) -secondary $(SECONDARY_ADDR)
 
# # Run secondary master (standby mode — monitors the primary for heartbeats)
# # Usage: make run-master-secondary MY_ADDR=192.168.1.66:50052 PRIMARY_ADDR=192.168.1.87:50051
# run-master-secondary: build
# 	@if [ -z "$(MY_ADDR)" ]; then \
# 		echo "Error: MY_ADDR not specified. Usage: make run-master-secondary MY_ADDR=192.168.1.66:50052 PRIMARY_ADDR=192.168.1.87:50051"; exit 1; \
# 	fi
# 	@./bin/master -addr $(MY_ADDR) -secondary $(PRIMARY_ADDR)
 
run-master-primary: build
	@if [ -z "$(MY_ADDR)" ]; then \
		echo "Error: MY_ADDR not specified. Usage: make run-master-primary MY_ADDR=192.168.1.10:50051 SECONDARY_ADDR=192.168.1.20:50052"; exit 1; \
	fi
	@./bin/master -addr $(MY_ADDR) -secondary $(SECONDARY_ADDR) -role primary
 
run-master-secondary: build
	@if [ -z "$(MY_ADDR)" ]; then \
		echo "Error: MY_ADDR not specified. Usage: make run-master-secondary MY_ADDR=192.168.1.20:50052 PRIMARY_ADDR=192.168.1.10:50051"; exit 1; \
	fi
	@./bin/master -addr $(MY_ADDR) -secondary $(PRIMARY_ADDR) -role secondary
 
# Run web server
run-webserver: build
	@./bin/webserver
 
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
 
# Download a file (usage: make download FILE=myfile.pdf)
download:
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make download FILE=myfile.pdf"; exit 1; \
	fi
	@./bin/client download "$(FILE)"
 
# Delete a file (usage: make delete FILE=myfile.pdf)
delete:
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make delete FILE=myfile.pdf"; exit 1; \
	fi
	@./bin/client delete "$(FILE)"
 
# List all files uploaded by this client (usage: cd clients/client1 && make ls)
ls:
	./bin/client ls
 
# List files with details (usage: make ls-detailed [FOLDER=path])
ls-detailed:
	@if [ -z "$(FOLDER)" ]; then \
		./bin/client ls-detailed; \
	else \
		./bin/client ls-detailed $(FOLDER); \
	fi
 
# Create a folder (usage: make mkdir FOLDER=documents/photos)
mkdir:
	@if [ -z "$(FOLDER)" ]; then \
		echo "Error: FOLDER not specified. Usage: make mkdir FOLDER=path"; exit 1; \
	else \
		./bin/client mkdir "$(FOLDER)"; \
	fi
 
# Remove an empty folder (usage: make rmdir FOLDER=documents/photos)
rmdir:
	@if [ -z "$(FOLDER)" ]; then \
		echo "Error: FOLDER not specified. Usage: make rmdir FOLDER=path"; exit 1; \
	else \
		./bin/client rmdir "$(FOLDER)"; \
	fi
 
# Move/rename a file (usage: make mv SRC=file.pdf DEST=folder/file.pdf)
mv:
	@if [ -z "$(SRC)" ] || [ -z "$(DEST)" ]; then \
		echo "Error: SRC and DEST required. Usage: make mv SRC=source DEST=destination"; exit 1; \
	else \
		./bin/client mv "$(SRC)" "$(DEST)"; \
	fi
 
# Preview file content (usage: make cat FILE=readme.txt)
cat:
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make cat FILE=filename"; exit 1; \
	else \
		./bin/client cat "$(FILE)"; \
	fi
 
# Set the master address for this client workspace
# Usage (from client workspace): make set-master MASTER_ADDR=192.168.1.10:50051
.PHONY: set-master
set-master:
	@if [ -z "$(MASTER_ADDR)" ]; then \
		echo "Error: MASTER_ADDR not specified. Usage: make set-master MASTER_ADDR=host:port [SECONDARY_MASTER_ADDR=host:port]"; exit 1; \
	fi
	@echo "$(MASTER_ADDR)" > .master_addr
	@echo "Wrote .master_addr with $(MASTER_ADDR)"
	@if [ -n "$(SECONDARY_MASTER_ADDR)" ]; then \
		echo "$(SECONDARY_MASTER_ADDR)" > .secondary_master_addr; \
		echo "Wrote .secondary_master_addr with $(SECONDARY_MASTER_ADDR)"; \
	fi
 
.PHONY: set_master
set_master: set-master
 
# Generate protobuf code (if you modify dfs.proto)
proto:
	@echo "Generating protobuf code..."
	@protoc --go_out=dfspb --go_opt=paths=source_relative \
		--go-grpc_out=dfspb --go-grpc_opt=paths=source_relative \
		dfs.proto
	@echo "Protobuf generation complete"
 

# Help
help:
	@echo ""
	@echo "╔════════════════════════════════════════════════════════════════╗"
	@echo "║      DFS Project - Multi-Machine Setup Guide                   ║"
	@echo "╚════════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "📦 BUILD"
	@echo "  make build         Build all Go binaries (master, chunkserver, client, webserver)"
	@echo "  make clean         Remove binaries, data, and logs"
	@echo ""
	@echo "🚀 SETUP FOR MULTI-MACHINE DEPLOYMENT"
	@echo ""
	@echo "MACHINE 1 - Primary Master:"
	@echo "  make run-master-primary MY_ADDR=<this-machine-ip>:50051 SECONDARY_ADDR=<secondary-master-ip>:50052"
	@echo "  Example: make run-master-primary MY_ADDR=192.168.100.1:50051 SECONDARY_ADDR=192.168.100.2:50052"
	@echo ""
	@echo "MACHINE 2 - Secondary Master:"
	@echo "  make run-master-secondary MY_ADDR=<this-machine-ip>:50052 PRIMARY_ADDR=<primary-master-ip>:50051"
	@echo "  Example: make run-master-secondary MY_ADDR=192.168.100.2:50052 PRIMARY_ADDR=192.168.100.1:50051"
	@echo ""
	@echo "MACHINE 3 - Chunk Server 1:"
	@echo "  make run-chunk_server1 MASTER_ADDR=<primary-master-ip>:50051 SECONDARY_MASTER_ADDR=<secondary-master-ip>:50052 CHUNK_HOST=<this-machine-ip>"
	@echo "  Example: make run-chunk_server1 MASTER_ADDR=192.168.100.1:50051 SECONDARY_MASTER_ADDR=192.168.100.2:50052 CHUNK_HOST=192.168.100.3"
	@echo ""
	@echo "MACHINE 4 - Chunk Server 2:"
	@echo "  make run-chunk_server2 MASTER_ADDR=<primary-master-ip>:50051 SECONDARY_MASTER_ADDR=<secondary-master-ip>:50052 CHUNK_HOST=<this-machine-ip>"
	@echo "  Example: make run-chunk_server2 MASTER_ADDR=192.168.100.1:50051 SECONDARY_MASTER_ADDR=192.168.100.2:50052 CHUNK_HOST=192.168.100.4"
	@echo ""
	@echo "MACHINE 5 - Chunk Server 3:"
	@echo "  make run-chunk_server3 MASTER_ADDR=<primary-master-ip>:50051 SECONDARY_MASTER_ADDR=<secondary-master-ip>:50052 CHUNK_HOST=<this-machine-ip>"
	@echo "  Example: make run-chunk_server3 MASTER_ADDR=192.168.100.1:50051 SECONDARY_MASTER_ADDR=192.168.100.2:50052 CHUNK_HOST=192.168.100.5"
	@echo ""
	@echo "MACHINE 6 - Webserver (REST API):"
	@echo "  1. Create configuration files with master addresses:"
	@echo "     echo 'PRIMARY_MASTER_IP:50051' > .master_addr"
	@echo "     echo 'SECONDARY_MASTER_IP:50052' > .secondary_master_addr"
	@echo "  2. Run webserver:"
	@echo "     make run-webserver"
	@echo ""
	@echo "  Example:"
	@echo "    echo '192.168.100.1:50051' > .master_addr"
	@echo "    echo '192.168.100.2:50052' > .secondary_master_addr"
	@echo "    make run-webserver"
	@echo ""
	@echo "MACHINE 6 (or separate) - Frontend (Vite Dev Server):"
	@echo "  cd web && npm run dev -- --host 0.0.0.0"
	@echo ""
	@echo "🌐 ACCESS POINTS"
	@echo "  Web UI:            http://<webserver-machine>:5173"
	@echo "  REST API:          http://<webserver-machine>:8080"
	@echo "  Primary Master:    <primary-master-ip>:50051"
	@echo "  Secondary Master:  <secondary-master-ip>:50052"
	@echo "  Chunkserver 1:     <chunk-server-1-ip>:9001"
	@echo "  Chunkserver 2:     <chunk-server-2-ip>:9002"
	@echo "  Chunkserver 3:     <chunk-server-3-ip>:9003"
	@echo ""
	@echo "🧹 CLEANUP"
	@echo "  make clean         Remove all binaries, data, and logs"
	@echo ""