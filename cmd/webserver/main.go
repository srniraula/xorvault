package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"dfs-project/pkg/config"
	"dfs-project/pkg/dfsclient"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	// Create a DFS client (gRPC)
	cli, err := dfsclient.NewGrpcClient(config.GetMasterAddr())
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	r = NewRouter(cli)

	// Start server
	_ = r.Run(":8080")
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

	r.POST("/files", func(c *gin.Context) {
		// Request validation
		clientIDStr := c.PostForm("clientId")
		clientID := int64(0)
		if clientIDStr != "" {
			id, err := strconv.ParseInt(clientIDStr, 10, 64)
			if err != nil || id < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid clientId"})
				return
			}
			clientID = id
		}

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

		assignedClient, err := cli.UploadFile(uctx, clientID, filename, f, size)
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

	r.GET("/files", func(c *gin.Context) {
		// Accept multiple query param spellings for client id (case-insensitive)
		clientIDStr := c.Query("clientId")
		if clientIDStr == "" {
			clientIDStr = c.Query("clientid")
		}
		if clientIDStr == "" {
			clientIDStr = c.Query("client_id")
		}
		if clientIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "clientId required"})
			return
		}
		id, err := strconv.ParseInt(clientIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid clientId"})
			return
		}
		// add timeout
		lctx, cancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
		defer cancel()

		files, err := cli.ListFiles(lctx, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"filenames": files})
	})

	r.GET("/files/:clientId/:filename", func(c *gin.Context) {
		clientIDStr := c.Param("clientId")
		id, err := strconv.ParseInt(clientIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid clientId"})
			return
		}
		filename := c.Param("filename")
		// Sanity check: verify file exists in listing
		lctx, lcancel := context.WithTimeout(cRequestContext(c), 15*time.Second)
		defer lcancel()
		files, err := cli.ListFiles(lctx, id)
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

		tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("download_%d_%s", id, filename))
		_ = os.Remove(tmpPath)

		dctx, dcancel := context.WithTimeout(cRequestContext(c), 5*time.Minute)
		defer dcancel()
		if err := cli.DownloadFile(dctx, id, filename, tmpPath); err != nil {
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

	r.DELETE("/files/:clientId/:filename", func(c *gin.Context) {
		clientIDStr := c.Param("clientId")
		id, err := strconv.ParseInt(clientIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid clientId"})
			return
		}
		filename := c.Param("filename")
		dctx, dcancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
		defer dcancel()

		deleted, err := cli.DeleteFile(dctx, id, filename)
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

	return r
}

func cRequestContext(c *gin.Context) context.Context { return c.Request.Context() }
