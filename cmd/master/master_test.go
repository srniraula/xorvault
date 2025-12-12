package main

import (
	"bufio"
	"context"
	"dfs-project/dfspb"
	"encoding/json"
	"log"
	"os"
	"testing"
)

// TestCreateFileWAL tests that CreateFile appends an entry to the WAL
func TestCreateFileWAL(t *testing.T) {
	// Create a temporary WAL file for testing
	walPath := "test_master.wal"
	// defer os.Remove(walPath) // Clean up after test

	// Open WAL file
	walFile, err := os.OpenFile(walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("Failed to create WAL file: %v", err)
	}
	defer walFile.Close()

	// Create test logger
	logger := log.New(os.Stdout, "TEST: ", log.LstdFlags)

	// Create test MasterServer
	server := &MasterServer{
		fileInfo:  make(map[string]map[int32]*dfspb.StripeMetadata),
		clientIDs: make(map[int64][]string),
		fileSizes: make(map[string]int64),
		walFile:   walFile,
		walWriter: bufio.NewWriter(walFile),
		logger:    logger,
	}

	// Initialize fileInfo map for test file
	server.fileInfo["test.pdf"] = make(map[int32]*dfspb.StripeMetadata)

	// Call CreateFile
	req := &dfspb.CreateFileRequest{
		Filename:  "test.pdf",
		TotalSize: 1024,
		ClientId:  0, // Will be auto-generated
	}

	resp, err := server.CreateFile(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	if !resp.Success {
		t.Fatalf("CreateFile returned success=false")
	}

	if resp.ClientId == 0 {
		t.Fatalf("CreateFile should have generated a client ID")
	}

	// Flush WAL to ensure data is written
	server.walWriter.Flush()
	walFile.Close()

	// Read and verify WAL file
	walFile, err = os.Open(walPath)
	if err != nil {
		t.Fatalf("Failed to open WAL for reading: %v", err)
	}
	defer walFile.Close()

	scanner := bufio.NewScanner(walFile)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		line := scanner.Text()

		// Parse the WAL entry
		var entry WALEntry
		err := json.Unmarshal([]byte(line), &entry)
		if err != nil {
			t.Fatalf("Failed to parse WAL entry: %v", err)
		}

		// Verify operation type
		if entry.Operation != OpCreateFile {
			t.Errorf("Expected operation %s, got %s", OpCreateFile, entry.Operation)
		}

		// Verify timestamp exists
		if entry.Timestamp == 0 {
			t.Errorf("WAL entry has no timestamp")
		}

		// Parse and verify the data
		var data CreateFileData
		err = json.Unmarshal(entry.Data, &data)
		if err != nil {
			t.Fatalf("Failed to parse WAL data: %v", err)
		}

		if data.Filename != "test.pdf" {
			t.Errorf("Expected filename test.pdf, got %s", data.Filename)
		}

		if data.TotalSize != 1024 {
			t.Errorf("Expected size 1024, got %d", data.TotalSize)
		}

		if data.ClientID != resp.ClientId {
			t.Errorf("Expected client ID %d, got %d", resp.ClientId, data.ClientID)
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("Error reading WAL file: %v", err)
	}

	if lineCount != 1 {
		t.Errorf("Expected 1 WAL entry, found %d", lineCount)
	}

	t.Logf("WAL test passed: CreateFile logged correctly")
}
