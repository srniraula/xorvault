// package main

// import (
// 	"context"
// 	"fmt"
// 	"os"
// 	"path/filepath"

// 	"dfs-project/dfspb"

// 	"google.golang.org/grpc"
// 	"google.golang.org/grpc/credentials/insecure"
// )

// const CHUNK_SIZE = 1 * 1024 * 1024 // 1 MB

// // uploadToDFS uploads a file to the DFS
// func uploadToDFS(localPath, filename string) error {
// 	// Get file info
// 	fileInfo, err := os.Stat(localPath)
// 	if err != nil {
// 		return fmt.Errorf("cannot access file: %v", err)
// 	}
// 	fileSize := fileInfo.Size()

// 	// Connect to master
// 	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		return fmt.Errorf("failed to connect to master: %v", err)
// 	}
// 	defer conn.Close()

// 	master := dfspb.NewMasterServerClient(conn)

// 	// Register file with master
// 	createResp, err := master.CreateFile(context.Background(), &dfspb.CreateFileRequest{
// 		Filename:  filename,
// 		TotalSize: fileSize,
// 		ClientId:  clientID,
// 	})
// 	if err != nil {
// 		return fmt.Errorf("CreateFile failed: %v", err)
// 	}

// 	// Save client ID if this is first upload
// 	if clientID == 0 {
// 		clientID = createResp.ClientId
// 		saveClientID(clientID)
// 	}

// 	// Read file and upload chunks
// 	file, err := os.Open(localPath)
// 	if err != nil {
// 		return fmt.Errorf("failed to open file: %v", err)
// 	}
// 	defer file.Close()

// 	// Simple upload logic (not using the full streaming pipeline for simplicity)
// 	// In production, you'd reuse the client upload logic
// 	successfulChunks := []string{}

// 	for stripeNum, stripe := range createResp.Stripes {
// 		// Read data for this stripe (simplified)
// 		chunk1Data := make([]byte, CHUNK_SIZE)
// 		chunk2Data := make([]byte, CHUNK_SIZE)

// 		n1, _ := file.Read(chunk1Data)
// 		n2, _ := file.Read(chunk2Data)

// 		chunk1Data = chunk1Data[:n1]
// 		chunk2Data = chunk2Data[:n2]

// 		// Calculate parity
// 		parityData := xorBytes(chunk1Data, chunk2Data)

// 		// Upload chunks
// 		chunks := []struct {
// 			data   []byte
// 			id     string
// 			server string
// 		}{
// 			{chunk1Data, stripe.ChunkIds[0], stripe.Servers[0]},
// 			{chunk2Data, stripe.ChunkIds[1], stripe.Servers[1]},
// 			{parityData, stripe.ChunkIds[2], stripe.Servers[2]},
// 		}

// 		for _, chunk := range chunks {
// 			err := uploadChunk(chunk.server, chunk.id, chunk.data, clientID)
// 			if err != nil {
// 				return fmt.Errorf("failed to upload chunk %s: %v", chunk.id, err)
// 			}
// 			successfulChunks = append(successfulChunks, chunk.id)
// 		}

// 		fmt.Printf("Uploaded stripe %d\n", stripeNum)
// 	}

// 	// Confirm write
// 	_, err = master.ConfirmWrite(context.Background(), &dfspb.ConfirmWriteRequest{
// 		Filename: filename,
// 		ChunkIds: successfulChunks,
// 	})
// 	if err != nil {
// 		return fmt.Errorf("ConfirmWrite failed: %v", err)
// 	}

// 	return nil
// }

// // downloadFromDFS downloads a file from DFS and returns local path
// func downloadFromDFS(filename string) (string, error) {
// 	// Connect to master
// 	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		return "", fmt.Errorf("failed to connect to master: %v", err)
// 	}
// 	defer conn.Close()

// 	master := dfspb.NewMasterServerClient(conn)

// 	// Get file metadata
// 	meta, err := master.GetFileMetadata(context.Background(), &dfspb.GetFileMetadataRequest{
// 		Filename: filename,
// 		ClientId: clientID,
// 	})
// 	if err != nil {
// 		return "", fmt.Errorf("GetFileMetadata failed: %v", err)
// 	}

// 	// Create temporary file
// 	tempPath := filepath.Join(os.TempDir(), "download_"+filename)
// 	outFile, err := os.Create(tempPath)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to create temp file: %v", err)
// 	}
// 	defer outFile.Close()

// 	// Download stripes
// 	for stripeNum := int32(1); stripeNum <= int32(len(meta.Stripes)); stripeNum++ {
// 		stripe := meta.Stripes[stripeNum]

// 		// Download data chunks (simplified - not using reconstruction)
// 		chunk1, err := downloadChunk(stripe.Servers[0], stripe.ChunkIds[0], clientID)
// 		if err != nil {
// 			return "", fmt.Errorf("failed to download chunk: %v", err)
// 		}

// 		chunk2, err := downloadChunk(stripe.Servers[1], stripe.ChunkIds[1], clientID)
// 		if err != nil {
// 			return "", fmt.Errorf("failed to download chunk: %v", err)
// 		}

// 		// Write to file
// 		outFile.Write(chunk1)
// 		outFile.Write(chunk2)
// 	}

// 	return tempPath, nil
// }

// // deleteFromDFS deletes a file from DFS
// func deleteFromDFS(filename string) error {
// 	if clientID == 0 {
// 		return fmt.Errorf("no client ID")
// 	}

// 	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		return fmt.Errorf("failed to connect to master: %v", err)
// 	}
// 	defer conn.Close()

// 	master := dfspb.NewMasterServerClient(conn)

// 	resp, err := master.DeleteFile(context.Background(), &dfspb.DeleteFileRequest{
// 		Filename: filename,
// 		ClientId: clientID,
// 	})
// 	if err != nil {
// 		return fmt.Errorf("DeleteFile failed: %v", err)
// 	}

// 	if !resp.Success {
// 		return fmt.Errorf("delete failed: %s", resp.Message)
// 	}

// 	return nil
// }

// // listFilesFromDFS lists all files for current client
// func listFilesFromDFS() ([]string, error) {
// 	if clientID == 0 {
// 		return []string{}, nil
// 	}

// 	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to connect to master: %v", err)
// 	}
// 	defer conn.Close()

// 	master := dfspb.NewMasterServerClient(conn)

// 	resp, err := master.ListFiles(context.Background(), &dfspb.ListFilesRequest{
// 		ClientId: clientID,
// 	})
// 	if err != nil {
// 		return nil, fmt.Errorf("ListFiles failed: %v", err)
// 	}

// 	return resp.Filenames, nil
// }

// // uploadChunk uploads a single chunk to a chunk server
// func uploadChunk(server, chunkID string, data []byte, clientID int64) error {
// 	conn, err := grpc.NewClient(server, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		return err
// 	}
// 	defer conn.Close()

// 	client := dfspb.NewChunkServerClient(conn)

// 	_, err = client.WriteChunk(context.Background(), &dfspb.WriteChunkRequest{
// 		ChunkId:  chunkID,
// 		Data:     data,
// 		ClientId: clientID,
// 		Checksum: calculateChecksum(data),
// 	})

// 	return err
// }

// // downloadChunk downloads a single chunk from a chunk server
// func downloadChunk(server, chunkID string, clientID int64) ([]byte, error) {
// 	conn, err := grpc.NewClient(server, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer conn.Close()

// 	client := dfspb.NewChunkServerClient(conn)

// 	resp, err := client.ReadChunk(context.Background(), &dfspb.ReadChunkRequest{
// 		ChunkId:  chunkID,
// 		ClientId: clientID,
// 	})
// 	if err != nil {
// 		return nil, err
// 	}

// 	return resp.Data, nil
// }

// // xorBytes performs XOR operation on two byte slices
// func xorBytes(a, b []byte) []byte {
// 	maxLen := len(a)
// 	if len(b) > maxLen {
// 		maxLen = len(b)
// 	}

// 	result := make([]byte, maxLen)
// 	for i := 0; i < maxLen; i++ {
// 		var aVal, bVal byte
// 		if i < len(a) {
// 			aVal = a[i]
// 		}
// 		if i < len(b) {
// 			bVal = b[i]
// 		}
// 		result[i] = aVal ^ bVal
// 	}
// 	return result
// }

// // calculateChecksum calculates CRC32 checksum
// func calculateChecksum(data []byte) string {
// 	// Simplified - you should import your actual checksum function
// 	return fmt.Sprintf("%08x", len(data)) // Placeholder
// }

package main

import (
	"context"
	"fmt"
	"hash/crc32"
	"log"
	"os"
	"path/filepath"

	"dfs-project/dfspb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const CHUNK_SIZE = 1 * 1024 * 1024 // 1 MB

// uploadToDFS uploads a file to the DFS
func uploadToDFS(localPath, filename string) error {
	// Get current client ID
	clientID := getClientID()

	// Get file info
	fileInfo, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("cannot access file: %v", err)
	}
	fileSize := fileInfo.Size()

	// Connect to master
	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to master: %v", err)
	}
	defer conn.Close()

	master := dfspb.NewMasterServerClient(conn)

	// Register file with master
	createResp, err := master.CreateFile(context.Background(), &dfspb.CreateFileRequest{
		Filename:  filename,
		TotalSize: fileSize,
		ClientId:  clientID,
	})
	if err != nil {
		return fmt.Errorf("CreateFile failed: %v", err)
	}

	// Save client ID if this is first upload
	if clientID == 0 {
		clientID = createResp.ClientId
		saveClientID(clientID)
		log.Printf("Assigned new client ID: %d", clientID)
	}

	// Read file and upload chunks
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// Simple upload logic (not using the full streaming pipeline for simplicity)
	// In production, you'd reuse the client upload logic
	successfulChunks := []string{}

	for stripeNum, stripe := range createResp.Stripes {
		// Read data for this stripe (simplified)
		chunk1Data := make([]byte, CHUNK_SIZE)
		chunk2Data := make([]byte, CHUNK_SIZE)

		n1, _ := file.Read(chunk1Data)
		n2, _ := file.Read(chunk2Data)

		chunk1Data = chunk1Data[:n1]
		chunk2Data = chunk2Data[:n2]

		// Calculate parity
		parityData := xorBytes(chunk1Data, chunk2Data)

		// Upload chunks
		chunks := []struct {
			data   []byte
			id     string
			server string
		}{
			{chunk1Data, stripe.ChunkIds[0], stripe.Servers[0]},
			{chunk2Data, stripe.ChunkIds[1], stripe.Servers[1]},
			{parityData, stripe.ChunkIds[2], stripe.Servers[2]},
		}

		for _, chunk := range chunks {
			err := uploadChunk(chunk.server, chunk.id, chunk.data, clientID)
			if err != nil {
				return fmt.Errorf("failed to upload chunk %s: %v", chunk.id, err)
			}
			successfulChunks = append(successfulChunks, chunk.id)
		}

		fmt.Printf("Uploaded stripe %d\n", stripeNum)
	}

	// Confirm write
	_, err = master.ConfirmWrite(context.Background(), &dfspb.ConfirmWriteRequest{
		Filename: filename,
		ChunkIds: successfulChunks,
	})
	if err != nil {
		return fmt.Errorf("ConfirmWrite failed: %v", err)
	}

	return nil
}

// downloadFromDFS downloads a file from DFS and returns local path
func downloadFromDFS(filename string) (string, error) {
	clientID := getClientID()

	// Connect to master
	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("failed to connect to master: %v", err)
	}
	defer conn.Close()

	master := dfspb.NewMasterServerClient(conn)

	// Get file metadata
	meta, err := master.GetFileMetadata(context.Background(), &dfspb.GetFileMetadataRequest{
		Filename: filename,
		ClientId: clientID,
	})
	if err != nil {
		return "", fmt.Errorf("GetFileMetadata failed: %v", err)
	}

	// Create temporary file
	tempPath := filepath.Join(os.TempDir(), "download_"+filename)
	outFile, err := os.Create(tempPath)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %v", err)
	}
	defer outFile.Close()

	// Download stripes
	for stripeNum := int32(1); stripeNum <= int32(len(meta.Stripes)); stripeNum++ {
		stripe := meta.Stripes[stripeNum]

		// Download data chunks (simplified - not using reconstruction)
		chunk1, err := downloadChunk(stripe.Servers[0], stripe.ChunkIds[0], clientID)
		if err != nil {
			return "", fmt.Errorf("failed to download chunk: %v", err)
		}

		chunk2, err := downloadChunk(stripe.Servers[1], stripe.ChunkIds[1], clientID)
		if err != nil {
			return "", fmt.Errorf("failed to download chunk: %v", err)
		}

		// Write to file
		outFile.Write(chunk1)
		outFile.Write(chunk2)
	}

	return tempPath, nil
}

// deleteFromDFS deletes a file from DFS
func deleteFromDFS(filename string) error {
	clientID := getClientID()
	if clientID == 0 {
		return fmt.Errorf("no client ID")
	}

	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to master: %v", err)
	}
	defer conn.Close()

	master := dfspb.NewMasterServerClient(conn)

	resp, err := master.DeleteFile(context.Background(), &dfspb.DeleteFileRequest{
		Filename: filename,
		ClientId: clientID,
	})
	if err != nil {
		return fmt.Errorf("DeleteFile failed: %v", err)
	}

	if !resp.Success {
		return fmt.Errorf("delete failed: %s", resp.Message)
	}

	return nil
}

// listFilesFromDFS lists all files for current client
func listFilesFromDFS() ([]string, error) {
	clientID := getClientID()
	if clientID == 0 {
		return []string{}, nil
	}

	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to master: %v", err)
	}
	defer conn.Close()

	master := dfspb.NewMasterServerClient(conn)

	resp, err := master.ListFiles(context.Background(), &dfspb.ListFilesRequest{
		ClientId: clientID,
	})
	if err != nil {
		return nil, fmt.Errorf("ListFiles failed: %v", err)
	}

	return resp.Filenames, nil
}

// uploadChunk uploads a single chunk to a chunk server
func uploadChunk(server, chunkID string, data []byte, clientID int64) error {
	conn, err := grpc.NewClient(server, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := dfspb.NewChunkServerClient(conn)

	_, err = client.WriteChunk(context.Background(), &dfspb.WriteChunkRequest{
		ChunkId:  chunkID,
		Data:     data,
		ClientId: clientID,
		Checksum: calculateChecksum(data),
	})

	return err
}

// downloadChunk downloads a single chunk from a chunk server
func downloadChunk(server, chunkID string, clientID int64) ([]byte, error) {
	conn, err := grpc.NewClient(server, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := dfspb.NewChunkServerClient(conn)

	resp, err := client.ReadChunk(context.Background(), &dfspb.ReadChunkRequest{
		ChunkId:  chunkID,
		ClientId: clientID,
	})
	if err != nil {
		return nil, err
	}

	return resp.Data, nil
}

// xorBytes performs XOR operation on two byte slices
func xorBytes(a, b []byte) []byte {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}

	result := make([]byte, maxLen)
	for i := 0; i < maxLen; i++ {
		var aVal, bVal byte
		if i < len(a) {
			aVal = a[i]
		}
		if i < len(b) {
			bVal = b[i]
		}
		result[i] = aVal ^ bVal
	}
	return result
}

// calculateChecksum calculates CRC32 checksum
func calculateChecksum(data []byte) string {
	checksum := crc32.ChecksumIEEE(data)
	return fmt.Sprintf("%08x", checksum)
}
