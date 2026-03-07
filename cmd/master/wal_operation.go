package main

import (
	"dfs-project/dfspb"
	"encoding/json"
	"fmt"
	"time"
)

// WAL operation types
const (
	OpCreateFile    = "CREATE_FILE"
	OpAllocateChunk = "ALLOCATE_CHUNK"
	OpConfirmWrite  = "CONFIRM_WRITE"
	OpDeleteFile    = "DELETE_FILE"
)

// WALEntry represents a single entry in the Write-Ahead Log
type WALEntry struct {
	Operation string          `json:"operation"` // Type of operation
	Timestamp int64           `json:"timestamp"` // Unix timestamp
	Data      json.RawMessage `json:"data"`      // Serialized operation data
}

// CreateFileData stores the data for CreateFile operation
type CreateFileData struct {
	ClientID  int64  `json:"client_id"`
	Filename  string `json:"filename"`
	TotalSize int64  `json:"total_size"`
}

// AllocateChunkData stores the data for AllocateChunk operation with status tracking
type AllocateChunkData struct {
	ClientID int64                           `json:"cliendID"`
	Filename string                          `json:"filename"`
	Stripes  map[int32]*dfspb.StripeMetadata `json:"stripes"` //store full stripe info
	Status   string                          `json:"status"`  // "PENDING" or "SUCCESS"
}

// ConfirmWriteData stores the data for ConfirmWrite operation
type ConfirmWriteData struct {
	Filename string   `json:"filename"`
	ChunkIDs []string `json:"chunk_ids"`
	Status   string   `json:"status"` // "SUCCESS"
}

// DeleteFileData stores the data for DeleteFile operation
type DeleteFileData struct {
	Filename string `json:"filename"`
	ClientID int64  `json:"client_id"`
}

// AppendWAL writes an entry to the Write-Ahead Log with durability guarantees
// and then synchronously replicates to the secondary (best-effort, 2 s timeout).
// This ensures operations are persisted to disk before updating in-memory state.
func (m *MasterServer) AppendWAL(operation string, data interface{}) error {
	m.walMu.Lock()

	// Serialize the operation data to JSON
	dataBytes, err := json.Marshal(data)
	if err != nil {
		m.walMu.Unlock()
		return fmt.Errorf("failed to marshal WAL data: %v", err)
	}

	// Create WAL entry
	entry := WALEntry{
		Operation: operation,
		Timestamp: time.Now().Unix(),
		Data:      dataBytes,
	}

	// Serialize the entire entry to JSON
	entryBytes, err := json.Marshal(entry)
	if err != nil {
		m.walMu.Unlock()
		return fmt.Errorf("failed to marshal WAL entry: %v", err)
	}

	// Write entry as a single line (newline-delimited JSON)
	_, err = m.walWriter.WriteString(string(entryBytes) + "\n")
	if err != nil {
		m.walMu.Unlock()
		return fmt.Errorf("failed to write to WAL: %v", err)
	}

	// Flush buffer to OS
	if err := m.walWriter.Flush(); err != nil {
		m.walMu.Unlock()
		return fmt.Errorf("failed to flush WAL: %v", err)
	}

	// Sync to disk for durability (survives crash/power loss)
	if err := m.walFile.Sync(); err != nil {
		m.walMu.Unlock()
		return fmt.Errorf("failed to sync WAL: %v", err)
	}

	// Increment sequence and capture values before releasing the lock
	m.walSeq++
	seq := m.walSeq
	entryCopy := entry

	// Release lock BEFORE network I/O so other callers are not blocked
	m.walMu.Unlock()

	// Synchronous replication to secondary — uses a 2 s deadline so a slow/dead
	// secondary never stalls the primary for more than 2 s per operation.
	// Errors are logged but do NOT fail the primary write (best-effort HA).
	m.replicateWALToSecondary(entryCopy, seq)

	return nil
}

// logCreateFileToWAL logs a CreateFile operation to the WAL
func (m *MasterServer) LogCreateFileToWAL(clientID int64, filename string, totalSize int64) error {
	walData := CreateFileData{
		ClientID:  clientID,
		Filename:  filename,
		TotalSize: totalSize,
	}
	if err := m.AppendWAL(OpCreateFile, walData); err != nil {
		m.logger.Printf("WAL append failed for CreateFile: %v", err)
		return fmt.Errorf("failed to log to WAL: %v", err)
	}
	return nil
}
