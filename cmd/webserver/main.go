// package main

// import (
// 	"context"
// 	"fmt"
// 	"io"
// 	"net/http"
// 	"os"
// 	"path/filepath"
// 	"strconv"
// 	"sync"
// 	"time"

// 	"dfs-project/pkg/auth"
// 	"dfs-project/pkg/dfsclient"
// 	"dfs-project/pkg/webserver"

// 	"github.com/gin-gonic/gin"
// )

// func main() {
// 	// Ensure data directory exists for user storage
// 	dataDir := "data"
// 	if err := os.MkdirAll(dataDir, 0755); err != nil {
// 		panic("Failed to create data directory: " + err.Error())
// 	}

// 	// Set auth storage directory and initialize
// 	auth.SetStorageDir(dataDir)
// 	if err := auth.InitStorage(); err != nil {
// 		panic("Failed to initialize auth storage: " + err.Error())
// 	}

// 	// Initialize webserver logger (creates webserver_logs/ directory)
// 	_ = webserver.GetWebServerLogger()

// 	r := gin.Default()

// 	// Create a DFS client with automatic master failover.
// 	// All known master addresses are read from MASTER_ADDRS env var
// 	// (comma-separated, e.g. "192.168.1.10:50051,192.168.1.20:50051").
// 	// Falls back to MASTER_ADDR / .master_addr file / local defaults.
// 	cli, err := dfsclient.NewFailoverClient(nil)
// 	if err != nil {
// 		panic(err)
// 	}
// 	defer cli.Close()

// 	r = NewRouter(cli)

// 	// Start server on all interfaces so it can be accessed from other devices
// 	_ = r.Run("0.0.0.0:8080")
// }

// // NewRouter constructs the Gin engine with handlers using provided DFS client
// func NewRouter(cli dfsclient.Client) *gin.Engine {
// 	r := gin.Default()

// 	// Start background goroutine that cleans up abandoned uploads.
// 	// Runs every 5 minutes and removes uploads older than 10 minutes.
// 	go startUploadSweeper(5*time.Minute, 10*time.Minute)

// 	// Simple CORS for dev: allow requests from frontend dev server
// 	r.Use(func(c *gin.Context) {
// 		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
// 		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
// 		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
// 		if c.Request.Method == "OPTIONS" {
// 			c.AbortWithStatus(204)
// 			return
// 		}
// 		c.Next()
// 	})

// 	// Authentication endpoints (no auth required)
// 	r.POST("/auth/register", auth.RegisterHandler)
// 	r.POST("/auth/login", auth.LoginHandler)

// 	// File operations (require authentication)
// 	r.POST("/files", auth.AuthMiddleware(), func(c *gin.Context) {
// 		// Get authenticated user info (clientID is always non-zero, set at registration)
// 		username, clientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		// Request validation
// 		filename := c.PostForm("filename")
// 		fileHeader, err := c.FormFile("file")
// 		if err != nil {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "file required"})
// 			return
// 		}

// 		f, err := fileHeader.Open()
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to open file"})
// 			return
// 		}
// 		defer f.Close()

// 		if filename == "" {
// 			filename = filepath.Base(fileHeader.Filename)
// 		}

// 		size := fileHeader.Size

// 		// Log simple upload start
// 		logger := webserver.GetWebServerLogger()
// 		_ = logger.LogSimpleUploadStart(username, filename, size)

// 		// Use a request-scoped timeout for uploads
// 		uctx, cancel := context.WithTimeout(cRequestContext(c), 10*time.Minute)
// 		defer cancel()

// 		// Start metrics timer BEFORE upload begins
// 		opCtx := NewWebOpCtx("upload", filename, username, size)

// 		assignedClient, err := cli.UploadFile(uctx, int64(clientID), filename, f, size, username)
// 		if err != nil {
// 			// Log upload failure
// 			_ = logger.LogSimpleUploadFailed(username, filename, size, err.Error())
// 			RecordWebMetrics(opCtx.Finalise(err.Error()))
// 			// Map common errors to HTTP status
// 			if err.Error() == "no healthy chunkservers" {
// 				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
// 				return
// 			}
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Verify file shows up in listing (sanity check)
// 		files, err := cli.ListFiles(cRequestContext(c), assignedClient)
// 		if err != nil {
// 			c.JSON(http.StatusCreated, gin.H{"success": true, "clientId": assignedClient, "filename": filename, "warning": "uploaded but verification failed"})
// 			return
// 		}
// 		found := false
// 		for _, f := range files {
// 			if f == filename {
// 				found = true
// 				break
// 			}
// 		}

// 		if !found {
// 			c.JSON(http.StatusCreated, gin.H{"success": true, "clientId": assignedClient, "filename": filename, "warning": "uploaded but file not listed yet"})
// 			return
// 		}

// 		// Log successful upload
// 		_ = logger.LogSimpleUploadComplete(username, filename, size)

// 		// Record metrics (timer started before UploadFile call above)
// 		RecordWebMetrics(opCtx.Finalise(""))

// 		c.JSON(http.StatusCreated, gin.H{"success": true, "clientId": assignedClient, "filename": filename})
// 	})

// 	r.GET("/files", auth.AuthMiddleware(), func(c *gin.Context) {
// 		// Get authenticated user info
// 		username, clientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		// If user doesn't have a clientID assigned yet
// 		if clientID == 0 {
// 			c.JSON(http.StatusOK, gin.H{"filenames": []string{}})
// 			return
// 		}

// 		// add timeout
// 		lctx, cancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
// 		defer cancel()

// 		// Start metrics timer BEFORE ListFiles call
// 		opCtxLs := NewWebOpCtx("ls", "/", username, 0)

// 		files, err := cli.ListFiles(lctx, int64(clientID))
// 		if err != nil {
// 			RecordWebMetrics(opCtxLs.Finalise(err.Error()))
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Log file refresh
// 		if username != "" {
// 			logger := dfsclient.GetUserLogger()
// 			_ = logger.LogFileRefresh(username, len(files))
// 		}

// 		// Record metrics (timer started before ListFiles call above)
// 		RecordWebMetrics(opCtxLs.Finalise(""))

// 		c.JSON(http.StatusOK, gin.H{"filenames": files})
// 	})

// 	r.GET("/files/:clientId/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
// 		// Get authenticated user info
// 		username, userClientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		clientIDStr := c.Param("clientId")
// 		requestedClientID, err := strconv.ParseInt(clientIDStr, 10, 64)
// 		if err != nil {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid clientId"})
// 			return
// 		}

// 		// Ensure user can only access their own files
// 		if int64(userClientID) != requestedClientID {
// 			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "access denied"})
// 			return
// 		}

// 		filename := c.Param("filename")
// 		// Sanity check: verify file exists in listing
// 		lctx, lcancel := context.WithTimeout(cRequestContext(c), 15*time.Second)
// 		defer lcancel()
// 		files, err := cli.ListFiles(lctx, requestedClientID)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to validate file existence: " + err.Error()})
// 			return
// 		}
// 		found := false
// 		for _, f := range files {
// 			if f == filename {
// 				found = true
// 				break
// 			}
// 		}
// 		if !found {
// 			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 			return
// 		}

// 		downloadDir := filepath.Join(os.TempDir(), "dfs_downloads")
// 		if err := os.MkdirAll(downloadDir, 0755); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create download directory"})
// 			return
// 		}
// 		tmpPath := filepath.Join(downloadDir, filename)
// 		_ = os.Remove(tmpPath)

// 		// Initialize download session logging
// 		logger := webserver.GetWebServerLogger()
// 		sessionID, _ := logger.LogDownloadSessionStart(username, filename, requestedClientID, tmpPath)

// 		dctx, dcancel := context.WithTimeout(cRequestContext(c), 5*time.Minute)
// 		defer dcancel()

// 		// Start metrics timer BEFORE DownloadFile call
// 		opCtxDl := NewWebOpCtx("download", filename, username, 0)

// 		if err := cli.DownloadFile(dctx, requestedClientID, filename, tmpPath, username); err != nil {
// 			// Download failed
// 			_ = logger.LogDownloadFailed(sessionID, err.Error())
// 			RecordWebMetrics(opCtxDl.Finalise(err.Error()))
// 			// Clean up incomplete download
// 			if _, statErr := os.Stat(tmpPath); statErr == nil {
// 				if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
// 					_ = logger.LogIncompleteDownloadCleanup(sessionID, tmpPath, sizeFreed)
// 				}
// 			}
// 			os.Remove(tmpPath)

// 			if err.Error() == "file not found" {
// 				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 				return
// 			}
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Set file size now that download succeeded
// 		if info, err2 := os.Stat(tmpPath); err2 == nil {
// 			opCtxDl.SetFileSize(info.Size())
// 		}

// 		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
// 		c.File(tmpPath)

// 		// Log download complete and record metrics
// 		_ = logger.LogDownloadComplete(sessionID)
// 		RecordWebMetrics(opCtxDl.Finalise(""))

// 		// Clean up download file after response is sent
// 		defer func() {
// 			if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
// 				_ = logger.LogDownloadCleanup(sessionID, tmpPath, sizeFreed)
// 			}
// 			os.Remove(tmpPath)
// 		}()
// 	})

// 	r.DELETE("/files/:clientId/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
// 		// Get authenticated user info
// 		username, userClientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		clientIDStr := c.Param("clientId")
// 		requestedClientID, err := strconv.ParseInt(clientIDStr, 10, 64)
// 		if err != nil {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid clientId"})
// 			return
// 		}

// 		// Ensure user can only delete their own files
// 		if int64(userClientID) != requestedClientID {
// 			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "access denied"})
// 			return
// 		}

// 		filename := c.Param("filename")
// 		dctx, dcancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
// 		defer dcancel()

// 		// Log delete initiation
// 		logger := webserver.GetWebServerLogger()
// 		_ = logger.LogFileDeleteInitiated(username, filename, requestedClientID)

// 		deleted, err := cli.DeleteFile(dctx, requestedClientID, filename, username)
// 		if err != nil {
// 			// Map not found
// 			if err.Error() == "file not found" {
// 				_ = logger.LogFileDeleteFailed(username, filename, requestedClientID, "file not found")
// 				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 				return
// 			}
// 			_ = logger.LogFileDeleteFailed(username, filename, requestedClientID, err.Error())
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Log successful deletion
// 		_ = logger.LogFileDeleteSuccess(username, filename, requestedClientID, deleted)

// 		msg := "deleted"
// 		if deleted > 0 {
// 			msg = fmt.Sprintf("deleted %d chunks", deleted)
// 		}
// 		c.JSON(http.StatusOK, gin.H{"success": true, "message": msg})
// 	})

// 	// Simplified download endpoint that uses authentication
// 	r.GET("/files/download/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
// 		// Get authenticated user info
// 		username, userClientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		// User must have a clientID assigned
// 		if userClientID == 0 {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "no files available"})
// 			return
// 		}

// 		filename := c.Param("filename")
// 		clientID := int64(userClientID)

// 		// Sanity check: verify file exists in listing
// 		lctx, lcancel := context.WithTimeout(cRequestContext(c), 15*time.Second)
// 		defer lcancel()
// 		files, err := cli.ListFiles(lctx, clientID)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to validate file existence: " + err.Error()})
// 			return
// 		}
// 		found := false
// 		for _, f := range files {
// 			if f == filename {
// 				found = true
// 				break
// 			}
// 		}
// 		if !found {
// 			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 			return
// 		}

// 		downloadDir := filepath.Join(os.TempDir(), "dfs_downloads")
// 		if err := os.MkdirAll(downloadDir, 0755); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create download directory"})
// 			return
// 		}
// 		tmpPath := filepath.Join(downloadDir, filename)
// 		_ = os.Remove(tmpPath)

// 		// Initialize download session logging
// 		logger := webserver.GetWebServerLogger()
// 		sessionID, _ := logger.LogDownloadSessionStart(username, filename, clientID, tmpPath)

// 		dctx, dcancel := context.WithTimeout(cRequestContext(c), 5*time.Minute)
// 		defer dcancel()

// 		// Start metrics timer BEFORE DownloadFile call
// 		opCtxDl2 := NewWebOpCtx("download", filename, username, 0)

// 		if err := cli.DownloadFile(dctx, clientID, filename, tmpPath, username); err != nil {
// 			// Download failed
// 			_ = logger.LogDownloadFailed(sessionID, err.Error())
// 			RecordWebMetrics(opCtxDl2.Finalise(err.Error()))
// 			// Clean up incomplete download
// 			if _, statErr := os.Stat(tmpPath); statErr == nil {
// 				if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
// 					_ = logger.LogIncompleteDownloadCleanup(sessionID, tmpPath, sizeFreed)
// 				}
// 			}
// 			os.Remove(tmpPath)

// 			if err.Error() == "file not found" {
// 				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 				return
// 			}
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
// 		c.File(tmpPath)

// 		// Log download complete after file is served
// 		_ = logger.LogDownloadComplete(sessionID)

// 		// Set file size and record metrics (timer started before DownloadFile)
// 		if info, err2 := os.Stat(tmpPath); err2 == nil {
// 			opCtxDl2.SetFileSize(info.Size())
// 		}
// 		RecordWebMetrics(opCtxDl2.Finalise(""))

// 		// Clean up download file after response is sent
// 		defer func() {
// 			if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
// 				_ = logger.LogDownloadCleanup(sessionID, tmpPath, sizeFreed)
// 			}
// 			os.Remove(tmpPath)
// 		}()
// 	})

// 	// Simplified delete endpoint that uses authentication
// 	r.DELETE("/files/delete/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
// 		// Get authenticated user info
// 		username, userClientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		// User must have a clientID assigned
// 		if userClientID == 0 {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "no files to delete"})
// 			return
// 		}

// 		filename := c.Param("filename")
// 		clientID := int64(userClientID)

// 		dctx, dcancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
// 		defer dcancel()

// 		// Log delete initiation
// 		logger := webserver.GetWebServerLogger()
// 		_ = logger.LogFileDeleteInitiated(username, filename, clientID)

// 		// Start metrics timer BEFORE DeleteFile call
// 		opCtxDel := NewWebOpCtx("delete", filename, username, 0)

// 		deleted, err := cli.DeleteFile(dctx, clientID, filename, username)
// 		if err != nil {
// 			// Map not found
// 			if err.Error() == "file not found" {
// 				_ = logger.LogFileDeleteFailed(username, filename, clientID, "file not found")
// 				RecordWebMetrics(opCtxDel.Finalise("file not found"))
// 				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 				return
// 			}
// 			_ = logger.LogFileDeleteFailed(username, filename, clientID, err.Error())
// 			RecordWebMetrics(opCtxDel.Finalise(err.Error()))
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Log successful deletion
// 		_ = logger.LogFileDeleteSuccess(username, filename, clientID, deleted)

// 		msg := "deleted"
// 		if deleted > 0 {
// 			msg = fmt.Sprintf("deleted %d chunks", deleted)
// 		}

// 		// Record metrics (timer started before DeleteFile call above)
// 		RecordWebMetrics(opCtxDel.Finalise(""))

// 		c.JSON(http.StatusOK, gin.H{"success": true, "message": msg})
// 	})

// 	// Chunked upload endpoints (require authentication)
// 	r.POST("/files/chunk", auth.AuthMiddleware(), handleChunkUpload)
// 	r.POST("/files/finalize", auth.AuthMiddleware(), handleFinalizeUpload(cli))
// 	r.GET("/files/status/:uploadId", auth.AuthMiddleware(), handleUploadStatus)

// 	// Metrics endpoints (require authentication)
// 	r.GET("/metrics", auth.AuthMiddleware(), func(c *gin.Context) { HandleGetMetrics(c.Writer, c.Request) })
// 	r.GET("/metrics/csv", auth.AuthMiddleware(), func(c *gin.Context) { HandleGetMetricsCSV(c.Writer, c.Request) })

// 	return r
// }

// // ChunkUploadStatus tracks the status of chunked uploads
// type ChunkUploadStatus struct {
// 	UploadId       string    `json:"uploadId"`
// 	Username       string    `json:"username"`
// 	TotalChunks    int       `json:"totalChunks"`
// 	UploadedChunks []int     `json:"uploadedChunks"`
// 	Filename       string    `json:"filename"`
// 	TotalSize      int64     `json:"totalSize"`
// 	CreatedAt      time.Time `json:"createdAt"`
// 	mu             sync.RWMutex
// }

// var (
// 	uploadStatuses = make(map[string]*ChunkUploadStatus)
// 	uploadStatusMu sync.RWMutex
// )

// // handleChunkUpload handles individual chunk uploads
// func handleChunkUpload(c *gin.Context) {
// 	uploadId := c.PostForm("uploadId")
// 	chunkIndexStr := c.PostForm("chunkIndex")
// 	totalChunksStr := c.PostForm("totalChunks")
// 	filename := c.PostForm("filename")

// 	if uploadId == "" || chunkIndexStr == "" || totalChunksStr == "" {
// 		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "missing required fields"})
// 		return
// 	}

// 	chunkIndex, err := strconv.Atoi(chunkIndexStr)
// 	if err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid chunkIndex"})
// 		return
// 	}

// 	totalChunks, err := strconv.Atoi(totalChunksStr)
// 	if err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid totalChunks"})
// 		return
// 	}

// 	// Get chunk file
// 	chunkFile, err := c.FormFile("chunk")
// 	if err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "chunk file required"})
// 		return
// 	}

// 	// Get authenticated user for logging
// 	username, _, ok := auth.GetUserFromContext(c)
// 	if !ok {
// 		username = "unknown"
// 	}

// 	// Create upload directory
// 	uploadDir := filepath.Join(os.TempDir(), "dfs_uploads", uploadId)
// 	if err := os.MkdirAll(uploadDir, 0755); err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create upload directory"})
// 		return
// 	}

// 	// Save chunk
// 	chunkPath := filepath.Join(uploadDir, fmt.Sprintf("chunk_%d", chunkIndex))
// 	if err := c.SaveUploadedFile(chunkFile, chunkPath); err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to save chunk"})
// 		return
// 	}

// 	// Update upload status
// 	uploadStatusMu.Lock()
// 	status, exists := uploadStatuses[uploadId]
// 	if !exists {
// 		status = &ChunkUploadStatus{
// 			UploadId:       uploadId,
// 			Username:       username,
// 			TotalChunks:    totalChunks,
// 			UploadedChunks: make([]int, 0),
// 			Filename:       filename,
// 			CreatedAt:      time.Now(),
// 		}
// 		uploadStatuses[uploadId] = status
// 		uploadStatusMu.Unlock()

// 		// Log upload start (only on first chunk)
// 		logger := webserver.GetWebServerLogger()
// 		_ = logger.LogChunkUploadStart(username, uploadId, filename, totalChunks, uploadDir)
// 	} else {
// 		uploadStatusMu.Unlock()
// 	}

// 	// Add chunk to uploaded list
// 	status.mu.Lock()
// 	// Check if chunk already uploaded
// 	found := false
// 	for _, uploaded := range status.UploadedChunks {
// 		if uploaded == chunkIndex {
// 			found = true
// 			break
// 		}
// 	}
// 	if !found {
// 		status.UploadedChunks = append(status.UploadedChunks, chunkIndex)
// 	}
// 	uploadedCount := len(status.UploadedChunks)
// 	status.mu.Unlock()

// 	// Log chunk stored
// 	logger := webserver.GetWebServerLogger()
// 	chunkSize := chunkFile.Size
// 	totalSize := int64(uploadedCount) * chunkSize // Approximate
// 	_ = logger.LogChunkStored(username, uploadId, chunkIndex, chunkSize, uploadedCount, totalChunks, totalSize, uploadDir)

// 	c.JSON(http.StatusOK, gin.H{
// 		"success":        true,
// 		"chunkIndex":     chunkIndex,
// 		"uploadedChunks": uploadedCount,
// 		"totalChunks":    totalChunks,
// 	})
// }

// // handleFinalizeUpload reassembles chunks and uploads to DFS
// func handleFinalizeUpload(cli dfsclient.Client) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		// Get authenticated user info (clientID is always non-zero, set at registration)
// 		username, clientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		uploadId := c.PostForm("uploadId")
// 		filename := c.PostForm("filename")
// 		totalSizeStr := c.PostForm("totalSize")

// 		if uploadId == "" || filename == "" {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "uploadId and filename required"})
// 			return
// 		}

// 		totalSize, err := strconv.ParseInt(totalSizeStr, 10, 64)
// 		if err != nil {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid totalSize"})
// 			return
// 		}

// 		// Get upload status
// 		uploadStatusMu.RLock()
// 		status, exists := uploadStatuses[uploadId]
// 		uploadStatusMu.RUnlock()

// 		if !exists {
// 			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "upload not found"})
// 			return
// 		}

// 		// Check if all chunks are uploaded
// 		status.mu.RLock()
// 		if len(status.UploadedChunks) != status.TotalChunks {
// 			status.mu.RUnlock()
// 			c.JSON(http.StatusBadRequest, gin.H{
// 				"success": false,
// 				"message": fmt.Sprintf("incomplete upload: %d/%d chunks", len(status.UploadedChunks), status.TotalChunks),
// 			})
// 			return
// 		}
// 		status.mu.RUnlock()

// 		uploadDir := filepath.Join(os.TempDir(), "dfs_uploads", uploadId)

// 		// Log: all chunks uploaded, about to reassemble
// 		logger := webserver.GetWebServerLogger()
// 		_ = logger.LogChunkUploadComplete(username, uploadId, filename, status.TotalChunks, totalSize, uploadDir)

// 		// Reassemble file
// 		assembledFile := filepath.Join(uploadDir, "assembled_file")

// 		// Log: reassembly start
// 		_ = logger.LogChunkReassemblyStart(username, uploadId, uploadDir)

// 		reassemblyStart := time.Now()
// 		if err := reassembleChunks(uploadDir, assembledFile, status.TotalChunks); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to reassemble file: " + err.Error()})
// 			return
// 		}
// 		reassemblyMs := time.Since(reassemblyStart).Milliseconds()

// 		// Log: reassembly complete
// 		_ = logger.LogChunkReassemblyComplete(username, uploadId, totalSize, reassemblyMs)

// 		// Open assembled file for upload to DFS
// 		file, err := os.Open(assembledFile)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to open assembled file"})
// 			return
// 		}
// 		defer file.Close()

// 		// Upload to DFS with timeout
// 		uctx, cancel := context.WithTimeout(cRequestContext(c), 15*time.Minute) // Extended timeout for large files
// 		defer cancel()

// 		// cleanupUpload removes both the temp directory and the status map entry.
// 		cleanupUpload := func() {
// 			// Calculate size freed before deleting
// 			sizeFreed := int64(0)
// 			filepath.Walk(uploadDir, func(path string, info os.FileInfo, err error) error {
// 				if err == nil && !info.IsDir() {
// 					sizeFreed += info.Size()
// 				}
// 				return nil
// 			})

// 			os.RemoveAll(uploadDir)
// 			uploadStatusMu.Lock()
// 			delete(uploadStatuses, uploadId)
// 			uploadStatusMu.Unlock()

// 			// Log cleanup
// 			logger := webserver.GetWebServerLogger()
// 			_ = logger.LogChunkUploadCleanup(username, uploadId, uploadDir, sizeFreed)
// 		}

// 		// Start metrics timer BEFORE UploadFile call
// 		opCtxFin := NewWebOpCtx("upload", filename, username, totalSize)

// 		assignedClient, err := cli.UploadFile(uctx, int64(clientID), filename, file, totalSize, username)
// 		if err != nil {
// 			RecordWebMetrics(opCtxFin.Finalise(err.Error()))
// 			// Also clean up on failure — previously this was skipped, leaking /tmp
// 			go func() {
// 				time.Sleep(1 * time.Minute)
// 				cleanupUpload()
// 			}()
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Cleanup after success
// 		go func() {
// 			time.Sleep(1 * time.Minute) // Brief delay so the file handle is fully released
// 			cleanupUpload()
// 		}()

// 		// Record metrics (timer started before UploadFile call above)
// 		RecordWebMetrics(opCtxFin.Finalise(""))

// 		c.JSON(http.StatusCreated, gin.H{
// 			"success":  true,
// 			"clientId": assignedClient,
// 			"filename": filename,
// 			"method":   "chunked",
// 		})
// 	}
// }

// // handleUploadStatus returns the status of a chunked upload
// func handleUploadStatus(c *gin.Context) {
// 	uploadId := c.Param("uploadId")

// 	uploadStatusMu.RLock()
// 	status, exists := uploadStatuses[uploadId]
// 	uploadStatusMu.RUnlock()

// 	if !exists {
// 		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "upload not found"})
// 		return
// 	}

// 	status.mu.RLock()
// 	response := gin.H{
// 		"success":        true,
// 		"uploadId":       status.UploadId,
// 		"totalChunks":    status.TotalChunks,
// 		"uploadedChunks": status.UploadedChunks,
// 		"filename":       status.Filename,
// 		"progress":       float64(len(status.UploadedChunks)) / float64(status.TotalChunks) * 100,
// 		"createdAt":      status.CreatedAt,
// 	}
// 	status.mu.RUnlock()

// 	c.JSON(http.StatusOK, response)
// }

// // reassembleChunks combines individual chunks into a single file
// func reassembleChunks(uploadDir, outputPath string, totalChunks int) error {
// 	outputFile, err := os.Create(outputPath)
// 	if err != nil {
// 		return fmt.Errorf("failed to create output file: %v", err)
// 	}
// 	defer outputFile.Close()

// 	// Read chunks in order and append to output file
// 	for i := 0; i < totalChunks; i++ {
// 		chunkPath := filepath.Join(uploadDir, fmt.Sprintf("chunk_%d", i))

// 		chunkFile, err := os.Open(chunkPath)
// 		if err != nil {
// 			return fmt.Errorf("failed to open chunk %d: %v", i, err)
// 		}

// 		_, err = io.Copy(outputFile, chunkFile)
// 		chunkFile.Close()

// 		if err != nil {
// 			return fmt.Errorf("failed to copy chunk %d: %v", i, err)
// 		}
// 	}

// 	return nil
// }

// func cRequestContext(c *gin.Context) context.Context { return c.Request.Context() }

// // getFileSize returns the size of a file, or 0 if file doesn't exist
// func getFileSize(path string) (int64, error) {
// 	info, err := os.Stat(path)
// 	if err != nil {
// 		return 0, err
// 	}
// 	return info.Size(), nil
// }

// // startUploadSweeper runs in a background goroutine and periodically evicts
// // abandoned chunked uploads — those where /files/chunk was called but
// // /files/finalize was never called (browser closed, network dropped, etc.).
// //
// // sweepInterval: how often to scan (e.g. every 5 minutes)
// // maxAge:        how old an upload must be before it is considered abandoned (e.g. 10 minutes)
// func startUploadSweeper(sweepInterval, maxAge time.Duration) {
// 	ticker := time.NewTicker(sweepInterval)
// 	defer ticker.Stop()

// 	for range ticker.C {
// 		now := time.Now()

// 		// Collect expired upload IDs under a short read lock.
// 		uploadStatusMu.RLock()
// 		var expired []string
// 		for id, status := range uploadStatuses {
// 			if now.Sub(status.CreatedAt) > maxAge {
// 				expired = append(expired, id)
// 			}
// 		}
// 		uploadStatusMu.RUnlock()

// 		if len(expired) == 0 {
// 			continue
// 		}

// 		// Remove each expired entry from the map and from disk.
// 		uploadStatusMu.Lock()
// 		uploadsDeleted := 0
// 		var totalSizeFreed int64 = 0
// 		var deletedIds []string

// 		logger := webserver.GetWebServerLogger()

// 		for _, id := range expired {
// 			status := uploadStatuses[id]
// 			delete(uploadStatuses, id)
// 			uploadDir := filepath.Join(os.TempDir(), "dfs_uploads", id)

// 			// Calculate size freed before deleting
// 			sizeFreed := int64(0)
// 			filepath.Walk(uploadDir, func(path string, info os.FileInfo, err error) error {
// 				if err == nil && !info.IsDir() {
// 					sizeFreed += info.Size()
// 				}
// 				return nil
// 			})

// 			_ = os.RemoveAll(uploadDir)

// 			// Log abandoned upload cleanup to cleanup log
// 			_ = logger.LogAbandonedUploadCleanup(id, uploadDir, sizeFreed)

// 			// Log abandoned upload to user's personal log file
// 			_ = logger.LogChunkUploadAbandoned(status.Username, id, uploadDir, sizeFreed)

// 			uploadsDeleted++
// 			totalSizeFreed += sizeFreed
// 			deletedIds = append(deletedIds, id)
// 		}
// 		uploadStatusMu.Unlock()

// 		// Log sweep completion summary
// 		sweepSummary := webserver.UploadSweepSummary{
// 			SweepTime:        time.Now(),
// 			UploadsChecked:   len(uploadStatuses),
// 			UploadsDeleted:   uploadsDeleted,
// 			TotalSizeFreed:   totalSizeFreed,
// 			AbandonedUploads: deletedIds,
// 		}
// 		_ = logger.LogUploadSweepComplete(sweepSummary)
// 	}
// }

// package main

// import (
// 	"context"
// 	"fmt"
// 	"io"
// 	"net/http"
// 	"os"
// 	"path/filepath"
// 	"strconv"
// 	"sync"
// 	"time"

// 	"dfs-project/pkg/auth"
// 	"dfs-project/pkg/dfsclient"
// 	"dfs-project/pkg/webserver"

// 	"github.com/gin-gonic/gin"
// )

// func main() {
// 	// Ensure data directory exists for user storage
// 	dataDir := "data"
// 	if err := os.MkdirAll(dataDir, 0755); err != nil {
// 		panic("Failed to create data directory: " + err.Error())
// 	}

// 	// Set auth storage directory and initialize
// 	auth.SetStorageDir(dataDir)
// 	if err := auth.InitStorage(); err != nil {
// 		panic("Failed to initialize auth storage: " + err.Error())
// 	}

// 	// Initialize webserver logger (creates webserver_logs/ directory)
// 	_ = webserver.GetWebServerLogger()

// 	r := gin.Default()

// 	// Create a DFS client with automatic master failover.
// 	// All known master addresses are read from MASTER_ADDRS env var
// 	// (comma-separated, e.g. "192.168.1.10:50051,192.168.1.20:50051").
// 	// Falls back to MASTER_ADDR / .master_addr file / local defaults.
// 	cli, err := dfsclient.NewFailoverClient(nil)
// 	if err != nil {
// 		panic(err)
// 	}
// 	defer cli.Close()

// 	r = NewRouter(cli)

// 	// Start server on all interfaces so it can be accessed from other devices
// 	_ = r.Run("0.0.0.0:8080")
// }

// // NewRouter constructs the Gin engine with handlers using provided DFS client
// func NewRouter(cli dfsclient.Client) *gin.Engine {
// 	r := gin.Default()

// 	// Start background goroutine that cleans up abandoned uploads.
// 	// Runs every 5 minutes and removes uploads older than 10 minutes.
// 	go startUploadSweeper(5*time.Minute, 10*time.Minute)

// 	// Simple CORS for dev: allow requests from frontend dev server
// 	r.Use(func(c *gin.Context) {
// 		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
// 		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
// 		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
// 		if c.Request.Method == "OPTIONS" {
// 			c.AbortWithStatus(204)
// 			return
// 		}
// 		c.Next()
// 	})

// 	// Authentication endpoints (no auth required)
// 	r.POST("/auth/register", auth.RegisterHandler)
// 	r.POST("/auth/login", auth.LoginHandler)

// 	// File operations (require authentication)
// 	r.POST("/files", auth.AuthMiddleware(), func(c *gin.Context) {
// 		// Capture request arrival time immediately — includes HTTP body receive time
// 		requestStart := time.Now()

// 		// Get authenticated user info (clientID is always non-zero, set at registration)
// 		username, clientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		// Request validation
// 		filename := c.PostForm("filename")
// 		fileHeader, err := c.FormFile("file")
// 		if err != nil {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "file required"})
// 			return
// 		}

// 		f, err := fileHeader.Open()
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to open file"})
// 			return
// 		}
// 		defer f.Close()

// 		if filename == "" {
// 			filename = filepath.Base(fileHeader.Filename)
// 		}

// 		size := fileHeader.Size

// 		// Log simple upload start
// 		logger := webserver.GetWebServerLogger()
// 		_ = logger.LogSimpleUploadStart(username, filename, size)

// 		// Use a request-scoped timeout for uploads
// 		uctx, cancel := context.WithTimeout(cRequestContext(c), 10*time.Minute)
// 		defer cancel()

// 		// Use requestStart captured at handler entry for full browser→chunkserver latency
// 		opCtx := NewWebOpCtxAt("upload", filename, username, size, requestStart)

// 		assignedClient, err := cli.UploadFile(uctx, int64(clientID), filename, f, size, username)
// 		if err != nil {
// 			// Log upload failure
// 			_ = logger.LogSimpleUploadFailed(username, filename, size, err.Error())
// 			RecordWebMetrics(opCtx.Finalise(err.Error()))
// 			// Map common errors to HTTP status
// 			if err.Error() == "no healthy chunkservers" {
// 				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
// 				return
// 			}
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Verify file shows up in listing (sanity check)
// 		files, err := cli.ListFiles(cRequestContext(c), assignedClient)
// 		if err != nil {
// 			c.JSON(http.StatusCreated, gin.H{"success": true, "clientId": assignedClient, "filename": filename, "warning": "uploaded but verification failed"})
// 			return
// 		}
// 		found := false
// 		for _, f := range files {
// 			if f == filename {
// 				found = true
// 				break
// 			}
// 		}

// 		if !found {
// 			c.JSON(http.StatusCreated, gin.H{"success": true, "clientId": assignedClient, "filename": filename, "warning": "uploaded but file not listed yet"})
// 			return
// 		}

// 		// Log successful upload
// 		_ = logger.LogSimpleUploadComplete(username, filename, size)

// 		// Record metrics (timer started before UploadFile call above)
// 		RecordWebMetrics(opCtx.Finalise(""))

// 		c.JSON(http.StatusCreated, gin.H{"success": true, "clientId": assignedClient, "filename": filename})
// 	})

// 	r.GET("/files", auth.AuthMiddleware(), func(c *gin.Context) {
// 		requestStart := time.Now()

// 		// Get authenticated user info
// 		username, clientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		// If user doesn't have a clientID assigned yet
// 		if clientID == 0 {
// 			c.JSON(http.StatusOK, gin.H{"filenames": []string{}})
// 			return
// 		}

// 		// add timeout
// 		lctx, cancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
// 		defer cancel()

// 		// Use requestStart for full latency including auth overhead
// 		opCtxLs := NewWebOpCtxAt("ls", "/", username, 0, requestStart)

// 		files, err := cli.ListFiles(lctx, int64(clientID))
// 		if err != nil {
// 			RecordWebMetrics(opCtxLs.Finalise(err.Error()))
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Log file refresh
// 		if username != "" {
// 			logger := dfsclient.GetUserLogger()
// 			_ = logger.LogFileRefresh(username, len(files))
// 		}

// 		// Record metrics (timer started before ListFiles call above)
// 		RecordWebMetrics(opCtxLs.Finalise(""))

// 		c.JSON(http.StatusOK, gin.H{"filenames": files})
// 	})

// 	r.GET("/files/:clientId/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
// 		requestStart := time.Now()

// 		// Get authenticated user info
// 		username, userClientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		clientIDStr := c.Param("clientId")
// 		requestedClientID, err := strconv.ParseInt(clientIDStr, 10, 64)
// 		if err != nil {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid clientId"})
// 			return
// 		}

// 		// Ensure user can only access their own files
// 		if int64(userClientID) != requestedClientID {
// 			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "access denied"})
// 			return
// 		}

// 		filename := c.Param("filename")
// 		// Sanity check: verify file exists in listing
// 		lctx, lcancel := context.WithTimeout(cRequestContext(c), 15*time.Second)
// 		defer lcancel()
// 		files, err := cli.ListFiles(lctx, requestedClientID)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to validate file existence: " + err.Error()})
// 			return
// 		}
// 		found := false
// 		for _, f := range files {
// 			if f == filename {
// 				found = true
// 				break
// 			}
// 		}
// 		if !found {
// 			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 			return
// 		}

// 		downloadDir := filepath.Join(os.TempDir(), "dfs_downloads")
// 		if err := os.MkdirAll(downloadDir, 0755); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create download directory"})
// 			return
// 		}
// 		tmpPath := filepath.Join(downloadDir, filename)
// 		_ = os.Remove(tmpPath)

// 		// Initialize download session logging
// 		logger := webserver.GetWebServerLogger()
// 		sessionID, _ := logger.LogDownloadSessionStart(username, filename, requestedClientID, tmpPath)

// 		dctx, dcancel := context.WithTimeout(cRequestContext(c), 5*time.Minute)
// 		defer dcancel()

// 		// Use requestStart for full latency including listing validation overhead
// 		opCtxDl := NewWebOpCtxAt("download", filename, username, 0, requestStart)

// 		if err := cli.DownloadFile(dctx, requestedClientID, filename, tmpPath, username); err != nil {
// 			// Download failed
// 			_ = logger.LogDownloadFailed(sessionID, err.Error())
// 			RecordWebMetrics(opCtxDl.Finalise(err.Error()))
// 			// Clean up incomplete download
// 			if _, statErr := os.Stat(tmpPath); statErr == nil {
// 				if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
// 					_ = logger.LogIncompleteDownloadCleanup(sessionID, tmpPath, sizeFreed)
// 				}
// 			}
// 			os.Remove(tmpPath)

// 			if err.Error() == "file not found" {
// 				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 				return
// 			}
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Set file size now that download succeeded
// 		if info, err2 := os.Stat(tmpPath); err2 == nil {
// 			opCtxDl.SetFileSize(info.Size())
// 		}

// 		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
// 		c.File(tmpPath)

// 		// Log download complete and record metrics
// 		_ = logger.LogDownloadComplete(sessionID)
// 		RecordWebMetrics(opCtxDl.Finalise(""))

// 		// Clean up download file after response is sent
// 		defer func() {
// 			if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
// 				_ = logger.LogDownloadCleanup(sessionID, tmpPath, sizeFreed)
// 			}
// 			os.Remove(tmpPath)
// 		}()
// 	})

// 	r.DELETE("/files/:clientId/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
// 		// Get authenticated user info
// 		username, userClientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		clientIDStr := c.Param("clientId")
// 		requestedClientID, err := strconv.ParseInt(clientIDStr, 10, 64)
// 		if err != nil {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid clientId"})
// 			return
// 		}

// 		// Ensure user can only delete their own files
// 		if int64(userClientID) != requestedClientID {
// 			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "access denied"})
// 			return
// 		}

// 		filename := c.Param("filename")
// 		dctx, dcancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
// 		defer dcancel()

// 		// Log delete initiation
// 		logger := webserver.GetWebServerLogger()
// 		_ = logger.LogFileDeleteInitiated(username, filename, requestedClientID)

// 		deleted, err := cli.DeleteFile(dctx, requestedClientID, filename, username)
// 		if err != nil {
// 			// Map not found
// 			if err.Error() == "file not found" {
// 				_ = logger.LogFileDeleteFailed(username, filename, requestedClientID, "file not found")
// 				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 				return
// 			}
// 			_ = logger.LogFileDeleteFailed(username, filename, requestedClientID, err.Error())
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Log successful deletion
// 		_ = logger.LogFileDeleteSuccess(username, filename, requestedClientID, deleted)

// 		msg := "deleted"
// 		if deleted > 0 {
// 			msg = fmt.Sprintf("deleted %d chunks", deleted)
// 		}
// 		c.JSON(http.StatusOK, gin.H{"success": true, "message": msg})
// 	})

// 	// Simplified download endpoint that uses authentication
// 	r.GET("/files/download/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
// 		requestStart := time.Now()

// 		// Get authenticated user info
// 		username, userClientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		// User must have a clientID assigned
// 		if userClientID == 0 {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "no files available"})
// 			return
// 		}

// 		filename := c.Param("filename")
// 		clientID := int64(userClientID)

// 		// Sanity check: verify file exists in listing
// 		lctx, lcancel := context.WithTimeout(cRequestContext(c), 15*time.Second)
// 		defer lcancel()
// 		files, err := cli.ListFiles(lctx, clientID)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to validate file existence: " + err.Error()})
// 			return
// 		}
// 		found := false
// 		for _, f := range files {
// 			if f == filename {
// 				found = true
// 				break
// 			}
// 		}
// 		if !found {
// 			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 			return
// 		}

// 		downloadDir := filepath.Join(os.TempDir(), "dfs_downloads")
// 		if err := os.MkdirAll(downloadDir, 0755); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create download directory"})
// 			return
// 		}
// 		tmpPath := filepath.Join(downloadDir, filename)
// 		_ = os.Remove(tmpPath)

// 		// Initialize download session logging
// 		logger := webserver.GetWebServerLogger()
// 		sessionID, _ := logger.LogDownloadSessionStart(username, filename, clientID, tmpPath)

// 		dctx, dcancel := context.WithTimeout(cRequestContext(c), 5*time.Minute)
// 		defer dcancel()

// 		// Use requestStart for full latency including listing validation overhead
// 		opCtxDl2 := NewWebOpCtxAt("download", filename, username, 0, requestStart)

// 		if err := cli.DownloadFile(dctx, clientID, filename, tmpPath, username); err != nil {
// 			// Download failed
// 			_ = logger.LogDownloadFailed(sessionID, err.Error())
// 			RecordWebMetrics(opCtxDl2.Finalise(err.Error()))
// 			// Clean up incomplete download
// 			if _, statErr := os.Stat(tmpPath); statErr == nil {
// 				if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
// 					_ = logger.LogIncompleteDownloadCleanup(sessionID, tmpPath, sizeFreed)
// 				}
// 			}
// 			os.Remove(tmpPath)

// 			if err.Error() == "file not found" {
// 				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 				return
// 			}
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
// 		c.File(tmpPath)

// 		// Log download complete after file is served
// 		_ = logger.LogDownloadComplete(sessionID)

// 		// Set file size and record metrics (timer started before DownloadFile)
// 		if info, err2 := os.Stat(tmpPath); err2 == nil {
// 			opCtxDl2.SetFileSize(info.Size())
// 		}
// 		RecordWebMetrics(opCtxDl2.Finalise(""))

// 		// Clean up download file after response is sent
// 		defer func() {
// 			if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
// 				_ = logger.LogDownloadCleanup(sessionID, tmpPath, sizeFreed)
// 			}
// 			os.Remove(tmpPath)
// 		}()
// 	})

// 	// Simplified delete endpoint that uses authentication
// 	r.DELETE("/files/delete/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
// 		requestStart := time.Now()

// 		// Get authenticated user info
// 		username, userClientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		// User must have a clientID assigned
// 		if userClientID == 0 {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "no files to delete"})
// 			return
// 		}

// 		filename := c.Param("filename")
// 		clientID := int64(userClientID)

// 		dctx, dcancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
// 		defer dcancel()

// 		// Log delete initiation
// 		logger := webserver.GetWebServerLogger()
// 		_ = logger.LogFileDeleteInitiated(username, filename, clientID)

// 		// Use requestStart for full latency from request arrival
// 		opCtxDel := NewWebOpCtxAt("delete", filename, username, 0, requestStart)

// 		deleted, err := cli.DeleteFile(dctx, clientID, filename, username)
// 		if err != nil {
// 			// Map not found
// 			if err.Error() == "file not found" {
// 				_ = logger.LogFileDeleteFailed(username, filename, clientID, "file not found")
// 				RecordWebMetrics(opCtxDel.Finalise("file not found"))
// 				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
// 				return
// 			}
// 			_ = logger.LogFileDeleteFailed(username, filename, clientID, err.Error())
// 			RecordWebMetrics(opCtxDel.Finalise(err.Error()))
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Log successful deletion
// 		_ = logger.LogFileDeleteSuccess(username, filename, clientID, deleted)

// 		msg := "deleted"
// 		if deleted > 0 {
// 			msg = fmt.Sprintf("deleted %d chunks", deleted)
// 		}

// 		// Record metrics (timer started before DeleteFile call above)
// 		RecordWebMetrics(opCtxDel.Finalise(""))

// 		c.JSON(http.StatusOK, gin.H{"success": true, "message": msg})
// 	})

// 	// Chunked upload endpoints (require authentication)
// 	r.POST("/files/chunk", auth.AuthMiddleware(), handleChunkUpload)
// 	r.POST("/files/finalize", auth.AuthMiddleware(), handleFinalizeUpload(cli))
// 	r.GET("/files/status/:uploadId", auth.AuthMiddleware(), handleUploadStatus)

// 	// Metrics endpoints (require authentication)
// 	r.GET("/metrics", auth.AuthMiddleware(), func(c *gin.Context) { HandleGetMetrics(c.Writer, c.Request) })
// 	r.GET("/metrics/csv", auth.AuthMiddleware(), func(c *gin.Context) { HandleGetMetricsCSV(c.Writer, c.Request) })

// 	return r
// }

// // ChunkUploadStatus tracks the status of chunked uploads
// type ChunkUploadStatus struct {
// 	UploadId         string    `json:"uploadId"`
// 	Username         string    `json:"username"`
// 	TotalChunks      int       `json:"totalChunks"`
// 	UploadedChunks   []int     `json:"uploadedChunks"`
// 	Filename         string    `json:"filename"`
// 	TotalSize        int64     `json:"totalSize"`
// 	CreatedAt        time.Time `json:"createdAt"`
// 	MetricsStartTime time.Time `json:"-"` // captured on first chunk arrival for full e2e latency
// 	mu               sync.RWMutex
// }

// var (
// 	uploadStatuses = make(map[string]*ChunkUploadStatus)
// 	uploadStatusMu sync.RWMutex
// )

// // handleChunkUpload handles individual chunk uploads
// func handleChunkUpload(c *gin.Context) {
// 	// Capture arrival time of first chunk — used as metrics start for full e2e latency
// 	chunkArrival := time.Now()

// 	uploadId := c.PostForm("uploadId")
// 	chunkIndexStr := c.PostForm("chunkIndex")
// 	totalChunksStr := c.PostForm("totalChunks")
// 	filename := c.PostForm("filename")

// 	if uploadId == "" || chunkIndexStr == "" || totalChunksStr == "" {
// 		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "missing required fields"})
// 		return
// 	}

// 	chunkIndex, err := strconv.Atoi(chunkIndexStr)
// 	if err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid chunkIndex"})
// 		return
// 	}

// 	totalChunks, err := strconv.Atoi(totalChunksStr)
// 	if err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid totalChunks"})
// 		return
// 	}

// 	// Get chunk file
// 	chunkFile, err := c.FormFile("chunk")
// 	if err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "chunk file required"})
// 		return
// 	}

// 	// Get authenticated user for logging
// 	username, _, ok := auth.GetUserFromContext(c)
// 	if !ok {
// 		username = "unknown"
// 	}

// 	// Create upload directory
// 	uploadDir := filepath.Join(os.TempDir(), "dfs_uploads", uploadId)
// 	if err := os.MkdirAll(uploadDir, 0755); err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create upload directory"})
// 		return
// 	}

// 	// Save chunk
// 	chunkPath := filepath.Join(uploadDir, fmt.Sprintf("chunk_%d", chunkIndex))
// 	if err := c.SaveUploadedFile(chunkFile, chunkPath); err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to save chunk"})
// 		return
// 	}

// 	// Update upload status
// 	uploadStatusMu.Lock()
// 	status, exists := uploadStatuses[uploadId]
// 	if !exists {
// 		status = &ChunkUploadStatus{
// 			UploadId:         uploadId,
// 			Username:         username,
// 			TotalChunks:      totalChunks,
// 			UploadedChunks:   make([]int, 0),
// 			Filename:         filename,
// 			CreatedAt:        time.Now(),
// 			MetricsStartTime: chunkArrival, // first chunk arrival = true start of upload
// 		}
// 		uploadStatuses[uploadId] = status
// 		uploadStatusMu.Unlock()

// 		// Log upload start (only on first chunk)
// 		logger := webserver.GetWebServerLogger()
// 		_ = logger.LogChunkUploadStart(username, uploadId, filename, totalChunks, uploadDir)
// 	} else {
// 		uploadStatusMu.Unlock()
// 	}

// 	// Add chunk to uploaded list
// 	status.mu.Lock()
// 	// Check if chunk already uploaded
// 	found := false
// 	for _, uploaded := range status.UploadedChunks {
// 		if uploaded == chunkIndex {
// 			found = true
// 			break
// 		}
// 	}
// 	if !found {
// 		status.UploadedChunks = append(status.UploadedChunks, chunkIndex)
// 	}
// 	uploadedCount := len(status.UploadedChunks)
// 	status.mu.Unlock()

// 	// Log chunk stored
// 	logger := webserver.GetWebServerLogger()
// 	chunkSize := chunkFile.Size
// 	totalSize := int64(uploadedCount) * chunkSize // Approximate
// 	_ = logger.LogChunkStored(username, uploadId, chunkIndex, chunkSize, uploadedCount, totalChunks, totalSize, uploadDir)

// 	c.JSON(http.StatusOK, gin.H{
// 		"success":        true,
// 		"chunkIndex":     chunkIndex,
// 		"uploadedChunks": uploadedCount,
// 		"totalChunks":    totalChunks,
// 	})
// }

// // handleFinalizeUpload reassembles chunks and uploads to DFS
// func handleFinalizeUpload(cli dfsclient.Client) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		// Get authenticated user info (clientID is always non-zero, set at registration)
// 		username, clientID, ok := auth.GetUserFromContext(c)
// 		if !ok {
// 			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
// 			return
// 		}

// 		uploadId := c.PostForm("uploadId")
// 		filename := c.PostForm("filename")
// 		totalSizeStr := c.PostForm("totalSize")

// 		if uploadId == "" || filename == "" {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "uploadId and filename required"})
// 			return
// 		}

// 		totalSize, err := strconv.ParseInt(totalSizeStr, 10, 64)
// 		if err != nil {
// 			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid totalSize"})
// 			return
// 		}

// 		// Get upload status
// 		uploadStatusMu.RLock()
// 		status, exists := uploadStatuses[uploadId]
// 		uploadStatusMu.RUnlock()

// 		if !exists {
// 			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "upload not found"})
// 			return
// 		}

// 		// Check if all chunks are uploaded
// 		status.mu.RLock()
// 		if len(status.UploadedChunks) != status.TotalChunks {
// 			status.mu.RUnlock()
// 			c.JSON(http.StatusBadRequest, gin.H{
// 				"success": false,
// 				"message": fmt.Sprintf("incomplete upload: %d/%d chunks", len(status.UploadedChunks), status.TotalChunks),
// 			})
// 			return
// 		}
// 		status.mu.RUnlock()

// 		uploadDir := filepath.Join(os.TempDir(), "dfs_uploads", uploadId)

// 		// Log: all chunks uploaded, about to reassemble
// 		logger := webserver.GetWebServerLogger()
// 		_ = logger.LogChunkUploadComplete(username, uploadId, filename, status.TotalChunks, totalSize, uploadDir)

// 		// Reassemble file
// 		assembledFile := filepath.Join(uploadDir, "assembled_file")

// 		// Log: reassembly start
// 		_ = logger.LogChunkReassemblyStart(username, uploadId, uploadDir)

// 		reassemblyStart := time.Now()
// 		if err := reassembleChunks(uploadDir, assembledFile, status.TotalChunks); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to reassemble file: " + err.Error()})
// 			return
// 		}
// 		reassemblyMs := time.Since(reassemblyStart).Milliseconds()

// 		// Log: reassembly complete
// 		_ = logger.LogChunkReassemblyComplete(username, uploadId, totalSize, reassemblyMs)

// 		// Open assembled file for upload to DFS
// 		file, err := os.Open(assembledFile)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to open assembled file"})
// 			return
// 		}
// 		defer file.Close()

// 		// Upload to DFS with timeout
// 		uctx, cancel := context.WithTimeout(cRequestContext(c), 15*time.Minute) // Extended timeout for large files
// 		defer cancel()

// 		// cleanupUpload removes both the temp directory and the status map entry.
// 		cleanupUpload := func() {
// 			// Calculate size freed before deleting
// 			sizeFreed := int64(0)
// 			filepath.Walk(uploadDir, func(path string, info os.FileInfo, err error) error {
// 				if err == nil && !info.IsDir() {
// 					sizeFreed += info.Size()
// 				}
// 				return nil
// 			})

// 			os.RemoveAll(uploadDir)
// 			uploadStatusMu.Lock()
// 			delete(uploadStatuses, uploadId)
// 			uploadStatusMu.Unlock()

// 			// Log cleanup
// 			logger := webserver.GetWebServerLogger()
// 			_ = logger.LogChunkUploadCleanup(username, uploadId, uploadDir, sizeFreed)
// 		}

// 		// Use MetricsStartTime from first chunk arrival for true browser→chunkserver latency
// 		// This captures: browser→webserver chunk transfers + reassembly + DFS upload
// 		opCtxFin := NewWebOpCtxAt("upload", filename, username, totalSize, status.MetricsStartTime)

// 		assignedClient, err := cli.UploadFile(uctx, int64(clientID), filename, file, totalSize, username)
// 		if err != nil {
// 			RecordWebMetrics(opCtxFin.Finalise(err.Error()))
// 			// Also clean up on failure — previously this was skipped, leaking /tmp
// 			go func() {
// 				time.Sleep(1 * time.Minute)
// 				cleanupUpload()
// 			}()
// 			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
// 			return
// 		}

// 		// Cleanup after success
// 		go func() {
// 			time.Sleep(1 * time.Minute) // Brief delay so the file handle is fully released
// 			cleanupUpload()
// 		}()

// 		// Record metrics (timer started before UploadFile call above)
// 		RecordWebMetrics(opCtxFin.Finalise(""))

// 		c.JSON(http.StatusCreated, gin.H{
// 			"success":  true,
// 			"clientId": assignedClient,
// 			"filename": filename,
// 			"method":   "chunked",
// 		})
// 	}
// }

// // handleUploadStatus returns the status of a chunked upload
// func handleUploadStatus(c *gin.Context) {
// 	uploadId := c.Param("uploadId")

// 	uploadStatusMu.RLock()
// 	status, exists := uploadStatuses[uploadId]
// 	uploadStatusMu.RUnlock()

// 	if !exists {
// 		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "upload not found"})
// 		return
// 	}

// 	status.mu.RLock()
// 	response := gin.H{
// 		"success":        true,
// 		"uploadId":       status.UploadId,
// 		"totalChunks":    status.TotalChunks,
// 		"uploadedChunks": status.UploadedChunks,
// 		"filename":       status.Filename,
// 		"progress":       float64(len(status.UploadedChunks)) / float64(status.TotalChunks) * 100,
// 		"createdAt":      status.CreatedAt,
// 	}
// 	status.mu.RUnlock()

// 	c.JSON(http.StatusOK, response)
// }

// // reassembleChunks combines individual chunks into a single file
// func reassembleChunks(uploadDir, outputPath string, totalChunks int) error {
// 	outputFile, err := os.Create(outputPath)
// 	if err != nil {
// 		return fmt.Errorf("failed to create output file: %v", err)
// 	}
// 	defer outputFile.Close()

// 	// Read chunks in order and append to output file
// 	for i := 0; i < totalChunks; i++ {
// 		chunkPath := filepath.Join(uploadDir, fmt.Sprintf("chunk_%d", i))

// 		chunkFile, err := os.Open(chunkPath)
// 		if err != nil {
// 			return fmt.Errorf("failed to open chunk %d: %v", i, err)
// 		}

// 		_, err = io.Copy(outputFile, chunkFile)
// 		chunkFile.Close()

// 		if err != nil {
// 			return fmt.Errorf("failed to copy chunk %d: %v", i, err)
// 		}
// 	}

// 	return nil
// }

// func cRequestContext(c *gin.Context) context.Context { return c.Request.Context() }

// // getFileSize returns the size of a file, or 0 if file doesn't exist
// func getFileSize(path string) (int64, error) {
// 	info, err := os.Stat(path)
// 	if err != nil {
// 		return 0, err
// 	}
// 	return info.Size(), nil
// }

// // startUploadSweeper runs in a background goroutine and periodically evicts
// // abandoned chunked uploads — those where /files/chunk was called but
// // /files/finalize was never called (browser closed, network dropped, etc.).
// //
// // sweepInterval: how often to scan (e.g. every 5 minutes)
// // maxAge:        how old an upload must be before it is considered abandoned (e.g. 10 minutes)
// func startUploadSweeper(sweepInterval, maxAge time.Duration) {
// 	ticker := time.NewTicker(sweepInterval)
// 	defer ticker.Stop()

// 	for range ticker.C {
// 		now := time.Now()

// 		// Collect expired upload IDs under a short read lock.
// 		uploadStatusMu.RLock()
// 		var expired []string
// 		for id, status := range uploadStatuses {
// 			if now.Sub(status.CreatedAt) > maxAge {
// 				expired = append(expired, id)
// 			}
// 		}
// 		uploadStatusMu.RUnlock()

// 		if len(expired) == 0 {
// 			continue
// 		}

// 		// Remove each expired entry from the map and from disk.
// 		uploadStatusMu.Lock()
// 		uploadsDeleted := 0
// 		var totalSizeFreed int64 = 0
// 		var deletedIds []string

// 		logger := webserver.GetWebServerLogger()

// 		for _, id := range expired {
// 			status := uploadStatuses[id]
// 			delete(uploadStatuses, id)
// 			uploadDir := filepath.Join(os.TempDir(), "dfs_uploads", id)

// 			// Calculate size freed before deleting
// 			sizeFreed := int64(0)
// 			filepath.Walk(uploadDir, func(path string, info os.FileInfo, err error) error {
// 				if err == nil && !info.IsDir() {
// 					sizeFreed += info.Size()
// 				}
// 				return nil
// 			})

// 			_ = os.RemoveAll(uploadDir)

// 			// Log abandoned upload cleanup to cleanup log
// 			_ = logger.LogAbandonedUploadCleanup(id, uploadDir, sizeFreed)

// 			// Log abandoned upload to user's personal log file
// 			_ = logger.LogChunkUploadAbandoned(status.Username, id, uploadDir, sizeFreed)

// 			uploadsDeleted++
// 			totalSizeFreed += sizeFreed
// 			deletedIds = append(deletedIds, id)
// 		}
// 		uploadStatusMu.Unlock()

// 		// Log sweep completion summary
// 		sweepSummary := webserver.UploadSweepSummary{
// 			SweepTime:        time.Now(),
// 			UploadsChecked:   len(uploadStatuses),
// 			UploadsDeleted:   uploadsDeleted,
// 			TotalSizeFreed:   totalSizeFreed,
// 			AbandonedUploads: deletedIds,
// 		}
// 		_ = logger.LogUploadSweepComplete(sweepSummary)
// 	}
// }

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
	"dfs-project/pkg/webserver"

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

	// Initialize webserver logger (creates webserver_logs/ directory)
	_ = webserver.GetWebServerLogger()

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

	// Start background goroutine that cleans up abandoned uploads.
	// Runs every 5 minutes and removes uploads older than 10 minutes.
	go startUploadSweeper(5*time.Minute, 10*time.Minute)

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
		// Capture request arrival time immediately — includes HTTP body receive time
		requestStart := time.Now()

		// Get authenticated user info (clientID is always non-zero, set at registration)
		username, clientID, ok := auth.GetUserFromContext(c)
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

		// Log simple upload start
		logger := webserver.GetWebServerLogger()
		_ = logger.LogSimpleUploadStart(username, filename, size)

		// Use a request-scoped timeout for uploads
		uctx, cancel := context.WithTimeout(cRequestContext(c), 10*time.Minute)
		defer cancel()

		// Use requestStart captured at handler entry for full browser→chunkserver latency
		opCtx := NewWebOpCtxAt("upload", filename, username, size, requestStart)

		assignedClient, uploadStats, err := cli.UploadFile(uctx, int64(clientID), filename, f, size, username)
		if err != nil {
			// Log upload failure
			_ = logger.LogSimpleUploadFailed(username, filename, size, err.Error())
			RecordWebMetrics(opCtx.Finalise(err.Error()))
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

		// Log successful upload
		_ = logger.LogSimpleUploadComplete(username, filename, size)

		// Apply DFS-level stats from inside cli.UploadFile
		opCtx.AddMasterRPC(uploadStats.MasterRPCMs)
		opCtx.AddDataXfer(uploadStats.DataTransferMs)
		opCtx.AddParity(uploadStats.ParityComputeMs)
		opCtx.AddStripes(uploadStats.StripeCount)
		for i := 0; i < uploadStats.ChunksAttempted; i++ {
			opCtx.AddChunkResult(i < uploadStats.ChunksSucceeded, false)
		}
		RecordWebMetrics(opCtx.Finalise(""))

		c.JSON(http.StatusCreated, gin.H{"success": true, "clientId": assignedClient, "filename": filename})
	})

	r.GET("/files", auth.AuthMiddleware(), func(c *gin.Context) {
		requestStart := time.Now()

		// Get authenticated user info
		username, clientID, ok := auth.GetUserFromContext(c)
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

		// Use requestStart for full latency including auth overhead
		opCtxLs := NewWebOpCtxAt("ls", "/", username, 0, requestStart)

		files, err := cli.ListFiles(lctx, int64(clientID))
		if err != nil {
			RecordWebMetrics(opCtxLs.Finalise(err.Error()))
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}

		// Log file refresh
		if username != "" {
			logger := dfsclient.GetUserLogger()
			_ = logger.LogFileRefresh(username, len(files))
		}

		// Record metrics (timer started before ListFiles call above)
		RecordWebMetrics(opCtxLs.Finalise(""))

		c.JSON(http.StatusOK, gin.H{"filenames": files})
	})

	r.GET("/files/:clientId/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
		requestStart := time.Now()

		// Get authenticated user info
		username, userClientID, ok := auth.GetUserFromContext(c)
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

		downloadDir := filepath.Join(os.TempDir(), "dfs_downloads")
		if err := os.MkdirAll(downloadDir, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create download directory"})
			return
		}
		tmpPath := filepath.Join(downloadDir, filename)
		_ = os.Remove(tmpPath)

		// Initialize download session logging
		logger := webserver.GetWebServerLogger()
		sessionID, _ := logger.LogDownloadSessionStart(username, filename, requestedClientID, tmpPath)

		dctx, dcancel := context.WithTimeout(cRequestContext(c), 5*time.Minute)
		defer dcancel()

		// Use requestStart for full latency including listing validation overhead
		opCtxDl := NewWebOpCtxAt("download", filename, username, 0, requestStart)

		dlStats, dlErr := cli.DownloadFile(dctx, requestedClientID, filename, tmpPath, username)
		if dlErr != nil {
			err = dlErr
			// Download failed
			_ = logger.LogDownloadFailed(sessionID, err.Error())
			RecordWebMetrics(opCtxDl.Finalise(err.Error()))
			// Clean up incomplete download
			if _, statErr := os.Stat(tmpPath); statErr == nil {
				if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
					_ = logger.LogIncompleteDownloadCleanup(sessionID, tmpPath, sizeFreed)
				}
			}
			os.Remove(tmpPath)

			if err.Error() == "file not found" {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}

		// Set file size and apply DFS-level stats from inside cli.DownloadFile
		if info, err2 := os.Stat(tmpPath); err2 == nil {
			opCtxDl.SetFileSize(info.Size())
		}
		opCtxDl.AddMasterRPC(dlStats.MasterRPCMs)
		opCtxDl.AddDataXfer(dlStats.DataTransferMs)
		opCtxDl.AddStripes(dlStats.StripeCount)
		opCtxDl.AddReconstruction(dlStats.ReconstructionMs)
		for i := 0; i < dlStats.ChunksAttempted; i++ {
			opCtxDl.AddChunkResult(i < dlStats.ChunksSucceeded, i < dlStats.ChunksReconstructed)
		}

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		c.File(tmpPath)

		// Log download complete and record metrics
		_ = logger.LogDownloadComplete(sessionID)
		RecordWebMetrics(opCtxDl.Finalise(""))

		// Clean up download file after response is sent
		defer func() {
			if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
				_ = logger.LogDownloadCleanup(sessionID, tmpPath, sizeFreed)
			}
			os.Remove(tmpPath)
		}()
	})

	r.DELETE("/files/:clientId/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
		// Get authenticated user info
		username, userClientID, ok := auth.GetUserFromContext(c)
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

		// Log delete initiation
		logger := webserver.GetWebServerLogger()
		_ = logger.LogFileDeleteInitiated(username, filename, requestedClientID)

		deleted, err := cli.DeleteFile(dctx, requestedClientID, filename, username)
		if err != nil {
			// Map not found
			if err.Error() == "file not found" {
				_ = logger.LogFileDeleteFailed(username, filename, requestedClientID, "file not found")
				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
				return
			}
			_ = logger.LogFileDeleteFailed(username, filename, requestedClientID, err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}

		// Log successful deletion
		_ = logger.LogFileDeleteSuccess(username, filename, requestedClientID, deleted)

		msg := "deleted"
		if deleted > 0 {
			msg = fmt.Sprintf("deleted %d chunks", deleted)
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "message": msg})
	})

	// Simplified download endpoint that uses authentication
	r.GET("/files/download/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
		requestStart := time.Now()

		// Get authenticated user info
		username, userClientID, ok := auth.GetUserFromContext(c)
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

		downloadDir := filepath.Join(os.TempDir(), "dfs_downloads")
		if err := os.MkdirAll(downloadDir, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create download directory"})
			return
		}
		tmpPath := filepath.Join(downloadDir, filename)
		_ = os.Remove(tmpPath)

		// Initialize download session logging
		logger := webserver.GetWebServerLogger()
		sessionID, _ := logger.LogDownloadSessionStart(username, filename, clientID, tmpPath)

		dctx, dcancel := context.WithTimeout(cRequestContext(c), 5*time.Minute)
		defer dcancel()

		// Use requestStart for full latency including listing validation overhead
		opCtxDl2 := NewWebOpCtxAt("download", filename, username, 0, requestStart)

		dlStats2, dlErr2 := cli.DownloadFile(dctx, clientID, filename, tmpPath, username)
		if dlErr2 != nil {
			// Download failed
			_ = logger.LogDownloadFailed(sessionID, dlErr2.Error())
			RecordWebMetrics(opCtxDl2.Finalise(dlErr2.Error()))
			// Clean up incomplete download
			if _, statErr := os.Stat(tmpPath); statErr == nil {
				if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
					_ = logger.LogIncompleteDownloadCleanup(sessionID, tmpPath, sizeFreed)
				}
			}
			os.Remove(tmpPath)

			if err.Error() == "file not found" {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		c.File(tmpPath)

		// Log download complete after file is served
		_ = logger.LogDownloadComplete(sessionID)

		// Set file size and apply DFS-level stats
		if info, err2 := os.Stat(tmpPath); err2 == nil {
			opCtxDl2.SetFileSize(info.Size())
		}
		opCtxDl2.AddMasterRPC(dlStats2.MasterRPCMs)
		opCtxDl2.AddDataXfer(dlStats2.DataTransferMs)
		opCtxDl2.AddStripes(dlStats2.StripeCount)
		opCtxDl2.AddReconstruction(dlStats2.ReconstructionMs)
		for i := 0; i < dlStats2.ChunksAttempted; i++ {
			opCtxDl2.AddChunkResult(i < dlStats2.ChunksSucceeded, i < dlStats2.ChunksReconstructed)
		}

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		c.File(tmpPath)

		// Log download complete and record metrics
		_ = logger.LogDownloadComplete(sessionID)
		RecordWebMetrics(opCtxDl2.Finalise(""))

		// Clean up download file after response is sent
		defer func() {
			if sizeFreed, _ := getFileSize(tmpPath); sizeFreed > 0 {
				_ = logger.LogDownloadCleanup(sessionID, tmpPath, sizeFreed)
			}
			os.Remove(tmpPath)
		}()
	})

	// Simplified delete endpoint that uses authentication
	r.DELETE("/files/delete/:filename", auth.AuthMiddleware(), func(c *gin.Context) {
		requestStart := time.Now()

		// Get authenticated user info
		username, userClientID, ok := auth.GetUserFromContext(c)
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

		// Log delete initiation
		logger := webserver.GetWebServerLogger()
		_ = logger.LogFileDeleteInitiated(username, filename, clientID)

		// Use requestStart for full latency from request arrival
		opCtxDel := NewWebOpCtxAt("delete", filename, username, 0, requestStart)

		deleted, err := cli.DeleteFile(dctx, clientID, filename, username)
		if err != nil {
			// Map not found
			if err.Error() == "file not found" {
				_ = logger.LogFileDeleteFailed(username, filename, clientID, "file not found")
				RecordWebMetrics(opCtxDel.Finalise("file not found"))
				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "file not found"})
				return
			}
			_ = logger.LogFileDeleteFailed(username, filename, clientID, err.Error())
			RecordWebMetrics(opCtxDel.Finalise(err.Error()))
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}

		// Log successful deletion
		_ = logger.LogFileDeleteSuccess(username, filename, clientID, deleted)

		msg := "deleted"
		if deleted > 0 {
			msg = fmt.Sprintf("deleted %d chunks", deleted)
		}

		// Record metrics (timer started before DeleteFile call above)
		RecordWebMetrics(opCtxDel.Finalise(""))

		c.JSON(http.StatusOK, gin.H{"success": true, "message": msg})
	})

	// Chunked upload endpoints (require authentication)
	r.POST("/files/chunk", auth.AuthMiddleware(), handleChunkUpload)
	r.POST("/files/finalize", auth.AuthMiddleware(), handleFinalizeUpload(cli))
	r.GET("/files/status/:uploadId", auth.AuthMiddleware(), handleUploadStatus)

	// Metrics endpoints (require authentication)
	r.GET("/metrics", auth.AuthMiddleware(), func(c *gin.Context) { HandleGetMetrics(c.Writer, c.Request) })
	r.GET("/metrics/csv", auth.AuthMiddleware(), func(c *gin.Context) { HandleGetMetricsCSV(c.Writer, c.Request) })

	return r
}

// ChunkUploadStatus tracks the status of chunked uploads
type ChunkUploadStatus struct {
	UploadId         string    `json:"uploadId"`
	Username         string    `json:"username"`
	TotalChunks      int       `json:"totalChunks"`
	UploadedChunks   []int     `json:"uploadedChunks"`
	Filename         string    `json:"filename"`
	TotalSize        int64     `json:"totalSize"`
	CreatedAt        time.Time `json:"createdAt"`
	MetricsStartTime time.Time `json:"-"` // captured on first chunk arrival for full e2e latency
	mu               sync.RWMutex
}

var (
	uploadStatuses = make(map[string]*ChunkUploadStatus)
	uploadStatusMu sync.RWMutex
)

// handleChunkUpload handles individual chunk uploads
func handleChunkUpload(c *gin.Context) {
	// Capture arrival time of first chunk — used as metrics start for full e2e latency
	chunkArrival := time.Now()

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

	// Get authenticated user for logging
	username, _, ok := auth.GetUserFromContext(c)
	if !ok {
		username = "unknown"
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
			UploadId:         uploadId,
			Username:         username,
			TotalChunks:      totalChunks,
			UploadedChunks:   make([]int, 0),
			Filename:         filename,
			CreatedAt:        time.Now(),
			MetricsStartTime: chunkArrival, // first chunk arrival = true start of upload
		}
		uploadStatuses[uploadId] = status
		uploadStatusMu.Unlock()

		// Log upload start (only on first chunk)
		logger := webserver.GetWebServerLogger()
		_ = logger.LogChunkUploadStart(username, uploadId, filename, totalChunks, uploadDir)
	} else {
		uploadStatusMu.Unlock()
	}

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
	uploadedCount := len(status.UploadedChunks)
	status.mu.Unlock()

	// Log chunk stored
	logger := webserver.GetWebServerLogger()
	chunkSize := chunkFile.Size
	totalSize := int64(uploadedCount) * chunkSize // Approximate
	_ = logger.LogChunkStored(username, uploadId, chunkIndex, chunkSize, uploadedCount, totalChunks, totalSize, uploadDir)

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"chunkIndex":     chunkIndex,
		"uploadedChunks": uploadedCount,
		"totalChunks":    totalChunks,
	})
}

// handleFinalizeUpload reassembles chunks and uploads to DFS
func handleFinalizeUpload(cli dfsclient.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get authenticated user info (clientID is always non-zero, set at registration)
		username, clientID, ok := auth.GetUserFromContext(c)
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

		uploadDir := filepath.Join(os.TempDir(), "dfs_uploads", uploadId)

		// Log: all chunks uploaded, about to reassemble
		logger := webserver.GetWebServerLogger()
		_ = logger.LogChunkUploadComplete(username, uploadId, filename, status.TotalChunks, totalSize, uploadDir)

		// Reassemble file
		assembledFile := filepath.Join(uploadDir, "assembled_file")

		// Log: reassembly start
		_ = logger.LogChunkReassemblyStart(username, uploadId, uploadDir)

		reassemblyStart := time.Now()
		if err := reassembleChunks(uploadDir, assembledFile, status.TotalChunks); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to reassemble file: " + err.Error()})
			return
		}
		reassemblyMs := time.Since(reassemblyStart).Milliseconds()

		// Log: reassembly complete
		_ = logger.LogChunkReassemblyComplete(username, uploadId, totalSize, reassemblyMs)

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

		// cleanupUpload removes both the temp directory and the status map entry.
		cleanupUpload := func() {
			// Calculate size freed before deleting
			sizeFreed := int64(0)
			filepath.Walk(uploadDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					sizeFreed += info.Size()
				}
				return nil
			})

			os.RemoveAll(uploadDir)
			uploadStatusMu.Lock()
			delete(uploadStatuses, uploadId)
			uploadStatusMu.Unlock()

			// Log cleanup
			logger := webserver.GetWebServerLogger()
			_ = logger.LogChunkUploadCleanup(username, uploadId, uploadDir, sizeFreed)
		}

		// Use MetricsStartTime from first chunk arrival for true browser→chunkserver latency
		// This captures: browser→webserver chunk transfers + reassembly + DFS upload
		opCtxFin := NewWebOpCtxAt("upload", filename, username, totalSize, status.MetricsStartTime)

		assignedClient, finStats, err := cli.UploadFile(uctx, int64(clientID), filename, file, totalSize, username)
		if err != nil {
			RecordWebMetrics(opCtxFin.Finalise(err.Error()))
			// Also clean up on failure — previously this was skipped, leaking /tmp
			go func() {
				time.Sleep(1 * time.Minute)
				cleanupUpload()
			}()
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}

		// Cleanup after success
		go func() {
			time.Sleep(1 * time.Minute) // Brief delay so the file handle is fully released
			cleanupUpload()
		}()

		// Apply DFS-level stats and record metrics
		opCtxFin.AddMasterRPC(finStats.MasterRPCMs)
		opCtxFin.AddDataXfer(finStats.DataTransferMs)
		opCtxFin.AddParity(finStats.ParityComputeMs)
		opCtxFin.AddStripes(finStats.StripeCount)
		for i := 0; i < finStats.ChunksAttempted; i++ {
			opCtxFin.AddChunkResult(i < finStats.ChunksSucceeded, false)
		}
		RecordWebMetrics(opCtxFin.Finalise(""))

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

// getFileSize returns the size of a file, or 0 if file doesn't exist
func getFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// startUploadSweeper runs in a background goroutine and periodically evicts
// abandoned chunked uploads — those where /files/chunk was called but
// /files/finalize was never called (browser closed, network dropped, etc.).
//
// sweepInterval: how often to scan (e.g. every 5 minutes)
// maxAge:        how old an upload must be before it is considered abandoned (e.g. 10 minutes)
func startUploadSweeper(sweepInterval, maxAge time.Duration) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()

		// Collect expired upload IDs under a short read lock.
		uploadStatusMu.RLock()
		var expired []string
		for id, status := range uploadStatuses {
			if now.Sub(status.CreatedAt) > maxAge {
				expired = append(expired, id)
			}
		}
		uploadStatusMu.RUnlock()

		if len(expired) == 0 {
			continue
		}

		// Remove each expired entry from the map and from disk.
		uploadStatusMu.Lock()
		uploadsDeleted := 0
		var totalSizeFreed int64 = 0
		var deletedIds []string

		logger := webserver.GetWebServerLogger()

		for _, id := range expired {
			status := uploadStatuses[id]
			delete(uploadStatuses, id)
			uploadDir := filepath.Join(os.TempDir(), "dfs_uploads", id)

			// Calculate size freed before deleting
			sizeFreed := int64(0)
			filepath.Walk(uploadDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					sizeFreed += info.Size()
				}
				return nil
			})

			_ = os.RemoveAll(uploadDir)

			// Log abandoned upload cleanup to cleanup log
			_ = logger.LogAbandonedUploadCleanup(id, uploadDir, sizeFreed)

			// Log abandoned upload to user's personal log file
			_ = logger.LogChunkUploadAbandoned(status.Username, id, uploadDir, sizeFreed)

			uploadsDeleted++
			totalSizeFreed += sizeFreed
			deletedIds = append(deletedIds, id)
		}
		uploadStatusMu.Unlock()

		// Log sweep completion summary
		sweepSummary := webserver.UploadSweepSummary{
			SweepTime:        time.Now(),
			UploadsChecked:   len(uploadStatuses),
			UploadsDeleted:   uploadsDeleted,
			TotalSizeFreed:   totalSizeFreed,
			AbandonedUploads: deletedIds,
		}
		_ = logger.LogUploadSweepComplete(sweepSummary)
	}
}
