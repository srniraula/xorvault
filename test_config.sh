#!/bin/bash

# XorFS Cross-Machine Testing Configuration
# This script configures the test environment for distributed testing across machines
#
# Usage:
#   1. On MacBook: ./test_config.sh --role macbook --master-ip 192.168.1.100 --chunk-servers 9001,9002,9003
#   2. On Kali VM: ./test_config.sh --role kali --master-ip 192.168.1.100 --secondary-master-ip 192.168.1.200
#
# Topology:
#   MacBook (Primary):
#     - Master:      192.168.1.100:50051
#     - ChunkServer1: 192.168.1.100:9001
#     - ChunkServer3: 192.168.1.100:9003
#
#   Kali VM (Secondary):
#     - Secondary Master: 192.168.1.200:50052
#     - ChunkServer2:     192.168.1.200:9002
#     - Client (test):    can run from here

set -e

# Default values
ROLE=""
MASTER_IP=""
SECONDARY_MASTER_IP=""
CHUNK_SERVERS=""
CLIENT_WORKSPACE="./clients/test_client"
CONFIG_DIR="./test_config"

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print colored output
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --role)
            ROLE="$2"
            shift 2
            ;;
        --master-ip)
            MASTER_IP="$2"
            shift 2
            ;;
        --secondary-master-ip)
            SECONDARY_MASTER_IP="$2"
            shift 2
            ;;
        --chunk-servers)
            CHUNK_SERVERS="$2"
            shift 2
            ;;
        --client-workspace)
            CLIENT_WORKSPACE="$2"
            shift 2
            ;;
        --help)
            show_help
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Show help message
show_help() {
    cat << EOF
${GREEN}XorFS Cross-Machine Testing Configuration${NC}

${YELLOW}Usage:${NC}
  ./test_config.sh --role ROLE [OPTIONS]

${YELLOW}Roles:${NC}
  macbook         Configure MacBook Air (primary master, chunk servers 1 & 3)
  kali            Configure Kali VM (secondary master, chunk server 2, test client)

${YELLOW}Options:${NC}
  --role ROLE                   Machine role (required): macbook | kali
  --master-ip IP                Primary master IP (required for macbook)
  --secondary-master-ip IP      Secondary master IP (required for kali)
  --chunk-servers PORTS         Chunk server ports comma-separated (default: 9001,9002,9003)
  --client-workspace PATH       Client workspace directory (default: ./clients/test_client)
  --help                        Show this help message

${YELLOW}Examples:${NC}

  # On MacBook Air (Primary):
  ./test_config.sh --role macbook --master-ip 192.168.1.100

  # On Kali VM (Secondary):
  ./test_config.sh --role kali --master-ip 192.168.1.100 --secondary-master-ip 192.168.1.200

${YELLOW}Network Topology:${NC}

  MacBook Air (192.168.1.100)           Kali VM (192.168.1.200)
  ├─ Master:50051 (Primary)  ←────────→ Secondary Master:50052
  ├─ ChunkServer:9001                   ChunkServer:9002
  └─ ChunkServer:9003

${YELLOW}Next Steps:${NC}
  1. Run 'make build' to build all binaries
  2. Start services: 'make run-master-primary' (macbook) or 'make run-master-secondary' (kali)
  3. Start chunk servers: 'make run-chunk_server1' on macbook, 'make run-chunk_server2' on kali
  4. Run tests: 'cd clients/test_client && make test-suite'

EOF
}

# Validate inputs
validate_inputs() {
    if [ -z "$ROLE" ]; then
        print_error "Role is required. Use --role macbook or --role kali"
        show_help
        exit 1
    fi

    if [[ ! "$ROLE" =~ ^(macbook|kali)$ ]]; then
        print_error "Invalid role: $ROLE. Must be 'macbook' or 'kali'"
        exit 1
    fi

    if [ "$ROLE" = "macbook" ] && [ -z "$MASTER_IP" ]; then
        print_error "Primary master IP is required for macbook role"
        show_help
        exit 1
    fi

    if [ "$ROLE" = "kali" ] && [ -z "$MASTER_IP" ]; then
        print_error "Primary master IP is required for kali role"
        show_help
        exit 1
    fi
}

# Create configuration directory
setup_config_dir() {
    print_info "Creating configuration directory: $CONFIG_DIR"
    mkdir -p "$CONFIG_DIR"
    print_success "Configuration directory created"
}

# Configure MacBook (Primary)
configure_macbook() {
    print_info "Configuring MacBook Air (Primary Master + ChunkServers 1 & 3)"

    setup_config_dir

    # Create master configuration
    cat > "$CONFIG_DIR/master.env" << EOF
# MacBook Primary Master Configuration
MY_ADDR=0.0.0.0:50051
MASTER_IP=$MASTER_IP
MASTER_PORT=50051
SECONDARY_ADDR=${SECONDARY_MASTER_IP}:50052
DFS_ROLE=master
LOG_FILE=log_files/master.log
EOF

    # Create chunk server 1 configuration
    cat > "$CONFIG_DIR/chunkserver1.env" << EOF
# MacBook ChunkServer 1 Configuration
PORT=9001
STORAGE=chunk_server1
MASTER_ADDR=$MASTER_IP:50051
SECONDARY_MASTER_ADDR=${SECONDARY_MASTER_IP}:50052
DFS_ROLE=chunkserver
LOG_FILE=log_files/chunkserver1.log
EOF

    # Create chunk server 3 configuration
    cat > "$CONFIG_DIR/chunkserver3.env" << EOF
# MacBook ChunkServer 3 Configuration
PORT=9003
STORAGE=chunk_server3
MASTER_ADDR=$MASTER_IP:50051
SECONDARY_MASTER_ADDR=${SECONDARY_MASTER_IP}:50052
DFS_ROLE=chunkserver
LOG_FILE=log_files/chunkserver3.log
EOF

    # Create startup script for MacBook
    cat > "$CONFIG_DIR/start_macbook.sh" << 'EOF'
#!/bin/bash

# Start MacBook services
set -e

echo "[1] Building binaries..."
make build

echo "[2] Starting Primary Master (port 50051)..."
make run-master-primary MY_ADDR=0.0.0.0:50051 SECONDARY_ADDR=${SECONDARY_MASTER_ADDR} &
MASTER_PID=$!
sleep 2

echo "[3] Starting ChunkServer 1 (port 9001)..."
make run-chunk_server1 MASTER_ADDR=${MASTER_ADDR} SECONDARY_MASTER_ADDR=${SECONDARY_MASTER_ADDR} &
CS1_PID=$!
sleep 1

echo "[4] Starting ChunkServer 3 (port 9003)..."
make run-chunk_server3 MASTER_ADDR=${MASTER_ADDR} SECONDARY_MASTER_ADDR=${SECONDARY_MASTER_ADDR} &
CS3_PID=$!
sleep 1

echo "[SUCCESS] All services started on MacBook"
echo "  Master:        http://localhost:50051"
echo "  ChunkServer1:  http://localhost:9001"
echo "  ChunkServer3:  http://localhost:9003"
echo ""
echo "Press Ctrl+C to stop all services"
echo ""

wait
EOF

    chmod +x "$CONFIG_DIR/start_macbook.sh"

    print_success "MacBook configuration complete"
    echo ""
    echo "Configuration files created in: $CONFIG_DIR"
    echo "  - master.env"
    echo "  - chunkserver1.env"
    echo "  - chunkserver3.env"
    echo "  - start_macbook.sh"
    echo ""
    echo "To start services on MacBook:"
    echo "  $ source test_config/master.env && source test_config/chunkserver1.env"
    echo "  $ make run-master-primary MY_ADDR=0.0.0.0:50051 SECONDARY_ADDR=$SECONDARY_MASTER_IP:50052"
    echo "  $ make run-chunk_server1 MASTER_ADDR=$MASTER_IP:50051 SECONDARY_MASTER_ADDR=$SECONDARY_MASTER_IP:50052"
    echo "  $ make run-chunk_server3 MASTER_ADDR=$MASTER_IP:50051 SECONDARY_MASTER_ADDR=$SECONDARY_MASTER_IP:50052"
}

# Configure Kali VM (Secondary)
configure_kali() {
    print_info "Configuring Kali VM (Secondary Master + ChunkServer 2 + Test Client)"

    setup_config_dir

    # Create secondary master configuration
    cat > "$CONFIG_DIR/master_secondary.env" << EOF
# Kali Secondary Master Configuration
MY_ADDR=0.0.0.0:50052
SECONDARY_MASTER_IP=$SECONDARY_MASTER_IP
SECONDARY_MASTER_PORT=50052
PRIMARY_MASTER_ADDR=${MASTER_IP}:50051
DFS_ROLE=secondary_master
LOG_FILE=log_files/master_secondary.log
EOF

    # Create chunk server 2 configuration
    cat > "$CONFIG_DIR/chunkserver2.env" << EOF
# Kali ChunkServer 2 Configuration
PORT=9002
STORAGE=chunk_server2
MASTER_ADDR=${MASTER_IP}:50051
SECONDARY_MASTER_ADDR=${SECONDARY_MASTER_IP}:50052
DFS_ROLE=chunkserver
LOG_FILE=log_files/chunkserver2.log
EOF

    # Create client configuration
    cat > "$CONFIG_DIR/client.env" << EOF
# Kali Test Client Configuration
MASTER_ADDR=${MASTER_IP}:50051
SECONDARY_MASTER_ADDR=${SECONDARY_MASTER_IP}:50052
CLIENT_ID=0
WORKSPACE=$CLIENT_WORKSPACE
EOF

    # Create client test suite wrapper script
    cat > "$CONFIG_DIR/run_tests.sh" << 'EOFSCRIPT'
#!/bin/bash

# Test suite runner for cross-machine testing
set -e

RESULTS_DIR="test_results_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$RESULTS_DIR"

echo "╔════════════════════════════════════════════════════════════╗"
echo "║     XorFS Cross-Machine Testing Suite                      ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo ""
echo "Results directory: $RESULTS_DIR"
echo ""

# Source environment
source test_config/client.env

export MASTER_ADDR
export SECONDARY_MASTER_ADDR

# Test 1: Upload small file
echo "[Test 1/6] Uploading small file (10MB)..."
./bin/client upload files/test_small.bin 2>&1 | tee "$RESULTS_DIR/test_1_upload_small.log"

# Test 2: Download small file
echo "[Test 2/6] Downloading small file..."
./bin/client download test_small.bin 2>&1 | tee "$RESULTS_DIR/test_2_download_small.log"

# Test 3: Upload medium file
echo "[Test 3/6] Uploading medium file (100MB)..."
./bin/client upload files/test_medium.bin 2>&1 | tee "$RESULTS_DIR/test_3_upload_medium.log"

# Test 4: Download medium file
echo "[Test 4/6] Downloading medium file..."
./bin/client download test_medium.bin 2>&1 | tee "$RESULTS_DIR/test_4_download_medium.log"

# Test 5: List files
echo "[Test 5/6] Listing files..."
./bin/client ls 2>&1 | tee "$RESULTS_DIR/test_5_list.log"

# Test 6: Delete file
echo "[Test 6/6] Deleting file..."
./bin/client delete test_small.bin 2>&1 | tee "$RESULTS_DIR/test_6_delete.log"

echo ""
echo "✓ Test suite completed"
echo "Results saved to: $RESULTS_DIR"
EOFSCRIPT

    chmod +x "$CONFIG_DIR/run_tests.sh"

    # Create startup script for Kali
    cat > "$CONFIG_DIR/start_kali.sh" << 'EOF'
#!/bin/bash

# Start Kali services
set -e

echo "[1] Building binaries..."
make build

echo "[2] Starting Secondary Master (port 50052)..."
make run-master-secondary MY_ADDR=0.0.0.0:50052 &
MASTER_SECONDARY_PID=$!
sleep 2

echo "[3] Starting ChunkServer 2 (port 9002)..."
make run-chunk_server2 MASTER_ADDR=${MASTER_ADDR} SECONDARY_MASTER_ADDR=${SECONDARY_MASTER_ADDR} &
CS2_PID=$!
sleep 1

echo "[SUCCESS] All services started on Kali VM"
echo "  Secondary Master: http://localhost:50052"
echo "  ChunkServer2:     http://localhost:9002"
echo ""
echo "To run tests, open another terminal:"
echo "  $ cd clients/test_client && make test-suite"
echo ""
echo "Press Ctrl+C to stop all services"
echo ""

wait
EOF

    chmod +x "$CONFIG_DIR/start_kali.sh"

    # Create client workspace
    print_info "Creating client workspace: $CLIENT_WORKSPACE"
    mkdir -p "$CLIENT_WORKSPACE/files"
    mkdir -p "$CLIENT_WORKSPACE/log_files"

    # Copy necessary files to client workspace
    cp test_config/client.env "$CLIENT_WORKSPACE/"
    
    # Create Makefile for client workspace
    cat > "$CLIENT_WORKSPACE/Makefile" << 'EOF'
.PHONY: test-suite test-upload test-download test-list test-delete test-failover

# Test suite - runs all basic tests
test-suite:
	@echo "╔═══════════════════════════════════════════════════════════╗"
	@echo "║  XorFS Cross-Machine Test Suite                          ║"
	@echo "╚═══════════════════════════════════════════════════════════╝"
	@echo ""
	@make test-upload
	@echo ""
	@make test-download
	@echo ""
	@make test-list
	@echo ""
	@make test-delete

# Individual tests
test-upload:
	@echo "[Test] Upload operation..."
	@../../bin/client upload files/testfile.pdf

test-download:
	@echo "[Test] Download operation..."
	@../../bin/client download testfile.pdf

test-list:
	@echo "[Test] List files..."
	@../../bin/client ls

test-delete:
	@echo "[Test] Delete operation..."
	@../../bin/client delete testfile.pdf

test-failover:
	@echo "[Test] Master failover test..."
	@echo "Kill the primary master and verify automatic failover to secondary"
	@../../bin/client ls

# Create test files
create-test-files:
	@echo "Creating test files..."
	@dd if=/dev/urandom of=files/testfile_10mb.pdf bs=1M count=10
	@dd if=/dev/urandom of=files/testfile_100mb.pdf bs=1M count=100
	@dd if=/dev/urandom of=files/testfile_500mb.pdf bs=1M count=500
	@echo "Test files created in files/"

# Clean up
clean:
	@rm -f .client_id
	@rm -f downloaded_*
	@rm -rf test_results_*
	@echo "Client workspace cleaned"
EOF

    print_success "Kali VM configuration complete"
    echo ""
    echo "Configuration files created in: $CONFIG_DIR"
    echo "  - master_secondary.env"
    echo "  - chunkserver2.env"
    echo "  - client.env"
    echo "  - start_kali.sh"
    echo "  - run_tests.sh"
    echo ""
    echo "Client workspace created: $CLIENT_WORKSPACE"
    echo ""
    echo "To start services on Kali VM:"
    echo "  $ make run-master-secondary MY_ADDR=0.0.0.0:50052"
    echo "  $ make run-chunk_server2 MASTER_ADDR=$MASTER_IP:50051 SECONDARY_MASTER_ADDR=$SECONDARY_MASTER_IP:50052"
    echo ""
    echo "To run tests:"
    echo "  $ cd $CLIENT_WORKSPACE"
    echo "  $ make create-test-files"
    echo "  $ make test-suite"
}

# Main execution
main() {
    print_info "XorFS Cross-Machine Testing Configuration"
    echo ""

    validate_inputs

    case "$ROLE" in
        macbook)
            configure_macbook
            ;;
        kali)
            configure_kali
            ;;
    esac

    echo ""
    print_success "Configuration complete!"
    echo ""
    print_warning "Important: Ensure network connectivity between machines"
    echo "  MacBook IP:  192.168.1.100"
    echo "  Kali IP:     192.168.1.200"
    echo ""
    print_info "Next steps:"
    echo "  1. On MacBook: Run the master and chunk servers"
    echo "  2. On Kali: Run the secondary master and chunk server 2"
    echo "  3. On Kali: Run the test suite"
    echo ""
}

main "$@"
