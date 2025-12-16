#!/bin/bash
# Script to set up multiple isolated client directories
# Each client gets their own workspace with a unique .client_id

set -e

NUM_CLIENTS=${1:-3}  # Default to 3 clients if not specified

echo "Setting up $NUM_CLIENTS isolated client workspaces..."

# Create clients directory if it doesn't exist
mkdir -p clients

for i in $(seq 1 $NUM_CLIENTS); do
    CLIENT_DIR="clients/client$i"
    
    # Create client directory
    mkdir -p "$CLIENT_DIR"
    
    # Create a symlink to the Makefile (so they can use make commands)
    if [ ! -L "$CLIENT_DIR/Makefile" ]; then
        ln -s ../../Makefile "$CLIENT_DIR/Makefile" 2>/dev/null || true
    fi
    
    # Create empty .client_id (will be generated on first run)
    touch "$CLIENT_DIR/.client_id"
    
    # Create a README for this client
    cat > "$CLIENT_DIR/README.md" << EOF
# Client $i Workspace

This is an isolated workspace for Client $i.

## Usage:

First, from the project root, run:

    make build

Then, in a new terminal:

    cd clients/client$i
    make upload FILE=test.pdf
    make ls
    make download FILE=test.pdf
    make delete FILE=test.pdf


## Client ID:
This client's unique ID is stored in \`.client_id\` in this directory.
Each client has a different ID, simulating separate users/machines.
EOF

    echo "✓ Created $CLIENT_DIR"
done

echo ""
echo "Setup complete! $NUM_CLIENTS client workspaces created in clients/"
echo ""
echo "Usage without using Docker:"
echo "  1. From the project root, run: make build"
echo "  2. In separate terminals, run:"
echo "     cd clients/client1 && make upload FILE=test1.pdf"
echo "     cd clients/client2 && make upload FILE=test2.pdf"
echo "     cd clients/client3 && make ls"
echo ""
echo "Each client will have a unique .client_id file in their directory."
