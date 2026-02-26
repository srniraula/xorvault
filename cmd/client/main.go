package main

import (
	"context"
	"dfs-project/dfspb"
	"dfs-project/pkg/config"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// CHUNK_SIZE defines how large each chunk should be (1 MB = 1024 * 1024 bytes)
// Files are split into chunks of this size before uploading to chunk servers
const CHUNK_SIZE = 1 * 1024 * 1024

// resolveFilePath checks if file exists as-is, otherwise prepends files/ directory
// For upload: looks in files/ directory if not found in current dir
// For download: files are saved to current directory with downloaded_ prefix
func resolveFilePath(filename string, forUpload bool) string {
	if forUpload {
		// First try the filename as given
		if _, err := os.Stat(filename); err == nil {
			return filename
		}

		// If not found, try files/ directory
		filesPath := "files/" + filename
		if _, err := os.Stat(filesPath); err == nil {
			return filesPath
		}

		// Return original filename (will fail later with proper error)
		return filename
	}

	// For download, just use the filename as-is (downloaded in current dir)
	return filename
}

// main is the entry point - parses command line arguments and calls upload/download
// Usage: go run cmd/client/main.go upload myfile.pdf
//
//	go run cmd/client/main.go download myfile.pdf
func main() {
	// Check if user provided correct number of arguments
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run cmd/client/main.go <upload|download|delete|ls|mkdir|rmdir|mv|cat|ls-detailed> <args...>")
	}

	// Load username from .username file (empty if new user)
	myUsername := loadUsername()
	if myUsername == "" {
		log.Println("New user - please specify username or it will be assigned (not supported yet in CLI, using 'default')")
		myUsername = "default"
	} else {
		log.Printf("Using existing username: %s", myUsername)
	}

	cmd := os.Args[1] // command name

	switch cmd {
	case "ls":
		listFiles(myUsername)
	case "ls-detailed":
		folderPath := ""
		if len(os.Args) > 2 {
			folderPath = os.Args[2]
		}
		listFilesDetailed(myUsername, folderPath)
	case "mkdir":
		if len(os.Args) < 3 {
			log.Fatal("Usage: client mkdir <folder_path>")
		}
		createFolder(myUsername, os.Args[2])
	case "rmdir":
		if len(os.Args) < 3 {
			log.Fatal("Usage: client rmdir <folder_path>")
		}
		deleteFolder(myUsername, os.Args[2])
	case "mv":
		if len(os.Args) < 4 {
			log.Fatal("Usage: client mv <source_path> <destination_path>")
		}
		moveFile(myUsername, os.Args[2], os.Args[3])
	case "cat":
		if len(os.Args) < 3 {
			log.Fatal("Usage: client cat <filename>")
		}
		catFile(myUsername, os.Args[2])
	case "upload", "download", "delete":
		if len(os.Args) < 3 {
			log.Fatalf("Usage: go run cmd/client/main.go %s <filename>", cmd)
		}
		file := os.Args[2] // filename to upload/download/delete

		if cmd == "upload" {
			// Resolve file path (check files/ directory)
			filePath := resolveFilePath(file, true)
			upload(filePath, myUsername)
		} else if cmd == "download" {
			download(file, myUsername)
		} else if cmd == "delete" {
			deleteFile(file, myUsername)
		}
	default:
		log.Fatalf("Unknown command: %s. Use upload, download, delete, ls, mkdir, rmdir, mv, cat, or ls-detailed", cmd)
	}
}

// upload reads a local file, splits it into chunks, and uploads to the DFS
// Process:
//  1. Read entire file into memory --> might be problem
//  2. Register file with master server
//  3. For each chunk: ask master where to store it, then send to chunk servers
//  4. Uses chain replication - sends to primary, which forwards to replicas
func upload(localPath string, username string) {
	// Get file info to determine file size
	fileInfo, err := os.Stat(localPath)
	if err != nil {
		log.Fatal("Cannot access file:", err)
	}
	if fileInfo.Size() == 0 {
		log.Fatal("File is empty")
	}
	fileSize := fileInfo.Size()

	// Extract just the filename (not the full path) for registration
	// This ensures chunk IDs don't include directory paths
	filename := filepath.Base(localPath)

	// Connect to master server (address can come from .master_addr, env, or config)
	conn, err := grpc.NewClient(getMasterAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	master := dfspb.NewMasterServerClient(conn) //grpc client that connects to master server.

	// Step 1: Register file with master and receive chunk allocation plan
	createResp, err := master.CreateFile(context.Background(), &dfspb.CreateFileRequest{
		Filename:  filename, // Use just filename, not full path
		TotalSize: fileSize,
		Username:  username,
	})
	if err != nil {
		// Check if it's an "already uploaded" error and print it cleanly
		if strings.Contains(err.Error(), "already uploaded by you") {
			// Extract the message from the gRPC error or just print a clean message
			// Simple way: just print what we know
			fmt.Printf("Error: file %s is already uploaded by you\n", filename)
			os.Exit(1)
		}
		log.Fatal("CreateFile failed:", err)
	}

	// Save username for future use
	if err := saveUsername(createResp.Username); err != nil {
		log.Printf("Warning: Failed to save username: %v", err)
	}

	// Calculate total chunks for progress tracking
	// Calculate total chunks for progress tracking

	// Step 2: Stream file in stripes and upload chunks in parallel using pipeline pattern
	// Producer goroutine reads file -> Consumer goroutines upload chunks
	// Memory efficient: only 2-3 stripes buffered in channel at any time

	stripeChan := make(chan Stripe, 2) // Buffer 2 stripes (6MB max in memory)
	errChan := make(chan error, 1)

	// Start producer goroutine to stream file
	go streamFileInStripes(localPath, createResp.Stripes, stripeChan, errChan)

	// Initialize ACK queue - will be populated as stripes are processed
	ackQueue := NewAckQueue()

	// Check for producer errors (non-blocking)
	select {
	case err := <-errChan:
		if err != nil {
			log.Fatal("Failed to stream file:", err)
		}
	default:
		// No error yet, proceed
	}

	// Start uploading stripes as they arrive from channel
	// This blocks until all stripes are consumed and uploaded
	successfulChunks, err := uploadStripesStreaming(stripeChan, ackQueue, createResp.Username)
	if err != nil {
		log.Fatal("Streaming upload failed:", err)
	}

	// Check for any producer errors that occurred during upload
	select {
	case err := <-errChan:
		if err != nil {
			log.Fatal("Error during file streaming:", err)
		}
	default:
		// No errors
	}

	// Check if all chunks were uploaded successfully
	if !ackQueue.IsEmpty() {
		pendingChunks := ackQueue.GetPending()
		log.Printf("Warning: %d chunks failed to upload: %v", len(pendingChunks), pendingChunks)
		log.Fatal("Upload incomplete - not all chunks were confirmed")
	}

	// Step 3: Confirm successful writes to master
	confirmResp, err := master.ConfirmWrite(context.Background(), &dfspb.ConfirmWriteRequest{
		Filename: filename, // Use filename, not full path
		ChunkIds: successfulChunks,
	})
	if err != nil {
		log.Fatal("ConfirmWrite failed:", err)
	}

	if confirmResp.Success {
		fmt.Println("Upload complete")
	} else {
		fmt.Println("Upload completed but confirmation failed")
	}

}

// download retrieves a file from the DFS using streaming with parity recovery
func download(filename string, username string) {
	// Connect to master server
	conn, err := grpc.NewClient(getMasterAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	master := dfspb.NewMasterServerClient(conn)

	// Step 1: Get file metadata from master (with authentication)
	meta, err := master.GetFileMetadata(context.Background(), &dfspb.GetFileMetadataRequest{
		Filename: filename,
		Username: username,
	})
	if err != nil {
		log.Fatal("File not found")
	}
	if len(meta.Stripes) == 0 {
		log.Fatal("File not found or access denied")
	}

	// Use stripe map directly from master (no parsing needed)
	totalStripes := len(meta.Stripes)

	// Create output file
	outputFile := "downloaded_" + filename
	opfile, err := os.Create(outputFile)
	if err != nil {
		log.Fatal("Failed to create output file:", err)
	}
	defer opfile.Close()

	// Step 2: Download and write stripes sequentially (streaming)
	var bytesWritten int64

	for stripeNum := int32(1); stripeNum <= int32(totalStripes); stripeNum++ {
		stripeInfo, exists := meta.Stripes[stripeNum]
		if !exists {
			log.Printf("Warning: Stripe %d metadata missing, skipping", stripeNum)
			continue
		}

		// Convert proto StripeMetadata to DownloadStripeInfo
		downloadInfo := DownloadStripeInfo{
			StripeNum:   int(stripeNum),
			DataChunk1:  ChunkServerPair{ChunkID: stripeInfo.ChunkIds[0], Server: stripeInfo.Servers[0]},
			DataChunk2:  ChunkServerPair{ChunkID: stripeInfo.ChunkIds[1], Server: stripeInfo.Servers[1]},
			ParityChunk: ChunkServerPair{ChunkID: stripeInfo.ChunkIds[2], Server: stripeInfo.Servers[2]},
		}

		// Download all 3 chunks of this stripe in parallel
		stripe := downloadStripe(downloadInfo, username)

		// Attempt reconstruction if needed
		err := reconstructMissingChunk(&stripe, downloadInfo)
		if err != nil {
			log.Fatalf("Stripe %d reconstruction failed: %v", stripeNum, err)
		}

		// Check if we have data to write
		if (downloadInfo.DataChunk1.ChunkID != "" && stripe.DataChunk1 == nil) ||
			(downloadInfo.DataChunk2.ChunkID != "" && stripe.DataChunk2 == nil) {
			log.Fatalf("Stripe %d missing data after reconstruction", stripeNum)
		}

		// Write stripe data to file
		isLastStripe := (int(stripeNum) == totalStripes)
		written, err := writeStripeToFile(opfile, &stripe, isLastStripe, meta.FileSize, bytesWritten)
		if err != nil {
			log.Fatalf("Failed to write stripe %d: %v", stripeNum, err)
		}

		bytesWritten += int64(written)
	}

	// Step 3: Verify and finalize
	if bytesWritten != meta.FileSize {
		log.Printf("Warning: File size mismatch (expected %d, wrote %d)", meta.FileSize, bytesWritten)
	}

	fmt.Println("Download complete")
}

// deleteFile removes a file from the DFS
func deleteFile(filename string, username string) {
	if username == "" {
		log.Fatal("Cannot delete file: no username found.")
	}

	log.Printf("Deleting file: %s (user: %s)", filename, username)

	// Connect to master server
	conn, err := grpc.NewClient(getMasterAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("Failed to connect to master:", err)
	}
	defer conn.Close()

	masterClient := dfspb.NewMasterServerClient(conn)

	// Send delete request
	resp, err := masterClient.DeleteFile(context.Background(), &dfspb.DeleteFileRequest{
		Filename: filename,
		Username: username,
	})

	if err != nil {
		log.Fatalf("Delete failed: %v", err)
	}

	if !resp.Success {
		log.Fatalf("Delete failed: %s", resp.Message)
	}

	log.Printf("Successfully deleted %s: %s", filename, resp.Message)
}

// listFiles displays all files uploaded by this user
func listFiles(username string) {
	if username == "" {
		log.Println("No files uploaded yet (new user)")
		return
	}

	// Connect to master server
	conn, err := grpc.NewClient(getMasterAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("Failed to connect to master:", err)
	}
	defer conn.Close()

	masterClient := dfspb.NewMasterServerClient(conn)

	// Request file list
	resp, err := masterClient.ListFiles(context.Background(), &dfspb.ListFilesRequest{
		Username: username,
	})

	if err != nil {
		log.Fatalf("ListFiles failed: %v", err)
	}

	if len(resp.Filenames) == 0 {
		log.Println("No files uploaded")
		return
	}

	log.Printf("Files uploaded by user %s:", username)
	for i, filename := range resp.Filenames {
		log.Printf("  %d. %s", i+1, filename)
	}
}

// getMasterAddr returns the master address to use for client RPCs.
// Precedence:
//  1. The MASTER_ADDR environment variable (set when invoking the command)
//  2. A local file named ".master_addr" in the current working directory (trimmed)
//  3. The default from config.GetMasterAddr()
//
// Automatic failover:
// If the resolved primary address is unreachable or responds as standby, the
// client tries the address in ".secondary_addr".  When the secondary is found
// to be active it rewrites ".master_addr" so that all subsequent calls use the
// promoted secondary without any manual reconfiguration.
func getMasterAddr() string {
	// 1) Prefer explicit environment variable (no failover – caller knows best)
	if env := os.Getenv("MASTER_ADDR"); strings.TrimSpace(env) != "" {
		return strings.TrimSpace(env)
	}

	// 2) Read candidate primary from .master_addr / config
	primaryAddr := ""
	if data, err := os.ReadFile(".master_addr"); err == nil {
		if addr := strings.TrimSpace(string(data)); addr != "" {
			primaryAddr = addr
		}
	}
	if primaryAddr == "" {
		primaryAddr = config.GetMasterAddr()
	}

	// 3) Read secondary address (written by standby master at startup)
	secondaryAddr := ""
	if data, err := os.ReadFile(".secondary_addr"); err == nil {
		if addr := strings.TrimSpace(string(data)); addr != "" {
			secondaryAddr = addr
		}
	}

	// If there is no secondary configured just return primary immediately.
	if secondaryAddr == "" {
		return primaryAddr
	}

	// 4) Probe the primary: if it is reachable and active, use it.
	if isActive(primaryAddr) {
		return primaryAddr
	}

	// 5) Primary is down or in standby – try secondary.
	log.Printf("[failover] Primary %s is unavailable, trying secondary %s", primaryAddr, secondaryAddr)
	if isActive(secondaryAddr) {
		log.Printf("[failover] Secondary %s is ACTIVE – updating .master_addr", secondaryAddr)
		// Persist so future client invocations skip the probe.
		_ = os.WriteFile(".master_addr", []byte(secondaryAddr+"\n"), 0644)
		return secondaryAddr
	}

	// Both unreachable – return primary and let the caller surface the error.
	log.Printf("[failover] Both primary and secondary appear down; using primary %s", primaryAddr)
	return primaryAddr
}

// isActive dials addr and calls Ping.  Returns true only when the server
// responds with Active=true (i.e., it is in primary mode, not standby).
func isActive(addr string) bool {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false
	}
	defer conn.Close()

	client := dfspb.NewMasterServerClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.Ping(ctx, &dfspb.PingRequest{})
	if err != nil {
		return false
	}
	return resp.Active
}
