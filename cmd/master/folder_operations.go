package main

import (
	"context"
	"dfs-project/dfspb"
	"fmt"
	"path/filepath"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// CreateFolder creates a new folder in the client's directory hierarchy
func (m *MasterServer) CreateFolder(ctx context.Context, req *dfspb.CreateFolderRequest) (*dfspb.CreateFolderResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ensure client maps exist
	m.ensureClientMaps(req.ClientId)

	// Clean the folder path (remove trailing slashes, etc.)
	folderPath := filepath.Clean(req.FolderPath)
	if folderPath == "." || folderPath == "/" {
		return &dfspb.CreateFolderResponse{
			Success: false,
			Message: "invalid folder path",
		}, nil
	}

	// Check if folder already exists
	if m.clientFolders[req.ClientId][folderPath] {
		return &dfspb.CreateFolderResponse{
			Success: false,
			Message: fmt.Sprintf("folder '%s' already exists", folderPath),
		}, nil
	}

	// Create folder and all parent folders
	parts := strings.Split(folderPath, "/")
	currentPath := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if currentPath == "" {
			currentPath = part
		} else {
			currentPath = filepath.Join(currentPath, part)
		}
		m.clientFolders[req.ClientId][currentPath] = true
	}

	m.logger.Printf("Client %d created folder: %s", req.ClientId, folderPath)

	return &dfspb.CreateFolderResponse{
		Success: true,
		Message: fmt.Sprintf("folder '%s' created successfully", folderPath),
	}, nil
}

// DeleteFolder removes an empty folder from the client's directory hierarchy
func (m *MasterServer) DeleteFolder(ctx context.Context, req *dfspb.DeleteFolderRequest) (*dfspb.DeleteFolderResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	folderPath := filepath.Clean(req.FolderPath)
	if folderPath == "." || folderPath == "/" {
		return &dfspb.DeleteFolderResponse{
			Success: false,
			Message: "cannot delete root folder",
		}, nil
	}

	// Check if folder exists
	if !m.clientFolders[req.ClientId][folderPath] {
		return &dfspb.DeleteFolderResponse{
			Success: false,
			Message: fmt.Sprintf("folder '%s' not found", folderPath),
		}, nil
	}

	// Check if folder contains files
	prefix := folderPath + "/"
	for filename := range m.fileInfo[req.ClientId] {
		if strings.HasPrefix(filename, prefix) || filename == folderPath {
			return &dfspb.DeleteFolderResponse{
				Success: false,
				Message: fmt.Sprintf("folder '%s' is not empty", folderPath),
			}, nil
		}
	}

	// Check if folder contains subfolders
	for subfolderPath := range m.clientFolders[req.ClientId] {
		if subfolderPath != folderPath && strings.HasPrefix(subfolderPath, prefix) {
			return &dfspb.DeleteFolderResponse{
				Success: false,
				Message: fmt.Sprintf("folder '%s' contains subfolders", folderPath),
			}, nil
		}
	}

	// Delete the folder
	delete(m.clientFolders[req.ClientId], folderPath)

	m.logger.Printf("Client %d deleted folder: %s", req.ClientId, folderPath)

	return &dfspb.DeleteFolderResponse{
		Success: true,
		Message: fmt.Sprintf("folder '%s' deleted successfully", folderPath),
	}, nil
}

// MoveFile moves a file from one path to another (can be used for renaming too)
func (m *MasterServer) MoveFile(ctx context.Context, req *dfspb.MoveFileRequest) (*dfspb.MoveFileResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sourcePath := filepath.Clean(req.SourcePath)
	destPath := filepath.Clean(req.DestinationPath)

	// Check if source file exists
	stripes, exists := m.fileInfo[req.ClientId][sourcePath]
	if !exists {
		return &dfspb.MoveFileResponse{
			Success: false,
			Message: fmt.Sprintf("source file '%s' not found", sourcePath),
		}, nil
	}

	// Check if destination already exists
	if _, exists := m.fileInfo[req.ClientId][destPath]; exists {
		return &dfspb.MoveFileResponse{
			Success: false,
			Message: fmt.Sprintf("destination file '%s' already exists", destPath),
		}, nil
	}

	// Check if destination folder exists (if not root)
	destDir := filepath.Dir(destPath)
	if destDir != "." && destDir != "/" {
		if !m.clientFolders[req.ClientId][destDir] {
			return &dfspb.MoveFileResponse{
				Success: false,
				Message: fmt.Sprintf("destination folder '%s' does not exist", destDir),
			}, nil
		}
	}

	// Move file metadata
	m.fileInfo[req.ClientId][destPath] = stripes
	delete(m.fileInfo[req.ClientId], sourcePath)

	// Move file size
	if size, ok := m.fileSizes[req.ClientId][sourcePath]; ok {
		m.fileSizes[req.ClientId][destPath] = size
		delete(m.fileSizes[req.ClientId], sourcePath)
	}

	// Move upload time
	if uploadTime, ok := m.fileUploadTimes[req.ClientId][sourcePath]; ok {
		m.fileUploadTimes[req.ClientId][destPath] = uploadTime
		delete(m.fileUploadTimes[req.ClientId], sourcePath)
	}

	// Update clientIDs list
	for i, filename := range m.clientIDs[req.ClientId] {
		if filename == sourcePath {
			m.clientIDs[req.ClientId][i] = destPath
			break
		}
	}

	m.logger.Printf("Client %d moved file: %s -> %s", req.ClientId, sourcePath, destPath)

	return &dfspb.MoveFileResponse{
		Success: true,
		Message: fmt.Sprintf("file moved from '%s' to '%s'", sourcePath, destPath),
	}, nil
}

// ListFilesDetailed returns detailed file listing with sizes, timestamps, and folder structure
func (m *MasterServer) ListFilesDetailed(ctx context.Context, req *dfspb.ListFilesDetailedRequest) (*dfspb.ListFilesDetailedResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := []*dfspb.FileMetadata{}

	folderPath := filepath.Clean(req.FolderPath)
	if folderPath == "." {
		folderPath = ""
	}

	// Add folders
	for folder := range m.clientFolders[req.ClientId] {
		// Filter by requested folder path
		if folderPath != "" {
			prefix := folderPath + "/"
			if !strings.HasPrefix(folder, prefix) {
				continue
			}
			// Only show immediate children
			relative := strings.TrimPrefix(folder, prefix)
			if strings.Contains(relative, "/") {
				continue
			}
		} else {
			// Only show top-level folders
			if strings.Contains(folder, "/") {
				continue
			}
		}

		items = append(items, &dfspb.FileMetadata{
			Path:        folder,
			Size:        0,
			UploadTime:  0,
			IsDirectory: true,
		})
	}

	// Add files
	for filename, size := range m.fileSizes[req.ClientId] {
		// Filter by requested folder path
		if folderPath != "" {
			prefix := folderPath + "/"
			if !strings.HasPrefix(filename, prefix) {
				continue
			}
			// Only show immediate children
			relative := strings.TrimPrefix(filename, prefix)
			if strings.Contains(relative, "/") {
				continue
			}
		} else {
			// Only show top-level files
			if strings.Contains(filename, "/") {
				continue
			}
		}

		uploadTime := int64(0)
		if t, ok := m.fileUploadTimes[req.ClientId][filename]; ok {
			uploadTime = t
		}

		items = append(items, &dfspb.FileMetadata{
			Path:        filename,
			Size:        size,
			UploadTime:  uploadTime,
			IsDirectory: false,
		})
	}

	return &dfspb.ListFilesDetailedResponse{
		Items: items,
	}, nil
}

// ReadFileContent reads file content without full download (for viewing/preview)
func (m *MasterServer) ReadFileContent(ctx context.Context, req *dfspb.ReadFileContentRequest) (*dfspb.ReadFileContentResponse, error) {
	m.mu.Lock()

	// Get file metadata
	stripes, exists := m.fileInfo[req.ClientId][req.Filename]
	if !exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("file not found: %s", req.Filename)
	}

	totalSize := int64(0)
	if size, ok := m.fileSizes[req.ClientId][req.Filename]; ok {
		totalSize = size
	}

	m.mu.Unlock()

	// Determine how much to read
	offset := req.Offset
	length := req.Length
	if length == 0 {
		length = totalSize - offset
	}
	if offset+length > totalSize {
		length = totalSize - offset
	}

	// Calculate which stripe(s) we need
	const CHUNK_SIZE = 1 * 1024 * 1024
	const STRIPE_SIZE = 2 * CHUNK_SIZE

	startStripe := int32((offset / STRIPE_SIZE) + 1)
	endStripe := int32(((offset + length - 1) / STRIPE_SIZE) + 1)

	// Collect data from required stripes
	result := []byte{}
	currentOffset := int64(0)

	for stripeNum := startStripe; stripeNum <= endStripe && stripeNum <= int32(len(stripes)); stripeNum++ {
		stripe := stripes[stripeNum]
		if stripe == nil {
			continue
		}

		// Download chunks for this stripe
		stripeData := m.downloadStripeData(stripe, req.ClientId)
		if stripeData == nil {
			return nil, fmt.Errorf("failed to read stripe %d", stripeNum)
		}

		// Extract the needed portion from this stripe
		stripeStart := (int64(stripeNum) - 1) * STRIPE_SIZE
		stripeEnd := stripeStart + int64(len(stripeData))

		// Calculate overlap with requested range
		readStart := max(offset, stripeStart) - stripeStart
		readEnd := min(offset+length, stripeEnd) - stripeStart

		if readEnd > readStart {
			result = append(result, stripeData[readStart:readEnd]...)
			currentOffset += readEnd - readStart
		}
	}

	return &dfspb.ReadFileContentResponse{
		Data:      result,
		TotalSize: totalSize,
	}, nil
}

// downloadStripeData downloads and reconstructs a stripe's data
func (m *MasterServer) downloadStripeData(stripe *dfspb.StripeMetadata, clientID int64) []byte {
	const CHUNK_SIZE = 1 * 1024 * 1024

	// Try to download data chunks
	var data1, data2 []byte

	// Download first data chunk
	if len(stripe.ChunkIds) > 0 && len(stripe.Servers) > 0 {
		data1 = m.downloadChunk(stripe.Servers[0], stripe.ChunkIds[0], clientID)
	}

	// Download second data chunk (if exists)
	if len(stripe.ChunkIds) > 1 && len(stripe.Servers) > 1 {
		data2 = m.downloadChunk(stripe.Servers[1], stripe.ChunkIds[1], clientID)
	}

	// Combine data chunks
	result := []byte{}
	if data1 != nil {
		result = append(result, data1...)
	}
	if data2 != nil {
		result = append(result, data2...)
	}

	return result
}

// downloadChunk downloads a single chunk from a chunk server
func (m *MasterServer) downloadChunk(serverAddr, chunkID string, clientID int64) []byte {
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		m.logger.Printf("Failed to connect to %s: %v", serverAddr, err)
		return nil
	}
	defer conn.Close()

	client := dfspb.NewChunkServerClient(conn)
	resp, err := client.ReadChunk(context.Background(), &dfspb.ReadChunkRequest{
		ChunkId:  chunkID,
		ClientId: clientID,
	})

	if err != nil {
		m.logger.Printf("Failed to read chunk %s from %s: %v", chunkID, serverAddr, err)
		return nil
	}

	return resp.Data
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
