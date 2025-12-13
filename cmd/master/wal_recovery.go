package main

import (
	"bufio"
	"dfs-project/dfspb"
	"encoding/json"
	"fmt"
	"os"
)

// RecoverFromWAL reads the WAL file and reconstructs master state
// This is called on master startup to restore metadata after a crash
func (m *MasterServer) RecoverFromWAL(walPath string) error {
	// Check if WAL file exists
	file, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			m.logger.Printf("No WAL file found - starting fresh")
			return nil // Not an error - fresh start
		}
		return fmt.Errorf("failed to open WAL file: %v", err)
	}
	defer file.Close()

	m.logger.Printf("Starting WAL recovery from %s", walPath)

	scanner := bufio.NewScanner(file)
	lineNum := 0
	opsRecovered := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip empty lines
		if len(line) == 0 {
			continue
		}

		// Parse WAL entry
		var entry WALEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			m.logger.Printf("Warning: Failed to parse WAL line %d: %v", lineNum, err)
			continue
		}

		// Replay operation based on type
		if err := m.replayOperation(&entry); err != nil {
			m.logger.Printf("Warning: Failed to replay operation at line %d: %v", lineNum, err)
			continue
		}

		opsRecovered++
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading WAL: %v", err)
	}

	m.logger.Printf("WAL recovery complete: %d operations replayed from %d lines", opsRecovered, lineNum)
	return nil
}

// replayOperation applies a single WAL entry to restore state
func (m *MasterServer) replayOperation(entry *WALEntry) error {
	switch entry.Operation {
	case OpCreateFile:
		return m.replayCreateFile(entry.Data)
	case OpAllocateChunk:
		return m.replayAllocateChunk(entry.Data)
	case OpConfirmWrite:
		return m.replayConfirmWrite(entry.Data)
	case OpDeleteFile:
		return m.replayDeleteFile(entry.Data)
	default:
		return fmt.Errorf("unknown operation: %s", entry.Operation)
	}
}

// replayCreateFile restores state from a CREATE_FILE operation
func (m *MasterServer) replayCreateFile(data json.RawMessage) error {
	var createData CreateFileData
	if err := json.Unmarshal(data, &createData); err != nil {
		return fmt.Errorf("failed to unmarshal CreateFile data: %v", err)
	}

	// Restore clientID mapping
	m.clientIDs[createData.ClientID] = append(m.clientIDs[createData.ClientID], createData.Filename)

	// Initialize fileInfo map for this file
	m.fileInfo[createData.Filename] = make(map[int32]*dfspb.StripeMetadata)

	// Restore file size
	m.fileSizes[createData.Filename] = createData.TotalSize

	m.logger.Printf("Recovered CREATE_FILE: client=%d, file=%s, size=%d",
		createData.ClientID, createData.Filename, createData.TotalSize)

	return nil
}

// replayAllocateChunk restores stripe metadata and chunk status
func (m *MasterServer) replayAllocateChunk(data json.RawMessage) error {
	var allocData AllocateChunkData
	if err := json.Unmarshal(data, &allocData); err != nil {
		return fmt.Errorf("failed to unmarshal AllocateChunk data: %v", err)
	}

	// Restore stripe metadata
	for stripeNum, stripe := range allocData.Stripes {
		m.fileInfo[allocData.Filename][stripeNum] = stripe

		// Restore chunk status (initially PENDING)
		for _, chunkID := range stripe.ChunkIds {
			m.chunkStatus[chunkID] = allocData.Status
		}
	}

	m.logger.Printf("Recovered ALLOCATE_CHUNK: file=%s, stripes=%d, status=%s",
		allocData.Filename, len(allocData.Stripes), allocData.Status)

	return nil
}

// replayConfirmWrite updates chunk status to SUCCESS
func (m *MasterServer) replayConfirmWrite(data json.RawMessage) error {
	var confirmData ConfirmWriteData
	if err := json.Unmarshal(data, &confirmData); err != nil {
		return fmt.Errorf("failed to unmarshal ConfirmWrite data: %v", err)
	}

	// Update chunk status to SUCCESS
	for _, chunkID := range confirmData.ChunkIDs {
		m.chunkStatus[chunkID] = confirmData.Status
	}

	m.logger.Printf("Recovered CONFIRM_WRITE: file=%s, chunks=%d, status=%s",
		confirmData.Filename, len(confirmData.ChunkIDs), confirmData.Status)

	return nil
}

// replayDeleteFile removes file metadata during WAL recovery
func (m *MasterServer) replayDeleteFile(data json.RawMessage) error {
	var deleteData DeleteFileData
	if err := json.Unmarshal(data, &deleteData); err != nil {
		return fmt.Errorf("failed to unmarshal DeleteFile data: %v", err)
	}

	filename := deleteData.Filename
	clientID := deleteData.ClientID

	// Collect all chunk IDs before deletion
	allChunkIDs := []string{}
	if stripes, exists := m.fileInfo[filename]; exists {
		for _, stripe := range stripes {
			allChunkIDs = append(allChunkIDs, stripe.ChunkIds...)
		}
	}

	// Remove file metadata
	delete(m.fileInfo, filename)
	delete(m.fileSizes, filename)

	// Remove from clientIDs
	if ownedFiles, exists := m.clientIDs[clientID]; exists {
		updatedFiles := []string{}
		for _, f := range ownedFiles {
			if f != filename {
				updatedFiles = append(updatedFiles, f)
			}
		}
		if len(updatedFiles) > 0 {
			m.clientIDs[clientID] = updatedFiles
		} else {
			delete(m.clientIDs, clientID)
		}
	}

	// Remove chunk statuses
	for _, chunkID := range allChunkIDs {
		delete(m.chunkStatus, chunkID)
	}

	m.logger.Printf("Recovered DELETE_FILE: client=%d, file=%s, chunks_removed=%d",
		clientID, filename, len(allChunkIDs))

	return nil
}
