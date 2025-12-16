package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"dfs-project/dfspb"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// handleUpload handles file upload via multipart form
func handleUpload(c *gin.Context) {
	// Get uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}
	defer file.Close()

	filename := header.Filename
	fileSize := header.Size

	log.Printf("Upload request: %s (%d bytes)", filename, fileSize)

	// Save to temporary file
	tempPath := filepath.Join(os.TempDir(), filename)
	tempFile, err := os.Create(tempPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}
	defer os.Remove(tempPath) // Cleanup

	_, err = io.Copy(tempFile, file)
	tempFile.Close()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write file"})
		return
	}

	// Upload to DFS
	err = uploadToDFS(tempPath, filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Upload failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"filename": filename,
		"size":     fileSize,
		"clientId": getClientID(),
		"message":  "File uploaded successfully",
	})
}

// handleDownload handles file download
func handleDownload(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Filename required"})
		return
	}

	log.Printf("Download request: %s", filename)

	// Download from DFS
	localPath, err := downloadFromDFS(filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Download failed: %v", err)})
		return
	}
	defer os.Remove(localPath) // Cleanup after sending

	// Send file
	c.File(localPath)
}

// handleDelete handles file deletion
func handleDelete(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Filename required"})
		return
	}

	log.Printf("Delete request: %s", filename)

	err := deleteFromDFS(filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Delete failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"filename": filename,
		"message":  "File deleted successfully",
	})
}

// handleListFiles lists all files for this client
func handleListFiles(c *gin.Context) {
	clientID := getClientID()
	if clientID == 0 {
		c.JSON(http.StatusOK, gin.H{"files": []string{}, "clientId": 0})
		return
	}

	files, err := listFilesFromDFS()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to list files: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"clientId": clientID,
		"files":    files,
		"count":    len(files),
	})
}

// handleSystemStatus returns master and chunk server status
func handleSystemStatus(c *gin.Context) {
	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot connect to master"})
		return
	}
	defer conn.Close()

	// For now, return basic status
	// In production, you'd query master for actual chunk server status
	status := gin.H{
		"master": gin.H{
			"address": masterAddr,
			"status":  "online",
		},
		"chunkServers": []gin.H{
			{"id": 1, "address": "192.168.1.65:9001", "status": "online"},
			{"id": 2, "address": "192.168.1.65:9002", "status": "online"},
			{"id": 3, "address": "192.168.1.65:9003", "status": "online"},
		},
	}

	c.JSON(http.StatusOK, status)
}

// handleFileChunks returns chunk location information for a file
func handleFileChunks(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Filename required"})
		return
	}

	clientID := getClientID()
	if clientID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No client ID"})
		return
	}

	// Connect to master
	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot connect to master"})
		return
	}
	defer conn.Close()

	master := dfspb.NewMasterServerClient(conn)

	// Get file metadata
	meta, err := master.GetFileMetadata(context.Background(), &dfspb.GetFileMetadataRequest{
		Filename: filename,
		ClientId: clientID,
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found or access denied"})
		return
	}

	// Format stripe information
	stripes := make([]gin.H, 0)
	for stripeNum, stripe := range meta.Stripes {
		stripes = append(stripes, gin.H{
			"stripeNumber": stripeNum,
			"chunks": []gin.H{
				{"type": "data1", "id": stripe.ChunkIds[0], "server": stripe.Servers[0]},
				{"type": "data2", "id": stripe.ChunkIds[1], "server": stripe.Servers[1]},
				{"type": "parity", "id": stripe.ChunkIds[2], "server": stripe.Servers[2]},
			},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"filename": filename,
		"size":     meta.FileSize,
		"stripes":  stripes,
	})
}

// handleListClients lists all clients (requires master support)
func handleListClients(c *gin.Context) {
	clientID := getClientID()

	// Get file count for current client
	files, _ := listFilesFromDFS()
	fileCount := len(files)

	// This would require a new RPC method on master to get ALL clients
	// For now, return current client only
	c.JSON(http.StatusOK, gin.H{
		"clients": []gin.H{
			{"id": clientID, "fileCount": fileCount, "current": true},
		},
	})
}

// handleClientFiles lists files for a specific client
func handleClientFiles(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
		return
	}

	clientID := getClientID()
	if id != clientID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Can only view own files"})
		return
	}

	files, err := listFilesFromDFS()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list files"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"clientId": id,
		"files":    files,
	})
}
