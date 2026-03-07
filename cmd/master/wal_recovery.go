package main

import (
	"bufio"
	"dfs-project/dfspb"
	"encoding/json"
	"fmt"
	"io"
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

// RecoverFromWALIncremental reads only WAL entries that were appended after
// m.walOffset and updates m.walOffset to the end of those new entries.
// This is used by the standby master to stay in sync efficiently.
// The caller must NOT hold m.walMu (this function acquires it briefly to read offset).
func (m *MasterServer) RecoverFromWALIncremental(walPath string) error {
	// Snapshot the current offset under the WAL mutex so we don't race with the
	// primary's AppendWAL.
	m.walMu.Lock()
	startOffset := m.walOffset
	m.walMu.Unlock()

	file, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No WAL yet - nothing to do
		}
		return fmt.Errorf("failed to open WAL file: %v", err)
	}
	defer file.Close()

	// Seek to where we left off
	if startOffset > 0 {
		// Check for file truncation (offset larger than file size)
		if fi, err := file.Stat(); err == nil {
			if startOffset > fi.Size() {
				m.logger.Printf("Standby: WAL truncated (offset %d > size %d) - resetting to 0", startOffset, fi.Size())
				// Reset offset
				m.walMu.Lock()
				m.walOffset = 0
				m.walMu.Unlock()
				startOffset = 0

				// Since WAL was truncated, the primary must have created a checkpoint.
				// We should reload the checkpoint to ensure state is consistent.
				// Note: LoadCheckpoint doesn't hold m.mu, which is good.
				if cpErr := m.LoadCheckpoint("master.checkpoint"); cpErr != nil {
					m.logger.Printf("Standby truncation reload error: %v", cpErr)
				}
			}
		}

		if startOffset > 0 {
			if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
				return fmt.Errorf("WAL seek failed: %v", err)
			}
		}
	}

	reader := bufio.NewReader(file)
	opsReplayed := 0
	bytesRead := int64(0)

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			bytesRead += int64(len(line))
			trimmed := line
			// Trim the trailing newline
			if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
				trimmed = trimmed[:len(trimmed)-1]
			}
			if len(trimmed) == 0 {
				if err == io.EOF {
					break
				}
				continue
			}

			var entry WALEntry
			if jsonErr := json.Unmarshal([]byte(trimmed), &entry); jsonErr != nil {
				m.logger.Printf("Standby: skipping malformed WAL entry: %v", jsonErr)
			} else {
				m.mu.Lock()
				if replayErr := m.replayOperation(&entry); replayErr != nil {
					m.logger.Printf("Standby: replay error: %v", replayErr)
				} else {
					opsReplayed++
				}
				m.mu.Unlock()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading incremental WAL: %v", err)
		}
	}

	// Advance the stored offset
	if bytesRead > 0 {
		m.walMu.Lock()
		m.walOffset = startOffset + bytesRead
		m.walMu.Unlock()
		m.logger.Printf("Standby WAL sync: +%d bytes, %d ops replayed (total offset %d)",
			bytesRead, opsReplayed, m.walOffset)
	}
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

	// Restore clientID mapping (Check for duplicates to ensure idempotency)
	exists := false
	for _, f := range m.clientIDs[createData.Username] {
		if f == createData.Filename {
			exists = true
			break
		}
	}
	if !exists {
		m.clientIDs[createData.Username] = append(m.clientIDs[createData.Username], createData.Filename)
	}

	// Ensure per-client maps exist before assigning
	m.ensureClientMaps(createData.Username)

	// Initialize fileInfo map for this file
	if _, ok := m.fileInfo[createData.Username][createData.Filename]; !ok {
		m.fileInfo[createData.Username][createData.Filename] = make(map[int32]*dfspb.StripeMetadata)
	}

	// Restore file size
	m.fileSizes[createData.Username][createData.Filename] = createData.TotalSize

	// Only log if verbose or separate logger?
	// m.logger.Printf("Recovered CREATE_FILE: user=%s, file=%s, size=%d", createData.Username, createData.Filename, createData.TotalSize)

	return nil
}

// replayAllocateChunk restores stripe metadata and chunk status
func (m *MasterServer) replayAllocateChunk(data json.RawMessage) error {
	var allocData AllocateChunkData
	if err := json.Unmarshal(data, &allocData); err != nil {
		return fmt.Errorf("failed to unmarshal AllocateChunk data: %v", err)
	}

	// Ensure per-client and per-file maps exist
	m.ensureClientMaps(allocData.Username)
	if _, ok := m.fileInfo[allocData.Username][allocData.Filename]; !ok {
		m.fileInfo[allocData.Username][allocData.Filename] = make(map[int32]*dfspb.StripeMetadata)
	}

	// Restore stripe metadata
	for stripeNum, stripe := range allocData.Stripes {
		m.fileInfo[allocData.Username][allocData.Filename][stripeNum] = stripe

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
	username := deleteData.Username

	// Collect all chunk IDs before deletion (guard for missing client/file)
	allChunkIDs := []string{}
	if clientFiles, clientExists := m.fileInfo[username]; clientExists {
		if stripes, exists := clientFiles[filename]; exists {
			for _, stripe := range stripes {
				allChunkIDs = append(allChunkIDs, stripe.ChunkIds...)
			}
		}
	}

	// Remove file metadata
	delete(m.fileInfo[username], filename)
	delete(m.fileSizes[username], filename)

	// Remove from clientIDs
	if ownedFiles, exists := m.clientIDs[username]; exists {
		updatedFiles := []string{}
		for _, f := range ownedFiles {
			if f != filename {
				updatedFiles = append(updatedFiles, f)
			}
		}
		if len(updatedFiles) > 0 {
			m.clientIDs[username] = updatedFiles
		} else {
			delete(m.clientIDs, username)
		}
	}

	// Remove chunk statuses
	for _, chunkID := range allChunkIDs {
		delete(m.chunkStatus, chunkID)
	}

	m.logger.Printf("Recovered DELETE_FILE: user=%s, file=%s, chunks_removed=%d",
		username, filename, len(allChunkIDs))

	return nil
}
