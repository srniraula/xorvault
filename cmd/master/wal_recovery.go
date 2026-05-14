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
	file, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			m.logger.Printf("No WAL file found - starting fresh")
			return nil
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

		if len(line) == 0 {
			continue
		}

		var entry WALEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			m.logger.Printf("Warning: Failed to parse WAL line %d: %v", lineNum, err)
			continue
		}

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
	m.walMu.Lock()
	startOffset := m.walOffset
	m.walMu.Unlock()

	file, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to open WAL file: %v", err)
	}
	defer file.Close()

	if startOffset > 0 {
		if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
			return fmt.Errorf("WAL seek failed: %v", err)
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

	exists := false
	for _, f := range m.clientIDs[createData.ClientID] {
		if f == createData.Filename {
			exists = true
			break
		}
	}
	if !exists {
		m.clientIDs[createData.ClientID] = append(m.clientIDs[createData.ClientID], createData.Filename)
	}

	m.ensureClientMaps(createData.ClientID)

	if _, ok := m.fileInfo[createData.ClientID][createData.Filename]; !ok {
		m.fileInfo[createData.ClientID][createData.Filename] = make(map[int32]*dfspb.StripeMetadata)
	}

	m.fileSizes[createData.ClientID][createData.Filename] = createData.TotalSize

	return nil
}

// replayAllocateChunk restores stripe metadata and chunk status
func (m *MasterServer) replayAllocateChunk(data json.RawMessage) error {
	var allocData AllocateChunkData
	if err := json.Unmarshal(data, &allocData); err != nil {
		return fmt.Errorf("failed to unmarshal AllocateChunk data: %v", err)
	}

	m.ensureClientMaps(allocData.ClientID)
	if _, ok := m.fileInfo[allocData.ClientID][allocData.Filename]; !ok {
		m.fileInfo[allocData.ClientID][allocData.Filename] = make(map[int32]*dfspb.StripeMetadata)
	}

	for stripeNum, stripe := range allocData.Stripes {
		m.fileInfo[allocData.ClientID][allocData.Filename][stripeNum] = stripe

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

	allChunkIDs := []string{}
	if clientFiles, clientExists := m.fileInfo[clientID]; clientExists {
		if stripes, exists := clientFiles[filename]; exists {
			for _, stripe := range stripes {
				allChunkIDs = append(allChunkIDs, stripe.ChunkIds...)
			}
		}
	}

	delete(m.fileInfo[clientID], filename)
	delete(m.fileSizes[clientID], filename)

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

	for _, chunkID := range allChunkIDs {
		delete(m.chunkStatus, chunkID)
	}

	m.logger.Printf("Recovered DELETE_FILE: client=%d, file=%s, chunks_removed=%d",
		clientID, filename, len(allChunkIDs))

	return nil
}
