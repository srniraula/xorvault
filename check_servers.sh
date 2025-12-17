#!/bin/bash

echo "=== XorFS Server Status Diagnostic ==="
echo ""

echo "1. Checking Master Server (192.168.1.75:50051)..."
nc -zv 192.168.1.75 50051 2>&1 | grep -q "succeeded" && echo "✅ Master Server is REACHABLE" || echo "❌ Master Server is UNREACHABLE"
echo ""

echo "2. Checking ChunkServer 1 (192.168.1.77:9001)..."
nc -zv 192.168.1.77 9001 2>&1 | grep -q "succeeded" && echo "✅ ChunkServer 1 is REACHABLE" || echo "❌ ChunkServer 1 is UNREACHABLE"
echo ""

echo "3. Checking ChunkServer 2 (192.168.1.75:9002)..."
nc -zv 192.168.1.75 9002 2>&1 | grep -q "succeeded" && echo "✅ ChunkServer 2 is REACHABLE" || echo "❌ ChunkServer 2 is UNREACHABLE"
echo ""

echo "4. Checking ChunkServer 3 (192.168.1.65:9003)..."
nc -zv 192.168.1.65 9003 2>&1 | grep -q "succeeded" && echo "✅ ChunkServer 3 is REACHABLE" || echo "❌ ChunkServer 3 is UNREACHABLE"
echo ""

echo "=== Recommendation ==="
echo "For RAID-5 to work properly, you need at least 3 chunk servers running."
echo "You can tolerate ONE server being offline, but not more than one."
echo ""
echo "Current setup based on your description:"
echo "- ChunkServer 1 (192.168.1.77:9001): Stores Data Chunk 1"
echo "- ChunkServer 2 (192.168.1.75:9002): Stores Parity Chunk (YOU STOPPED THIS ONE)"
echo "- ChunkServer 3 (192.168.1.65:9003): Stores Data Chunk 2"
echo ""
echo "To test RAID-5 reconstruction:"
echo "1. Make sure Master + all 3 ChunkServers are running"
echo "2. Upload a file: make upload FILE=big.pdf"
echo "3. Stop ONE chunk server (e.g., ChunkServer 2)"
echo "4. Download the file: make download FILE=big.pdf"
echo "5. The system should reconstruct the missing chunks automatically"
