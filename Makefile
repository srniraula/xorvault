# .PHONY: all build clean run-master run-chunk1 run-chunk2 run-chunk3 test proto

# # Build all binaries
# all: build

# # Build all components
# build:
# 	@echo "Building binaries..."
# 	@go build -o bin/master ./cmd/master
# 	@go build -o bin/chunkserver ./cmd/chunkserver
# 	@go build -o bin/client ./cmd/client
# 	@echo "Build complete: binaries in bin/"

# # Clean build artifacts and data
# clean:
# 	@echo "Cleaning..."
# 	@rm -rf bin/
# 	@rm -rf chunk_server1/ chunk_server2/ chunk_server3/
# 	@rm -f *.log
# 	@rm -f downloaded_*
# 	@echo "Clean complete"

# # Run master server
# run-master: build
# 	@./bin/master

# # Run chunk server 1
# run-chunk_server1: build
# 	@./bin/chunkserver -port 9001 -storage chunk_server1

# # Run chunk server 2
# run-chunk_server2: build
# 	@./bin/chunkserver -port 9002 -storage chunk_server2

# # Run chunk server 3
# run-chunk_server3: build
# 	@./bin/chunkserver -port 9003 -storage chunk_server3

# # Upload a file (usage: make upload FILE=myfile.pdf)
# upload: build
# 	@./bin/client upload $(FILE)

# # Download a file (usage: make download FILE=myfile.pdf)
# download: build
# 	@./bin/client download $(FILE)

# # Delete a file (usage: make delete FILE=myfile.pdf)
# delete: build
# 	@./bin/client delete $(FILE)

# # List all files uploaded by this client
# ls: build
# 	@./bin/client ls

# # Generate protobuf code (if you modify dfs.proto)
# proto:
# 	@echo "Generating protobuf code..."
# 	@protoc --go_out=dfspb --go_opt=paths=source_relative \
# 		--go-grpc_out=dfspb --go-grpc_opt=paths=source_relative \
# 		dfs.proto
# 	@echo "Protobuf generation complete"

# # Help
# help:
# 	@echo "DFS Project Makefile"
# 	@echo ""
# 	@echo "Usage:"
# 	@echo "  make build         - Build all binaries"
# 	@echo "  make clean         - Remove binaries and data"
# 	@echo "  make run-master    - Run master server"
# 	@echo "  make run-chunk_server1    - Run chunk server 1 (port 9001)"
# 	@echo "  make run-chunk_server2    - Run chunk server 2 (port 9002)"
# 	@echo "  make run-chunk_server3    - Run chunk server 3 (port 9003)"
# 	@echo "  make upload FILE=<file>   - Upload a file"
# 	@echo "  make download FILE=<file> - Download a file"
# 	@echo "  make delete FILE=<file>   - Delete a file"
# 	@echo "  make ls            - List all files uploaded by this client"
# 	@echo "  make proto         - Regenerate protobuf code"
# 	@echo ""
# 	@echo "Example workflow:"
# 	@echo "  Terminal 1: make run-master"
# 	@echo "  Terminal 2: make run-chunk_server1"
# 	@echo "  Terminal 3: make run-chunk_server2"
# 	@echo "  Terminal 4: make run-chunk_server3"
# 	@echo "  Terminal 5: make upload FILE=test.pdf"


# .PHONY: all build clean run-master run-chunk1 run-chunk2 run-chunk3 test proto

# # Default master address (override with: make upload FILE=test.pdf MASTER=192.168.1.100:50051)
# MASTER ?= 127.0.0.1:50051

# # Build all binaries
# all: build

# # Build all components
# build:
# 	@echo "Building binaries..."
# 	@go build -o bin/master ./cmd/master
# 	@go build -o bin/chunkserver ./cmd/chunkserver
# 	@go build -o bin/client ./cmd/client
# 	@echo "Build complete: binaries in bin/"

# # Clean build artifacts and data
# clean:
# 	@echo "Cleaning..."
# 	@rm -rf bin/
# 	@rm -rf chunk_server1/ chunk_server2/ chunk_server3/
# 	@rm -f *.log *.wal *.checkpoint
# 	@rm -f downloaded_*
# 	@rm -f .client_id .master_config
# 	@echo "Clean complete"

# # Run master server
# run-master: build
# 	@./bin/master

# # Run chunk server 1
# run-chunk_server1: build
# 	@./bin/chunkserver -port 9001 -storage chunk_server1 -master $(MASTER)

# # Run chunk server 2
# run-chunk_server2: build
# 	@./bin/chunkserver -port 9002 -storage chunk_server2 -master $(MASTER)

# # Run chunk server 3
# run-chunk_server3: build
# 	@./bin/chunkserver -port 9003 -storage chunk_server3 -master $(MASTER)

# # Upload a file (usage: make upload FILE=myfile.pdf)
# upload: build
# 	@if [ -z "$(FILE)" ]; then \
# 		echo "Error: FILE not specified. Usage: make upload FILE=myfile.pdf"; \
# 		exit 1; \
# 	fi
# 	@./bin/client -master=$(MASTER) upload $(FILE)

# # Download a file (usage: make download FILE=myfile.pdf)
# download: build
# 	@if [ -z "$(FILE)" ]; then \
# 		echo "Error: FILE not specified. Usage: make download FILE=myfile.pdf"; \
# 		exit 1; \
# 	fi
# 	@./bin/client -master=$(MASTER) download $(FILE)

# # Delete a file (usage: make delete FILE=myfile.pdf)
# delete: build
# 	@if [ -z "$(FILE)" ]; then \
# 		echo "Error: FILE not specified. Usage: make delete FILE=myfile.pdf"; \
# 		exit 1; \
# 	fi
# 	@./bin/client -master=$(MASTER) delete $(FILE)

# # List all files uploaded by this client
# ls: build
# 	@./bin/client -master=$(MASTER) ls

# # Setup master config file (usage: make setup-master MASTER_IP=192.168.1.100)
# setup-master:
# 	@if [ -z "$(MASTER_IP)" ]; then \
# 		echo "127.0.0.1:50051" > .master_config; \
# 		echo "Master config created: 127.0.0.1:50051 (local)"; \
# 	else \
# 		echo "$(MASTER_IP):50051" > .master_config; \
# 		echo "Master config created: $(MASTER_IP):50051"; \
# 	fi

# # Generate protobuf code (if you modify dfs.proto)
# proto:
# 	@echo "Generating protobuf code..."
# 	@protoc --go_out=dfspb --go_opt=paths=source_relative \
# 		--go-grpc_out=dfspb --go-grpc_opt=paths=source_relative \
# 		dfs.proto
# 	@echo "Protobuf generation complete"

# # Help
# help:
# 	@echo "DFS Project Makefile"
# 	@echo ""
# 	@echo "Usage:"
# 	@echo "  make build                    - Build all binaries"
# 	@echo "  make clean                    - Remove binaries and data"
# 	@echo "  make run-master               - Run master server"
# 	@echo "  make run-chunk_server1        - Run chunk server 1 (port 9001)"
# 	@echo "  make run-chunk_server2        - Run chunk server 2 (port 9002)"
# 	@echo "  make run-chunk_server3        - Run chunk server 3 (port 9003)"
# 	@echo "  make upload FILE=<file>       - Upload a file"
# 	@echo "  make download FILE=<file>     - Download a file"
# 	@echo "  make delete FILE=<file>       - Delete a file"
# 	@echo "  make ls                       - List all files uploaded by this client"
# 	@echo "  make setup-master [MASTER_IP=<ip>] - Create .master_config file"
# 	@echo "  make proto                    - Regenerate protobuf code"
# 	@echo ""
# 	@echo "Master Server Configuration:"
# 	@echo "  Default: MASTER=127.0.0.1:50051 (local)"
# 	@echo "  Override: make upload FILE=test.pdf MASTER=192.168.1.100:50051"
# 	@echo "  Or create .master_config: make setup-master MASTER_IP=192.168.1.100"
# 	@echo ""
# 	@echo "Example workflow (local):"
# 	@echo "  Terminal 1: make run-master"
# 	@echo "  Terminal 2: make run-chunk_server1"
# 	@echo "  Terminal 3: make run-chunk_server2"
# 	@echo "  Terminal 4: make run-chunk_server3"
# 	@echo "  Terminal 5: make upload FILE=test.pdf"
# 	@echo ""
# 	@echo "Example workflow (remote master at 192.168.1.100):"
# 	@echo "  On master machine:"
# 	@echo "    Terminal 1: make run-master"
# 	@echo "    Terminal 2: make run-chunk_server1 MASTER=192.168.1.100:50051"
# 	@echo "    Terminal 3: make run-chunk_server2 MASTER=192.168.1.100:50051"
# 	@echo "    Terminal 4: make run-chunk_server3 MASTER=192.168.1.100:50051"
# 	@echo "  On client machine (Kali VM):"
# 	@echo "    make setup-master MASTER_IP=192.168.1.100"
# 	@echo "    make upload FILE=test.pdf"


.PHONY: all build clean run-master run-chunk1 run-chunk2 run-chunk3 test proto

# Master address (leave empty to use .master_config file)
# Override with: make upload FILE=test.pdf MASTER=192.168.1.100:50051
MASTER ?=

# Build all binaries
all: build

# Build all components
build:
	@echo "Building binaries..."
	@go build -o bin/master ./cmd/master
	@go build -o bin/chunkserver ./cmd/chunkserver
	@go build -o bin/client ./cmd/client
	@echo "Build complete: binaries in bin/"

# Clean build artifacts and data
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -rf chunk_server1/ chunk_server2/ chunk_server3/
	@rm -f *.log *.wal *.checkpoint
	@rm -f downloaded_*
	@rm -f .client_id .master_config
	@echo "Clean complete"

# Run master server
run-master: build
	@./bin/master

# Run chunk server 1
run-chunk_server1: build
	@if [ -z "$(MASTER)" ]; then \
		./bin/chunkserver -port 9001 -storage chunk_server1 -master 127.0.0.1:50051; \
	else \
		./bin/chunkserver -port 9001 -storage chunk_server1 -master $(MASTER); \
	fi

# Run chunk server 2
run-chunk_server2: build
	@if [ -z "$(MASTER)" ]; then \
		./bin/chunkserver -port 9002 -storage chunk_server2 -master 127.0.0.1:50051; \
	else \
		./bin/chunkserver -port 9002 -storage chunk_server2 -master $(MASTER); \
	fi

# Run chunk server 3
run-chunk_server3: build
	@if [ -z "$(MASTER)" ]; then \
		./bin/chunkserver -port 9003 -storage chunk_server3 -master 127.0.0.1:50051; \
	else \
		./bin/chunkserver -port 9003 -storage chunk_server3 -master $(MASTER); \
	fi

# Run chunk server 4
run-chunk_server4: build
	@if [ -z "$(MASTER)" ]; then \
		./bin/chunkserver -port 9004 -storage chunk_server4 -master 127.0.0.1:50051; \
	else \
		./bin/chunkserver -port 9004 -storage chunk_server4 -master $(MASTER); \
	fi

# Upload a file (usage: make upload FILE=myfile.pdf)
upload: build
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make upload FILE=myfile.pdf"; \
		exit 1; \
	fi
	@if [ -z "$(MASTER)" ]; then \
		./bin/client upload $(FILE); \
	else \
		./bin/client -master=$(MASTER) upload $(FILE); \
	fi

# Download a file (usage: make download FILE=myfile.pdf)
download: build
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make download FILE=myfile.pdf"; \
		exit 1; \
	fi
	@if [ -z "$(MASTER)" ]; then \
		./bin/client download $(FILE); \
	else \
		./bin/client -master=$(MASTER) download $(FILE); \
	fi

# Delete a file (usage: make delete FILE=myfile.pdf)
delete: build
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE not specified. Usage: make delete FILE=myfile.pdf"; \
		exit 1; \
	fi
	@if [ -z "$(MASTER)" ]; then \
		./bin/client delete $(FILE); \
	else \
		./bin/client -master=$(MASTER) delete $(FILE); \
	fi

# List all files uploaded by this client
ls: build
	@if [ -z "$(MASTER)" ]; then \
		./bin/client ls; \
	else \
		./bin/client -master=$(MASTER) ls; \
	fi

# Setup master config file (usage: make setup-master MASTER_IP=192.168.1.100)
setup-master:
	@if [ -z "$(MASTER_IP)" ]; then \
		echo "127.0.0.1:50051" > .master_config; \
		echo "Master config created: 127.0.0.1:50051 (local)"; \
	else \
		echo "$(MASTER_IP):50051" > .master_config; \
		echo "Master config created: $(MASTER_IP):50051"; \
	fi

# Generate protobuf code (if you modify dfs.proto)
proto:
	@echo "Generating protobuf code..."
	@protoc --go_out=dfspb --go_opt=paths=source_relative \
		--go-grpc_out=dfspb --go-grpc_opt=paths=source_relative \
		dfs.proto
	@echo "Protobuf generation complete"

# Help
help:
	@echo "DFS Project Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build                    - Build all binaries"
	@echo "  make clean                    - Remove binaries and data"
	@echo "  make run-master               - Run master server"
	@echo "  make run-chunk_server1        - Run chunk server 1 (port 9001)"
	@echo "  make run-chunk_server2        - Run chunk server 2 (port 9002)"
	@echo "  make run-chunk_server3        - Run chunk server 3 (port 9003)"
	@echo "  make upload FILE=<file>       - Upload a file"
	@echo "  make download FILE=<file>     - Download a file"
	@echo "  make delete FILE=<file>       - Delete a file"
	@echo "  make ls                       - List all files uploaded by this client"
	@echo "  make setup-master [MASTER_IP=<ip>] - Create .master_config file"
	@echo "  make proto                    - Regenerate protobuf code"
	@echo ""
	@echo "Master Server Configuration:"
	@echo "  By default, uses .master_config file if it exists"
	@echo "  Override with: make upload FILE=test.pdf MASTER=192.168.1.100:50051"
	@echo "  Create config: make setup-master MASTER_IP=192.168.1.100"
	@echo ""
	@echo "Example workflow (local):"
	@echo "  Terminal 1: make run-master"
	@echo "  Terminal 2: make run-chunk_server1"
	@echo "  Terminal 3: make run-chunk_server2"
	@echo "  Terminal 4: make run-chunk_server3"
	@echo "  Terminal 5: make upload FILE=test.pdf"
	@echo ""
	@echo "Example workflow (remote master at 192.168.1.65):"
	@echo "  On master machine:"
	@echo "    Terminal 1: make run-master"
	@echo "    Terminal 2: make run-chunk_server1 MASTER=192.168.1.65:50051"
	@echo "    Terminal 3: make run-chunk_server2 MASTER=192.168.1.65:50051"
	@echo "    Terminal 4: make run-chunk_server3 MASTER=192.168.1.65:50051"
	@echo "  On client machine (Kali VM):"
	@echo "    make setup-master MASTER_IP=192.168.1.65"
	@echo "    make upload FILE=test.pdf"
	@echo ""
	@echo "Note: .master_config file takes precedence unless MASTER variable is set"

	# Add to Makefile

# Build web server
build-web:
	@echo "Building web server..."
	@go build -o bin/webserver ./cmd/webserver
	@echo "Web server built: bin/webserver"

# Run web server
run-web: build-web
	@./bin/webserver -port 8080 -master $(MASTER)