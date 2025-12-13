.PHONY: all build clean run-master run-chunk1 run-chunk2 run-chunk3 test proto

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
	@rm -f *.log
	@rm -f downloaded_*
	@echo "Clean complete"

# Run master server
run-master: build
	@./bin/master

# Run chunk server 1
run-chunk_server1: build
	@./bin/chunkserver -port 9001 -storage chunk_server1

# Run chunk server 2
run-chunk_server2: build
	@./bin/chunkserver -port 9002 -storage chunk_server2

# Run chunk server 3
run-chunk_server3: build
	@./bin/chunkserver -port 9003 -storage chunk_server3

# Upload a file (usage: make upload FILE=myfile.pdf)
upload: build
	@./bin/client upload $(FILE)

# Download a file (usage: make download FILE=myfile.pdf)
download: build
	@./bin/client download $(FILE)

# Delete a file (usage: make delete FILE=myfile.pdf)
delete: build
	@./bin/client delete $(FILE)

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
	@echo "  make build         - Build all binaries"
	@echo "  make clean         - Remove binaries and data"
	@echo "  make run-master    - Run master server"
	@echo "  make run-chunk_server1    - Run chunk server 1 (port 9001)"
	@echo "  make run-chunk_server2    - Run chunk server 2 (port 9002)"
	@echo "  make run-chunk_server3    - Run chunk server 3 (port 9003)"
	@echo "  make upload FILE=<file>   - Upload a file"
	@echo "  make download FILE=<file> - Download a file"
	@echo "  make delete FILE=<file>   - Delete a file"
	@echo "  make proto         - Regenerate protobuf code"
	@echo ""
	@echo "Example workflow:"
	@echo "  Terminal 1: make run-master"
	@echo "  Terminal 2: make run-chunk_server1"
	@echo "  Terminal 3: make run-chunk_server2"
	@echo "  Terminal 4: make run-chunk_server3"
	@echo "  Terminal 5: make upload FILE=test.pdf"
