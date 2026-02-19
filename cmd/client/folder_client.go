package main

import (
	"context"
	"dfs-project/dfspb"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// createFolder creates a new folder on the server
func createFolder(clientID int64, folderPath string) {
	if clientID == 0 {
		log.Fatal("Cannot create folder: no client ID. Please upload a file first.")
	}

	// Connect to master server
	conn, err := grpc.NewClient(getMasterAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("Failed to connect to master:", err)
	}
	defer conn.Close()

	masterClient := dfspb.NewMasterServerClient(conn)

	// Send create folder request
	resp, err := masterClient.CreateFolder(context.Background(), &dfspb.CreateFolderRequest{
		ClientId:   clientID,
		FolderPath: folderPath,
	})

	if err != nil {
		log.Fatalf("CreateFolder failed: %v", err)
	}

	if !resp.Success {
		log.Fatalf("CreateFolder failed: %s", resp.Message)
	}

	fmt.Println(resp.Message)
}

// deleteFolder removes an empty folder from the server
func deleteFolder(clientID int64, folderPath string) {
	if clientID == 0 {
		log.Fatal("Cannot delete folder: no client ID. Please upload a file first.")
	}

	// Connect to master server
	conn, err := grpc.NewClient(getMasterAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("Failed to connect to master:", err)
	}
	defer conn.Close()

	masterClient := dfspb.NewMasterServerClient(conn)

	// Send delete folder request
	resp, err := masterClient.DeleteFolder(context.Background(), &dfspb.DeleteFolderRequest{
		ClientId:   clientID,
		FolderPath: folderPath,
	})

	if err != nil {
		log.Fatalf("DeleteFolder failed: %v", err)
	}

	if !resp.Success {
		log.Fatalf("DeleteFolder failed: %s", resp.Message)
	}

	fmt.Println(resp.Message)
}

// moveFile moves or renames a file on the server
func moveFile(clientID int64, sourcePath, destPath string) {
	if clientID == 0 {
		log.Fatal("Cannot move file: no client ID. Please upload a file first.")
	}

	// Connect to master server
	conn, err := grpc.NewClient(getMasterAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("Failed to connect to master:", err)
	}
	defer conn.Close()

	masterClient := dfspb.NewMasterServerClient(conn)

	// Send move file request
	resp, err := masterClient.MoveFile(context.Background(), &dfspb.MoveFileRequest{
		ClientId:        clientID,
		SourcePath:      sourcePath,
		DestinationPath: destPath,
	})

	if err != nil {
		log.Fatalf("MoveFile failed: %v", err)
	}

	if !resp.Success {
		log.Fatalf("MoveFile failed: %s", resp.Message)
	}

	fmt.Println(resp.Message)
}

// listFilesDetailed shows detailed file listing with sizes, timestamps, and folder structure
func listFilesDetailed(clientID int64, folderPath string) {
	if clientID == 0 {
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

	// Request detailed file list
	resp, err := masterClient.ListFilesDetailed(context.Background(), &dfspb.ListFilesDetailedRequest{
		ClientId:   clientID,
		FolderPath: folderPath,
	})

	if err != nil {
		log.Fatalf("ListFilesDetailed failed: %v", err)
	}

	if len(resp.Items) == 0 {
		if folderPath == "" {
			fmt.Println("No files or folders")
		} else {
			fmt.Printf("No items in folder '%s'\n", folderPath)
		}
		return
	}

	// Display header
	if folderPath == "" {
		fmt.Println("Files and folders:")
	} else {
		fmt.Printf("Contents of '%s':\n", folderPath)
	}
	fmt.Println("================================================================================")
	fmt.Printf("%-5s %-40s %12s %20s\n", "Type", "Path", "Size", "Upload Time")
	fmt.Println("--------------------------------------------------------------------------------")

	// Display each item
	for _, item := range resp.Items {
		itemType := "FILE"
		size := formatSize(item.Size)
		uploadTime := "-"

		if item.IsDirectory {
			itemType = "DIR"
			size = "-"
		} else if item.UploadTime > 0 {
			uploadTime = time.Unix(item.UploadTime, 0).Format("2006-01-02 15:04:05")
		}

		fmt.Printf("%-5s %-40s %12s %20s\n", itemType, item.Path, size, uploadTime)
	}
	fmt.Println("================================================================================")
}

// catFile displays file content (for text files) or info (for binary files)
func catFile(clientID int64, filename string) {
	if clientID == 0 {
		log.Fatal("Cannot read file: no client ID. Please upload a file first.")
	}

	// Connect to master server
	conn, err := grpc.NewClient(getMasterAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("Failed to connect to master:", err)
	}
	defer conn.Close()

	masterClient := dfspb.NewMasterServerClient(conn)

	// Read file content (first 64KB for preview)
	const maxPreviewSize = 64 * 1024
	resp, err := masterClient.ReadFileContent(context.Background(), &dfspb.ReadFileContentRequest{
		Filename: filename,
		ClientId: clientID,
		Offset:   0,
		Length:   maxPreviewSize,
	})

	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	if len(resp.Data) == 0 {
		fmt.Println("File is empty")
		return
	}

	// Display file info
	fmt.Printf("File: %s (Total size: %s)\n", filename, formatSize(resp.TotalSize))

	// Check if the data is likely text
	if isTextData(resp.Data) {
		fmt.Println("--- Content Preview ---")
		fmt.Println(string(resp.Data))
		if resp.TotalSize > maxPreviewSize {
			fmt.Printf("\n... (showing first %s of %s total)\n", formatSize(int64(len(resp.Data))), formatSize(resp.TotalSize))
		}
	} else {
		fmt.Println("(Binary file - use 'download' to retrieve full content)")
		fmt.Printf("First %d bytes (hex): %x...\n", min(32, len(resp.Data)), resp.Data[:min(32, len(resp.Data))])
	}
}

// Helper functions

// formatSize converts bytes to human-readable format
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// isTextData checks if data is likely to be text (printable ASCII/UTF-8)
func isTextData(data []byte) bool {
	if len(data) == 0 {
		return true
	}

	textChars := 0
	for i := 0; i < len(data) && i < 512; i++ {
		b := data[i]
		// Check for common text characters
		if (b >= 32 && b <= 126) || b == '\n' || b == '\r' || b == '\t' {
			textChars++
		} else if b >= 128 {
			// Likely UTF-8 multibyte character
			textChars++
		}
	}

	// If more than 80% of chars are text-like, consider it text
	return float64(textChars)/float64(min(len(data), 512)) > 0.8
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
