package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

	// Listen on port from cluster.conf (or env WEB_API_PORT, default 8080)
	port := config.GetWebAPIPort()
	_ = r.Run("0.0.0.0:" + port)
}

// NewRouter constructs the Gin engine with handlers using provided DFS client
func NewRouter(cli dfsclient.Client) *gin.Engine {
	r := gin.Default()

	// Simple CORS for dev: allow requests from frontend dev server
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-DFS-Password")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	r.POST("/auth", func(c *gin.Context) {
		var req struct {
			Username   string `json:"username"`
			Password   string `json:"password"`
			IsRegister bool   `json:"is_register"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid request body"})
			return
		}
		if req.Username == "" || req.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Username and password required"})
			return
		}
		success, msg, err := cli.Authenticate(cRequestContext(c), req.Username, req.Password, req.IsRegister)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": success, "message": msg})
	})

	r.POST("/files", func(c *gin.Context) {
		// Request validation
		username := c.PostForm("username")
		password := c.GetHeader("X-DFS-Password")
		if username == "" || password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "username and password required"})
			return
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

		assignedUser, err := cli.UploadFile(uctx, username, password, filename, f, size)
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
		files, err := cli.ListFiles(cRequestContext(c), assignedUser, password)
		if err != nil {
			c.JSON(http.StatusCreated, gin.H{"success": true, "username": assignedUser, "filename": filename, "warning": "uploaded but verification failed"})
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
			c.JSON(http.StatusCreated, gin.H{"success": true, "username": assignedUser, "filename": filename, "warning": "uploaded but file not listed yet"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"success": true, "username": assignedUser, "filename": filename})
	})

	r.GET("/files", func(c *gin.Context) {
		username := c.Query("username")
		if username == "" {
			username = c.Query("user")
		}
		password := c.GetHeader("X-DFS-Password")
		if username == "" || password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "username and password required"})
			return
		}

		lctx, cancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
		defer cancel()

		files, err := cli.ListFiles(lctx, username, password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"filenames": files})
	})

	r.GET("/files/:username/:filename", func(c *gin.Context) {
		username := c.Param("username")
		filename := c.Param("filename")
		password := c.GetHeader("X-DFS-Password")

		lctx, lcancel := context.WithTimeout(cRequestContext(c), 15*time.Second)
		defer lcancel()
		files, err := cli.ListFiles(lctx, username, password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
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

		tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("download_%s_%s", username, filename))
		dctx, dcancel := context.WithTimeout(cRequestContext(c), 5*time.Minute)
		defer dcancel()

		if err := cli.DownloadFile(dctx, username, password, filename, tmpPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		c.File(tmpPath)
		go func() {
			time.Sleep(2 * time.Second)
			_ = os.Remove(tmpPath)
		}()
	})

	r.DELETE("/files/:username/:filename", func(c *gin.Context) {
		username := c.Param("username")
		filename := c.Param("filename")
		password := c.GetHeader("X-DFS-Password")
		dctx, dcancel := context.WithTimeout(cRequestContext(c), 30*time.Second)
		defer dcancel()

		deleted, err := cli.DeleteFile(dctx, username, password, filename)
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
