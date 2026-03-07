package main

import (
	"bufio"
	"dfs-project/dfspb"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Checkpoint represents a snapshot of master state at a point in time
type Checkpoint struct {
	Timestamp       int64                                               `json:"timestamp"`
	FileInfo        map[string]map[string]map[int32]*StripeMetadataJSON `json:"file_info"`
	ClientIDs       map[string][]string                                 `json:"client_ids"`
	FileSizes       map[string]map[string]int64                         `json:"file_sizes"`
	ChunkStatus     map[string]string                                   `json:"chunk_status"`
	UserPasswords   map[string]string                                   `json:"user_passwords"`
	ClientFolders   map[string]map[string]bool                          `json:"client_folders"`
	FileUploadTimes map[string]map[string]int64                         `json:"file_upload_times"`
}

// StripeMetadataJSON is a JSON-serializable version of StripeMetadata
type StripeMetadataJSON struct {
	StripeNum int32    `json:"stripe_num"`
	ChunkIds  []string `json:"chunk_ids"`
	Servers   []string `json:"servers"`
}

// CreateCheckpoint takes a snapshot of current master state and writes to disk
// This allows faster recovery - only need to replay WAL entries after checkpoint
func (m *MasterServer) CreateCheckpoint(checkpointPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Printf("Creating checkpoint at %s", checkpointPath)

	// Convert protobuf StripeMetadata to JSON-serializable format
	fileInfoJSON := make(map[string]map[string]map[int32]*StripeMetadataJSON)
	for username, _ := range m.fileInfo {
		// initialize per-client entry
		if _, ok := fileInfoJSON[username]; !ok {
			fileInfoJSON[username] = make(map[string]map[int32]*StripeMetadataJSON)
		}

		for filename, stripes := range m.fileInfo[username] {
			fileInfoJSON[username][filename] = make(map[int32]*StripeMetadataJSON)
			for stripeNum, stripe := range stripes {
				fileInfoJSON[username][filename][stripeNum] = &StripeMetadataJSON{
					StripeNum: stripe.StripeNum,
					ChunkIds:  stripe.ChunkIds,
					Servers:   stripe.Servers,
				}
			}
		}
	}

	// Create checkpoint structure
	checkpoint := Checkpoint{
		Timestamp:       time.Now().Unix(),
		FileInfo:        fileInfoJSON,
		ClientIDs:       m.clientIDs,
		FileSizes:       m.fileSizes,
		ChunkStatus:     m.chunkStatus,
		UserPasswords:   m.userPasswords,
		ClientFolders:   m.clientFolders,
		FileUploadTimes: m.fileUploadTimes,
	}

	// Serialize to JSON
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %v", err)
	}

	// Write to temporary file first (atomic write pattern)
	tmpPath := checkpointPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write checkpoint: %v", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, checkpointPath); err != nil {
		return fmt.Errorf("failed to rename checkpoint: %v", err)
	}

	m.logger.Printf("Checkpoint created: %d files, %d clients, %d chunks",
		len(m.fileInfo), len(m.clientIDs), len(m.chunkStatus))

	return nil
}

// LoadCheckpoint restores master state from a checkpoint file
// Called before WAL replay to speed up recovery
func (m *MasterServer) LoadCheckpoint(checkpointPath string) error {
	// Check if checkpoint exists
	data, err := os.ReadFile(checkpointPath)
	if err != nil {
		if os.IsNotExist(err) {
			m.logger.Printf("No checkpoint found - will recover from WAL only")
			return nil // Not an error - just no checkpoint
		}
		return fmt.Errorf("failed to read checkpoint: %v", err)
	}

	m.logger.Printf("Loading checkpoint from %s", checkpointPath)

	// Parse checkpoint
	var checkpoint Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return fmt.Errorf("failed to unmarshal checkpoint: %v", err)
	}

	// Restore state (no lock needed - called during initialization)
	m.clientIDs = checkpoint.ClientIDs
	m.fileSizes = checkpoint.FileSizes
	m.chunkStatus = checkpoint.ChunkStatus
	m.userPasswords = checkpoint.UserPasswords
	if m.userPasswords == nil {
		m.userPasswords = make(map[string]string)
	}
	m.clientFolders = checkpoint.ClientFolders
	if m.clientFolders == nil {
		m.clientFolders = make(map[string]map[string]bool)
	}
	m.fileUploadTimes = checkpoint.FileUploadTimes
	if m.fileUploadTimes == nil {
		m.fileUploadTimes = make(map[string]map[string]int64)
	}

	// Convert JSON format back to protobuf StripeMetadata
	m.fileInfo = make(map[string]map[string]map[int32]*dfspb.StripeMetadata)
	// 	for filename, stripesJSON := range checkpoint.FileInfo {
	// 		m.fileInfo[filename] = make(map[int32]*dfspb.StripeMetadata)
	// 		for stripeNum, stripeJSON := range stripesJSON {
	// 			m.fileInfo[filename][stripeNum] = &dfspb.StripeMetadata{
	// 				StripeNum: stripeJSON.StripeNum,
	// 				ChunkIds:  stripeJSON.ChunkIds,
	// 				Servers:   stripeJSON.Servers,
	// 			}
	// 		}
	// }

	for username, _ := range checkpoint.FileInfo {
		m.fileInfo[username] = make(map[string]map[int32]*dfspb.StripeMetadata)
		for filename, stripesJSON := range checkpoint.FileInfo[username] {
			// initialize per-file map
			if _, ok := m.fileInfo[username][filename]; !ok {
				m.fileInfo[username][filename] = make(map[int32]*dfspb.StripeMetadata)
			}
			for stripeNum, stripeJSON := range stripesJSON {
				m.fileInfo[username][filename][stripeNum] = &dfspb.StripeMetadata{
					StripeNum: stripeJSON.StripeNum,
					ChunkIds:  stripeJSON.ChunkIds,
					Servers:   stripeJSON.Servers,
				}
			}
		}
	}

	checkpointTime := time.Unix(checkpoint.Timestamp, 0)
	m.logger.Printf("Checkpoint loaded: timestamp=%s, files=%d, clients=%d, chunks=%d",
		checkpointTime.Format(time.RFC3339), len(m.fileInfo), len(m.clientIDs), len(m.chunkStatus))

	return nil
}

// TruncateWAL creates a new WAL file, discarding old entries after checkpoint
// This prevents WAL from growing indefinitely
func (m *MasterServer) TruncateWAL(walPath string) error {
	m.walMu.Lock()
	defer m.walMu.Unlock()

	m.logger.Printf("Truncating WAL at %s", walPath)

	// Flush and close current WAL writer
	if err := m.walWriter.Flush(); err != nil {
		return fmt.Errorf("failed to flush WAL before truncate: %v", err)
	}

	if err := m.walFile.Close(); err != nil {
		return fmt.Errorf("failed to close WAL before truncate: %v", err)
	}

	// Rename old WAL as backup
	backupPath := walPath + ".old"
	if err := os.Rename(walPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup old WAL: %v", err)
	}

	// Create new empty WAL
	newWalFile, err := os.OpenFile(walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to create new WAL: %v", err)
	}

	// Update master's WAL references
	m.walFile = newWalFile
	m.walWriter = bufio.NewWriter(newWalFile)

	m.logger.Printf("WAL truncated - old WAL saved to %s", backupPath)

	return nil
}

// PeriodicCheckpoint runs in a goroutine and creates checkpoints periodically
// Recommended interval: 5-10 minutes
// Also polling WAL for Standby mode
func (m *MasterServer) PeriodicCheckpoint(intervalMinutes int, checkpointPath string, walPath string) {
	ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	defer ticker.Stop()

	// WAL polling ticker for Standby mode - Check for new WAL entries very frequently
	walPoller := time.NewTicker(100 * time.Millisecond)
	defer walPoller.Stop()

	for {
		select {
		case <-ticker.C:
			// If Standby, we skip checkpoint creation (Primary handles it)
			if m.IsStandby {
				continue
			}
			// Create checkpoint
			if err := m.CreateCheckpoint(checkpointPath); err != nil {
				m.logger.Printf("ERROR: Failed to create checkpoint: %v", err)
				continue
			}

			// Truncate WAL after successful checkpoint
			if err := m.TruncateWAL(walPath); err != nil {
				m.logger.Printf("ERROR: Failed to truncate WAL: %v", err)
			}
			m.logger.Printf("Periodic checkpoint complete")

		case <-walPoller.C:
			// If Standby, poll WAL for new updates using incremental read
			if m.IsStandby {
				if err := m.RecoverFromWALIncremental(walPath); err != nil {
					// Don't spam logs for every poll, but record critical errors
					if !os.IsNotExist(err) {
						m.logger.Printf("Standby sync error: %v", err)
					}
				}
			}
		}
	}
}
