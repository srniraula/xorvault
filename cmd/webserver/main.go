// package main

// import (
// 	"flag"
// 	"fmt"
// 	"log"
// 	"net/http"
// 	"os"

// 	"github.com/gin-gonic/gin"
// )

// var (
// 	masterAddr string
// 	clientID   int64
// )

// func main() {
// 	// Parse command line flags
// 	port := flag.String("port", "8080", "web server port")
// 	master := flag.String("master", "127.0.0.1:50051", "master server address")
// 	flag.Parse()

// 	masterAddr = *master
// 	log.Printf("Web server starting on :%s", *port)
// 	log.Printf("Connecting to master: %s", masterAddr)

// 	// Load or create client ID
// 	clientID = loadClientID()
// 	if clientID == 0 {
// 		log.Println("No client ID found - will be assigned on first upload")
// 	} else {
// 		log.Printf("Using client ID: %d", clientID)
// 	}

// 	// Set Gin to release mode for production
// 	gin.SetMode(gin.ReleaseMode)

// 	// Create router
// 	router := gin.Default()

// 	// Enable CORS for frontend
// 	router.Use(corsMiddleware())

// 	// API routes
// 	api := router.Group("/api")
// 	{
// 		// File operations
// 		api.POST("/upload", handleUpload)
// 		api.GET("/download/:filename", handleDownload)
// 		api.DELETE("/delete/:filename", handleDelete)
// 		api.GET("/files", handleListFiles)

// 		// System information
// 		api.GET("/system/status", handleSystemStatus)
// 		api.GET("/files/:filename/chunks", handleFileChunks)
// 		api.GET("/clients", handleListClients)
// 		api.GET("/client/:id/files", handleClientFiles)
// 	}

// 	// Health check
// 	router.GET("/health", func(c *gin.Context) {
// 		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
// 	})

// 	// Start server
// 	if err := router.Run(":" + *port); err != nil {
// 		log.Fatalf("Failed to start server: %v", err)
// 	}
// }

// // corsMiddleware adds CORS headers for frontend access
// func corsMiddleware() gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
// 		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
// 		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
// 		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

// 		if c.Request.Method == "OPTIONS" {
// 			c.AbortWithStatus(204)
// 			return
// 		}

// 		c.Next()
// 	}
// }

// // loadClientID reads client ID from file (same as CLI client)
// func loadClientID() int64 {
// 	data, err := os.ReadFile(".client_id")
// 	if err != nil {
// 		return 0
// 	}
// 	var id int64
// 	_, err = fmt.Sscanf(string(data), "%d", &id)
// 	if err != nil {
// 		return 0
// 	}
// 	return id
// }

// // saveClientID saves client ID to file (same as CLI client)
// func saveClientID(id int64) error {
// 	return os.WriteFile(".client_id", []byte(fmt.Sprintf("%d", id)), 0644)
// }

// package main

// import (
// 	"flag"
// 	"fmt"
// 	"log"
// 	"net/http"
// 	"os"

// 	"github.com/gin-gonic/gin"
// )

// var (
// 	masterAddr string
// 	clientID   int64
// )

// func main() {
// 	// Parse command line flags
// 	port := flag.String("port", "8080", "web server port")
// 	master := flag.String("master", "127.0.0.1:50051", "master server address")
// 	flag.Parse()

// 	masterAddr = *master
// 	log.Printf("Web server starting on :%s", *port)
// 	log.Printf("Connecting to master: %s", masterAddr)

// 	// Load or create client ID
// 	clientID = loadClientID()
// 	if clientID == 0 {
// 		log.Println("No client ID found - will be assigned on first upload")
// 	} else {
// 		log.Printf("Using client ID: %d", clientID)
// 	}

// 	// Debug: print working directory
// 	wd, _ := os.Getwd()
// 	log.Printf("Working directory: %s", wd)

// 	// Set Gin to release mode for production
// 	gin.SetMode(gin.ReleaseMode)

// 	// Create router
// 	router := gin.Default()

// 	// Enable CORS for frontend
// 	router.Use(corsMiddleware())

// 	// API routes
// 	api := router.Group("/api")
// 	{
// 		// File operations
// 		api.POST("/upload", handleUpload)
// 		api.GET("/download/:filename", handleDownload)
// 		api.DELETE("/delete/:filename", handleDelete)
// 		api.GET("/files", handleListFiles)

// 		// System information
// 		api.GET("/system/status", handleSystemStatus)
// 		api.GET("/files/:filename/chunks", handleFileChunks)
// 		api.GET("/clients", handleListClients)
// 		api.GET("/client/:id/files", handleClientFiles)
// 	}

// 	// Health check
// 	router.GET("/health", func(c *gin.Context) {
// 		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
// 	})

// 	// Start server
// 	if err := router.Run(":" + *port); err != nil {
// 		log.Fatalf("Failed to start server: %v", err)
// 	}
// }

// // corsMiddleware adds CORS headers for frontend access
// func corsMiddleware() gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
// 		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
// 		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
// 		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

// 		if c.Request.Method == "OPTIONS" {
// 			c.AbortWithStatus(204)
// 			return
// 		}

// 		c.Next()
// 	}
// }

// // loadClientID reads client ID from file (same as CLI client)
// func loadClientID() int64 {
// 	data, err := os.ReadFile(".client_id")
// 	if err != nil {
// 		log.Printf("DEBUG: Failed to read .client_id: %v", err)
// 		return 0
// 	}
// 	var id int64
// 	_, err = fmt.Sscanf(string(data), "%d", &id)
// 	if err != nil {
// 		log.Printf("DEBUG: Failed to parse client ID: %v", err)
// 		return 0
// 	}
// 	log.Printf("DEBUG: Loaded client ID from .client_id: %d", id)
// 	return id
// }

// // saveClientID saves client ID to file (same as CLI client)
// func saveClientID(id int64) error {
// 	return os.WriteFile(".client_id", []byte(fmt.Sprintf("%d", id)), 0644)
// }

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

var (
	masterAddr string
	// Remove global clientID - we'll load it dynamically
)

func main() {
	// Parse command line flags
	port := flag.String("port", "8080", "web server port")
	master := flag.String("master", "127.0.0.1:50051", "master server address")
	flag.Parse()

	masterAddr = *master
	log.Printf("Web server starting on :%s", *port)
	log.Printf("Connecting to master: %s", masterAddr)

	// Check if client ID exists
	currentID := loadClientID()
	if currentID == 0 {
		log.Println("No client ID found - will be assigned on first upload")
	} else {
		log.Printf("Found existing client ID: %d", currentID)
	}

	// Set Gin to release mode for production
	gin.SetMode(gin.ReleaseMode)

	// Create router
	router := gin.Default()

	// Enable CORS for frontend
	router.Use(corsMiddleware())

	// API routes
	api := router.Group("/api")
	{
		// File operations
		api.POST("/upload", handleUpload)
		api.GET("/download/:filename", handleDownload)
		api.DELETE("/delete/:filename", handleDelete)
		api.GET("/files", handleListFiles)

		// System information
		api.GET("/system/status", handleSystemStatus)
		api.GET("/files/:filename/chunks", handleFileChunks)
		api.GET("/clients", handleListClients)
		api.GET("/client/:id/files", handleClientFiles)
	}

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	// Start server
	if err := router.Run(":" + *port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// corsMiddleware adds CORS headers for frontend access
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// getClientID returns current client ID (loads dynamically)
func getClientID() int64 {
	return loadClientID()
}

// loadClientID reads client ID from file (same as CLI client)
func loadClientID() int64 {
	data, err := os.ReadFile(".client_id")
	if err != nil {
		log.Printf("DEBUG: Failed to read .client_id: %v", err)
		return 0
	}
	var id int64
	_, err = fmt.Sscanf(string(data), "%d", &id)
	if err != nil {
		log.Printf("DEBUG: Failed to parse client ID: %v", err)
		return 0
	}
	log.Printf("DEBUG: Loaded client ID from .client_id: %d", id)
	return id
}

// saveClientID saves client ID to file (same as CLI client)
func saveClientID(id int64) error {
	return os.WriteFile(".client_id", []byte(fmt.Sprintf("%d", id)), 0644)
}
