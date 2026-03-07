package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"dfs-project/pkg/auth"
	"dfs-project/pkg/dfsclient"

	"github.com/gin-gonic/gin"
)

func main() {
	// Ensure data directory exists for user storage
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		panic("Failed to create data directory: " + err.Error())
	}

	// Set auth storage directory and initialize
	auth.SetStorageDir(dataDir)
	if err := auth.InitStorage(); err != nil {
		panic("Failed to initialize auth storage: " + err.Error())
	}

	r := gin.Default()

	// Create a DFS client with automatic master failover.
	// All known master addresses are read from MASTER_ADDRS env var
	// (comma-separated, e.g. "192.168.1.10:50051,192.168.1.20:50051").
	// Falls back to MASTER_ADDR / .master_addr file / local defaults.
	cli, err := dfsclient.NewFailoverClient(nil)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	r = NewRouter(cli)

	// Start server on all interfaces so it can be accessed from other devices
	_ = r.Run("0.0.0.0:8080")
}

// NewRouter constructs the Gin engine with handlers using provided DFS client
func NewRouter(cli dfsclient.Client) *gin.Engine {
	r := gin.Default()

	// Simple CORS for dev: allow requests from frontend dev server
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Authentication endpoints (no auth required)
	r.POST("/auth/register", auth.RegisterHandler)
	r.POST("/auth/login", auth.LoginHandler)

	// File operations (require authentication)
	r.POST("/files", auth.AuthMiddleware(), func(c *gin.Context) {
		// Get authenticated user info (clientID is always non-zero, set at registration)
		_, clientID, ok := auth.GetUserFromContext(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
			return
		}

		// Request validation
		filename := c.PostForm("filename")
		fileHeader, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "file required"})
			return
		}

		f, err := fileHeader.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to open file"})
			return
		}
		defer f.Close()

		if filename == "" {
			filename = filepath.Base(fileHeader.Filename)
		}

		size := fileHeader.Size

		// Use a request-scoped timeout for uploads
		uctx, cancel := context.WithTimeout(cRequestContext(c), 10*time.Minute)
		defer cancel()

		assignedClient, err := cli.UploadFile(uctx, int64(clientID), filename, f, size)
		if err != nil {
			// Map common errors to HTTP status
			if err.Error() == "no healthy chunkservers" {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}

		// Verify file shows up in listing (sanity check)
		files, err := cli.ListFiles(cRequestContext(c), assignedClient)
		if err != nil {
			c.JSON(http.StatusCreated, gin.H{"success": true, "clientId": assignedClient, "filename": filename, "warning": "uploaded but verification failed"})
			return
		}
		found := false
		for _, f := range files {
			if f == filename {
				found = true
				break
			}
		}

		if !found {
			c.JSON(http.StatusCreated, gin.H{"success": true, "clientId": assignedClient, "filename": filename, "warning": "uploaded but file not listed yet"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"success": true, "clientId": assignedClient, "filename": filename})
	})

	r.GET("/files", auth.AuthMiddleware(), func(c *gin.Context) {
		// Get authenticated user info
		_, clientID, ok := auth.GetUserFromContext(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
			return
		}

		// If user doesn't have a clientID assigned yet
		if clientID == 0 {
			c.JSON(http.StatusOK, gin.H{"filenames": []string{}})
			return
		}

		// add timeout
		lctx, cancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
		defer cancel()

		files, err := cli.ListFiles(lctx, int64(clientID))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"filenames": files})
	})

	r.GET("/files/:clientId/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
		// Get authenticated user info
		_, userClientID, ok := auth.GetUserFromContext(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
			return
		}

		clientIDStr := c.Param("clientId")
		requestedClientID, err := strconv.ParseInt(clientIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid clientId"})
			return
		}

		// Ensure user can only access their own files
		if int64(userClientID) != requestedClientID {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "access denied"})
			return
		}

		filename := c.Param("filename")
		// Sanity check: verify file exists in listing
		lctx, lcancel := context.WithTimeout(cRequestContext(c), 15*time.Second)
		defer lcancel()
		files, err := cli.ListFiles(lctx, requestedClientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to validate file existence: " + err.Error()})
			return
		}
		found := false
		for _, f := range files {
			if f == filename {
				found = true
				break
			}
		}
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
			return
		}

		tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("download_%d_%s", requestedClientID, filename))
		_ = os.Remove(tmpPath)

		dctx, dcancel := context.WithTimeout(cRequestContext(c), 5*time.Minute)
		defer dcancel()
		if err := cli.DownloadFile(dctx, requestedClientID, filename, tmpPath); err != nil {
			if err.Error() == "file not found" {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		c.File(tmpPath)
		// remove temp file after a short delay to allow transfer to finish
		go func() {
			time.Sleep(1 * time.Second)
			_ = os.Remove(tmpPath)
		}()
	})

	r.DELETE("/files/:clientId/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
		// Get authenticated user info
		_, userClientID, ok := auth.GetUserFromContext(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
			return
		}

		clientIDStr := c.Param("clientId")
		requestedClientID, err := strconv.ParseInt(clientIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid clientId"})
			return
		}

		// Ensure user can only delete their own files
		if int64(userClientID) != requestedClientID {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "access denied"})
			return
		}

		filename := c.Param("filename")
		dctx, dcancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
		defer dcancel()

		deleted, err := cli.DeleteFile(dctx, requestedClientID, filename)
		if err != nil {
			// Map not found
			if err.Error() == "file not found" {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
		msg := "deleted"
		if deleted > 0 {
			msg = fmt.Sprintf("deleted %d chunks", deleted)
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "message": msg})
	})

	// Simplified download endpoint that uses authentication
	r.GET("/files/download/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
		// Get authenticated user info
		_, userClientID, ok := auth.GetUserFromContext(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
			return
		}

		// User must have a clientID assigned
		if userClientID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "no files available"})
			return
		}

		filename := c.Param("filename")
		clientID := int64(userClientID)

		// Sanity check: verify file exists in listing
		lctx, lcancel := context.WithTimeout(cRequestContext(c), 15*time.Second)
		defer lcancel()
		files, err := cli.ListFiles(lctx, clientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to validate file existence: " + err.Error()})
			return
		}
		found := false
		for _, f := range files {
			if f == filename {
				found = true
				break
			}
		}
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
			return
		}

		tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("download_%d_%s", clientID, filename))
		_ = os.Remove(tmpPath)

		dctx, dcancel := context.WithTimeout(cRequestContext(c), 5*time.Minute)
		defer dcancel()
		if err := cli.DownloadFile(dctx, clientID, filename, tmpPath); err != nil {
			if err.Error() == "file not found" {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		c.File(tmpPath)
		// remove temp file after a short delay to allow transfer to finish
		go func() {
			time.Sleep(1 * time.Second)
			_ = os.Remove(tmpPath)
		}()
	})

	// Simplified delete endpoint that uses authentication
	r.DELETE("/files/delete/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
		// Get authenticated user info
		_, userClientID, ok := auth.GetUserFromContext(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
			return
		}

		// User must have a clientID assigned
		if userClientID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "no files to delete"})
			return
		}

		filename := c.Param("filename")
		clientID := int64(userClientID)

		dctx, dcancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
		defer dcancel()

		deleted, err := cli.DeleteFile(dctx, clientID, filename)
		if err != nil {
			// Map not found
			if err.Error() == "file not found" {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
		msg := "deleted"
		if deleted > 0 {
			msg = fmt.Sprintf("deleted %d chunks", deleted)
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "message": msg})
	})

	// Chunked upload endpoints (require authentication)
	r.POST("/files/chunk", auth.AuthMiddleware(), handleChunkUpload)
	r.POST("/files/finalize", auth.AuthMiddleware(), handleFinalizeUpload(cli))
	r.GET("/files/status/:uploadId", auth.AuthMiddleware(), handleUploadStatus)

	return r
}

// ChunkUploadStatus tracks the status of chunked uploads
type ChunkUploadStatus struct {
	UploadId       string    `json:"uploadId"`
	TotalChunks    int       `json:"totalChunks"`
	UploadedChunks []int     `json:"uploadedChunks"`
	Filename       string    `json:"filename"`
	TotalSize      int64     `json:"totalSize"`
	CreatedAt      time.Time `json:"createdAt"`
	mu             sync.RWMutex
}

var (
	uploadStatuses = make(map[string]*ChunkUploadStatus)
	uploadStatusMu sync.RWMutex
)

// handleChunkUpload handles individual chunk uploads
func handleChunkUpload(c *gin.Context) {
	uploadId := c.PostForm("uploadId")
	chunkIndexStr := c.PostForm("chunkIndex")
	totalChunksStr := c.PostForm("totalChunks")
	filename := c.PostForm("filename")

	if uploadId == "" || chunkIndexStr == "" || totalChunksStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "missing required fields"})
		return
	}

	chunkIndex, err := strconv.Atoi(chunkIndexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid chunkIndex"})
		return
	}

	totalChunks, err := strconv.Atoi(totalChunksStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid totalChunks"})
		return
	}

	// Get chunk file
	chunkFile, err := c.FormFile("chunk")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "chunk file required"})
		return
	}

	// Create upload directory
	uploadDir := filepath.Join(os.TempDir(), "dfs_uploads", uploadId)
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create upload directory"})
		return
	}

	// Save chunk
	chunkPath := filepath.Join(uploadDir, fmt.Sprintf("chunk_%d", chunkIndex))
	if err := c.SaveUploadedFile(chunkFile, chunkPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to save chunk"})
		return
	}

	// Update upload status
	uploadStatusMu.Lock()
	status, exists := uploadStatuses[uploadId]
	if !exists {
		status = &ChunkUploadStatus{
			UploadId:       uploadId,
			TotalChunks:    totalChunks,
			UploadedChunks: make([]int, 0),
			Filename:       filename,
			CreatedAt:      time.Now(),
		}
		uploadStatuses[uploadId] = status
	}
	uploadStatusMu.Unlock()

	// Add chunk to uploaded list
	status.mu.Lock()
	// Check if chunk already uploaded
	found := false
	for _, uploaded := range status.UploadedChunks {
		if uploaded == chunkIndex {
			found = true
			break
		}
	}
	if !found {
		status.UploadedChunks = append(status.UploadedChunks, chunkIndex)
	}
	status.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"chunkIndex":     chunkIndex,
		"uploadedChunks": len(status.UploadedChunks),
		"totalChunks":    totalChunks,
	})
}

// handleFinalizeUpload reassembles chunks and uploads to DFS
func handleFinalizeUpload(cli dfsclient.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get authenticated user info (clientID is always non-zero, set at registration)
		_, clientID, ok := auth.GetUserFromContext(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
			return
		}

		uploadId := c.PostForm("uploadId")
		filename := c.PostForm("filename")
		totalSizeStr := c.PostForm("totalSize")

		if uploadId == "" || filename == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "uploadId and filename required"})
			return
		}

		totalSize, err := strconv.ParseInt(totalSizeStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid totalSize"})
			return
		}

		// Get upload status
		uploadStatusMu.RLock()
		status, exists := uploadStatuses[uploadId]
		uploadStatusMu.RUnlock()

		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "upload not found"})
			return
		}

		// Check if all chunks are uploaded
		status.mu.RLock()
		if len(status.UploadedChunks) != status.TotalChunks {
			status.mu.RUnlock()
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": fmt.Sprintf("incomplete upload: %d/%d chunks", len(status.UploadedChunks), status.TotalChunks),
			})
			return
		}
		status.mu.RUnlock()

		// Reassemble file
		uploadDir := filepath.Join(os.TempDir(), "dfs_uploads", uploadId)
		assembledFile := filepath.Join(uploadDir, "assembled_file")

		if err := reassembleChunks(uploadDir, assembledFile, status.TotalChunks); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to reassemble file: " + err.Error()})
			return
		}

		// Open assembled file for upload to DFS
		file, err := os.Open(assembledFile)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to open assembled file"})
			return
		}
		defer file.Close()

		// Upload to DFS with timeout
		uctx, cancel := context.WithTimeout(cRequestContext(c), 15*time.Minute) // Extended timeout for large files
		defer cancel()

		assignedClient, err := cli.UploadFile(uctx, int64(clientID), filename, file, totalSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}

		// Cleanup
		go func() {
			time.Sleep(1 * time.Minute) // Give some time before cleanup
			os.RemoveAll(uploadDir)

			uploadStatusMu.Lock()
			delete(uploadStatuses, uploadId)
			uploadStatusMu.Unlock()
		}()

		c.JSON(http.StatusCreated, gin.H{
			"success":  true,
			"clientId": assignedClient,
			"filename": filename,
			"method":   "chunked",
		})
	}
}

// handleUploadStatus returns the status of a chunked upload
func handleUploadStatus(c *gin.Context) {
	uploadId := c.Param("uploadId")

	uploadStatusMu.RLock()
	status, exists := uploadStatuses[uploadId]
	uploadStatusMu.RUnlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "upload not found"})
		return
	}

	status.mu.RLock()
	response := gin.H{
		"success":        true,
		"uploadId":       status.UploadId,
		"totalChunks":    status.TotalChunks,
		"uploadedChunks": status.UploadedChunks,
		"filename":       status.Filename,
		"progress":       float64(len(status.UploadedChunks)) / float64(status.TotalChunks) * 100,
		"createdAt":      status.CreatedAt,
	}
	status.mu.RUnlock()

	c.JSON(http.StatusOK, response)
}

// reassembleChunks combines individual chunks into a single file
func reassembleChunks(uploadDir, outputPath string, totalChunks int) error {
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outputFile.Close()

	// Read chunks in order and append to output file
	for i := 0; i < totalChunks; i++ {
		chunkPath := filepath.Join(uploadDir, fmt.Sprintf("chunk_%d", i))

		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			return fmt.Errorf("failed to open chunk %d: %v", i, err)
		}

		_, err = io.Copy(outputFile, chunkFile)
		chunkFile.Close()

		if err != nil {
			return fmt.Errorf("failed to copy chunk %d: %v", i, err)
		}
	}

	return nil
}

func cRequestContext(c *gin.Context) context.Context { return c.Request.Context() }
