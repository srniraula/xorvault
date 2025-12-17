package main

import (
	"context"
	"dfs-project/dfspb"
	"dfs-project/pkg/config"
	"log"
	"os"
	"path/filepath"
	"strings"

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
		log.Fatal("Usage: go run cmd/client/main.go <upload|download|delete|ls> <filename>")
	}

	// Load client ID from .client_id file (0 if new client)
	myID := loadClientID()
	if myID == 0 {
		log.Println("New client - will receive ID from master")
	} else {
		log.Printf("Using existing client ID: %d", myID)
	}

	cmd := os.Args[1] // "upload", "download", "delete", or "ls"

	if cmd == "ls" {
		listFiles(myID)
	} else if len(os.Args) < 3 {
		log.Fatal("Usage: go run cmd/client/main.go <upload|download|delete> <filename>")
	} else {
		file := os.Args[2] // filename to upload/download/delete

		if cmd == "upload" {
			// Resolve file path (check files/ directory)
			filePath := resolveFilePath(file, true)
			upload(filePath, myID)
		} else if cmd == "download" {
			download(file, myID)
		} else if cmd == "delete" {
			deleteFile(file, myID)
		} else {
			log.Fatalf("Unknown command: %s. Use upload, download, delete, or ls", cmd)
		}
	}
}

// upload reads a local file, splits it into chunks, and uploads to the DFS
// Process:
//  1. Read entire file into memory --> might be problem
//  2. Register file with master server
//  3. For each chunk: ask master where to store it, then send to chunk servers
//  4. Uses chain replication - sends to primary, which forwards to replicas
func upload(localPath string, myID int64) {
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
		ClientId:  myID,
	})
	if err != nil {
		log.Fatal("CreateFile failed:", err)
	}
	log.Printf("File registered with client ID: %d", createResp.ClientId)

	// Save client ID for future use (if new client)
	if myID == 0 {
		if err := saveClientID(createResp.ClientId); err != nil {
			log.Printf("Warning: Failed to save client ID: %v", err)
		}
	}

	// Calculate total chunks for progress tracking
	totalChunks := (int(fileSize) + CHUNK_SIZE - 1) / CHUNK_SIZE
	log.Printf("Uploading %s → %d chunks (%.2f MB)", filename, totalChunks, float64(fileSize)/(1024*1024))

	// Display chunk allocation plan received from CreateFile
	log.Printf("Chunk allocation plan:")
	for stripeNum, stripe := range createResp.Stripes {
		log.Printf("  Stripe %d: chunks=%v, servers=%v", stripeNum, stripe.ChunkIds, stripe.Servers)
	}

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
	successfulChunks, err := uploadStripesStreaming(stripeChan, ackQueue, createResp.ClientId)
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

	log.Printf("Successfully uploaded %d chunks", len(successfulChunks))

	// Step 3: Confirm successful writes to master
	confirmResp, err := master.ConfirmWrite(context.Background(), &dfspb.ConfirmWriteRequest{
		Filename: filename, // Use filename, not full path
		ChunkIds: successfulChunks,
	})
	if err != nil {
		log.Fatal("ConfirmWrite failed:", err)
	}

	if confirmResp.Success {
		log.Printf("Upload complete! %d/%d chunks confirmed as SUCCESS", totalChunks, totalChunks)
	} else {
		log.Printf("Upload completed but confirmation failed")
	}

}

// download retrieves a file from the DFS using streaming with parity recovery
// Process:
//  1. Get file metadata from master (chunk IDs, servers, file size)
//  2. For each stripe: download 3 chunks in parallel, reconstruct if needed
//  3. Write stripe data to disk incrementally (streaming)
//  4. Save with "downloaded_" prefix
func download(filename string, myID int64) {
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
		ClientId: myID,
	})
	if err != nil {
		log.Fatal("File not found")
	}
	if len(meta.Stripes) == 0 {
		log.Fatal("File not found or access denied")
	}

	log.Printf("Downloading %s (%d bytes)", filename, meta.FileSize)

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
	successfulStripes := 0

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
		stripe := downloadStripe(downloadInfo, myID)

		// Attempt reconstruction if needed
		err := reconstructMissingChunk(&stripe)
		if err != nil {
			log.Fatalf("Stripe %d reconstruction failed: %v", stripeNum, err)
		}

		// Check if we have data to write
		// DataChunk1 must always be present
		// DataChunk2 is only required if it was expected (even stripe or not last stripe)
		if stripe.DataChunk1 == nil {
			log.Fatalf("Stripe %d missing DataChunk1 after reconstruction", stripeNum)
		}
		if stripe.IsData2Expected && stripe.DataChunk2 == nil {
			log.Fatalf("Stripe %d missing DataChunk2 after reconstruction", stripeNum)
		}

		// Write stripe data to file
		isLastStripe := (int(stripeNum) == totalStripes)
		written, err := writeStripeToFile(opfile, &stripe, isLastStripe, meta.FileSize, bytesWritten)
		if err != nil {
			log.Fatalf("Failed to write stripe %d: %v", stripeNum, err)
		}

		bytesWritten += int64(written)
		successfulStripes++

		chunkWord := "chunk"
		if stripe.ChunksOK != 1 {
			chunkWord = "chunks"
		}
		log.Printf("Stripe %d/%d: %d/%d %s downloaded, %d bytes written",
			stripeNum, totalStripes, stripe.ChunksOK, 3, chunkWord, written)
	}

	// Step 3: Verify and finalize
	if bytesWritten != meta.FileSize {
		log.Printf("Warning: File size mismatch (expected %d, wrote %d)", meta.FileSize, bytesWritten)
	}

	log.Printf("Download complete: %s (%d stripes, %d bytes)", outputFile, successfulStripes, bytesWritten)
}

// deleteFile removes a file from the DFS
// Process:
//  1. Connect to master server
//  2. Send DeleteFile RPC with filename and client ID
//  3. Master verifies ownership and deletes all chunks from chunk servers
func deleteFile(filename string, myID int64) {
	if myID == 0 {
		log.Fatal("Cannot delete file: no client ID. Please upload a file first.")
	}

	log.Printf("Deleting file: %s (client ID: %d)", filename, myID)

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
		ClientId: myID,
	})

	if err != nil {
		log.Fatalf("Delete failed: %v", err)
	}

	if !resp.Success {
		log.Fatalf("Delete failed: %s", resp.Message)
	}

	log.Printf("Successfully deleted %s: %s", filename, resp.Message)
}

// listFiles displays all files uploaded by this client
func listFiles(myID int64) {
	if myID == 0 {
		log.Println("No files uploaded yet (new client)")
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
		ClientId: myID,
	})

	if err != nil {
		log.Fatalf("ListFiles failed: %v", err)
	}

	if len(resp.Filenames) == 0 {
		log.Println("No files uploaded")
		return
	}

	log.Printf("Files uploaded by client %d:", myID)
	for i, filename := range resp.Filenames {
		log.Printf("  %d. %s", i+1, filename)
	}
}

// getMasterAddr returns the master address to use for client RPCs.
// Precedence:
//  1) The MASTER_ADDR environment variable (set when invoking the command)
//  2) A local file named ".master_addr" in the current working directory (trimmed)
//  3) The default from config.GetMasterAddr()
func getMasterAddr() string {
	// 1) Prefer explicit environment variable
	if env := os.Getenv("MASTER_ADDR"); strings.TrimSpace(env) != "" {
		return strings.TrimSpace(env)
	}

	// 2) Then look for local .master_addr file (legacy)
	if data, err := os.ReadFile(".master_addr"); err == nil {
		addr := strings.TrimSpace(string(data))
		if addr != "" {
			return addr
		}
	}

	// 3) Fall back to config (which also checks env and defaults)
	return config.GetMasterAddr()
}
