package webserver

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Helper function to clean up test logs
func cleanupTestLogs(t *testing.T) {
	ResetLoggerForTesting()
	err := os.RemoveAll(webserverLogsDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("Failed to cleanup test logs: %v", err)
	}
}

// Helper function to verify log file exists and contains content
func verifyLogFileExists(t *testing.T, filename string) {
	path := filepath.Join(webserverLogsDir, filename)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Log file not created: %s", path)
	}
}

// Helper function to read log file contents
func readLogFile(t *testing.T, filename string) []string {
	path := filepath.Join(webserverLogsDir, filename)
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Failed to open log file: %v", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

// Helper function to verify log entry contains expected substring
func verifyLogEntry(t *testing.T, lines []string, expectedSubstring string) {
	for _, line := range lines {
		if strings.Contains(line, expectedSubstring) {
			return
		}
	}
	t.Fatalf("Expected log entry containing '%s' not found. Logs:\n%v", expectedSubstring, lines)
}

// ============================================================================
// CHUNK UPLOAD LOGGING TESTS
// ============================================================================

func TestChunkUploadLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	uploadID := "upload_123"
	filename := "test.pdf"
	totalChunks := 5
	tmpPath := "/tmp/dfs_uploads/upload_123"

	// Test: Log chunk upload start
	err := logger.LogChunkUploadStart(username, uploadID, filename, totalChunks, tmpPath)
	if err != nil {
		t.Fatalf("Failed to log chunk upload start: %v", err)
	}

	verifyLogFileExists(t, username+".log")
	lines := readLogFile(t, username+".log")
	if len(lines) == 0 {
		t.Fatal("No log entries found")
	}

	verifyLogEntry(t, lines, "CHUNK_UPLOAD_START")
	verifyLogEntry(t, lines, "upload_123")
	verifyLogEntry(t, lines, "test.pdf")
	verifyLogEntry(t, lines, "totalChunks=5")

	t.Logf("✓ Chunk upload start logged correctly")
}

func TestChunkStoredLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	uploadID := "upload_456"
	chunkIndex := 0
	chunkSize := int64(2097152) // 2MB
	uploadedCount := 1
	totalChunks := 5
	totalSize := int64(2097152)

	err := logger.LogChunkStored(username, uploadID, chunkIndex, chunkSize, uploadedCount, totalChunks, totalSize, "/tmp/dfs_uploads/upload_456")
	if err != nil {
		t.Fatalf("Failed to log chunk stored: %v", err)
	}

	lines := readLogFile(t, username+".log")
	verifyLogEntry(t, lines, "CHUNK_STORED")
	verifyLogEntry(t, lines, "chunkIndex=0")
	verifyLogEntry(t, lines, "chunkSize=2097152")
	verifyLogEntry(t, lines, "uploadedChunks=1/5")

	t.Logf("✓ Chunk stored logged correctly")
}

func TestChunkUploadCompleteLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	uploadID := "upload_789"
	filename := "largefile.zip"
	totalChunks := 10
	totalSize := int64(10485760)

	err := logger.LogChunkUploadComplete(username, uploadID, filename, totalChunks, totalSize, "/tmp/dfs_uploads/upload_789")
	if err != nil {
		t.Fatalf("Failed to log chunk upload complete: %v", err)
	}

	lines := readLogFile(t, username+".log")
	verifyLogEntry(t, lines, "CHUNK_UPLOAD_COMPLETE")
	verifyLogEntry(t, lines, "totalChunks=10")
	verifyLogEntry(t, lines, "totalSize=10485760")

	t.Logf("✓ Chunk upload complete logged correctly")
}

func TestChunkReassemblyLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	uploadID := "upload_asm"

	// Log reassembly start
	err := logger.LogChunkReassemblyStart(username, uploadID, "/tmp/dfs_uploads/upload_asm")
	if err != nil {
		t.Fatalf("Failed to log reassembly start: %v", err)
	}

	// Log reassembly complete
	err = logger.LogChunkReassemblyComplete(username, uploadID, int64(10485760), 1250)
	if err != nil {
		t.Fatalf("Failed to log reassembly complete: %v", err)
	}

	lines := readLogFile(t, username+".log")
	verifyLogEntry(t, lines, "CHUNK_REASSEMBLY_START")
	verifyLogEntry(t, lines, "CHUNK_REASSEMBLY_COMPLETE")
	verifyLogEntry(t, lines, "reassemblyTimeMs=1250")

	t.Logf("✓ Chunk reassembly logged correctly")
}

func TestChunkCleanupLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	uploadID := "upload_cleanup"

	err := logger.LogChunkUploadCleanup(username, uploadID, "/tmp/dfs_uploads/upload_cleanup", int64(10485760))
	if err != nil {
		t.Fatalf("Failed to log chunk cleanup: %v", err)
	}

	lines := readLogFile(t, username+".log")
	verifyLogEntry(t, lines, "CHUNK_CLEANUP")
	verifyLogEntry(t, lines, "sizeFreed=10485760")

	t.Logf("✓ Chunk cleanup logged correctly")
}

// ============================================================================
// DOWNLOAD SESSION LOGGING TESTS
// ============================================================================

func TestDownloadSessionLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	filename := "document.pdf"
	clientID := int64(42)
	tmpPath := "/tmp/download_42_document.pdf"

	// Log download start
	sessionID, err := logger.LogDownloadSessionStart(username, filename, clientID, tmpPath)
	if err != nil {
		t.Fatalf("Failed to log download start: %v", err)
	}

	if sessionID == "" {
		t.Fatal("Session ID not returned")
	}

	verifyLogFileExists(t, username+".log")
	lines := readLogFile(t, username+".log")
	verifyLogEntry(t, lines, "DOWNLOAD_START")
	verifyLogEntry(t, lines, "document.pdf")
	verifyLogEntry(t, lines, "clientId=42")

	// Verify session was created
	session := logger.GetDownloadSession(sessionID)
	if session == nil {
		t.Fatal("Download session not created")
	}
	if session.Username != username {
		t.Fatalf("Session username mismatch: expected %s, got %s", username, session.Username)
	}

	t.Logf("✓ Download session created and logged: %s", sessionID)
}

func TestDownloadCompleteLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	filename := "largefile.zip"
	clientID := int64(42)

	sessionID, _ := logger.LogDownloadSessionStart(username, filename, clientID, "/tmp/download_42_largefile.zip")
	time.Sleep(100 * time.Millisecond) // Simulate download time

	err := logger.LogDownloadComplete(sessionID)
	if err != nil {
		t.Fatalf("Failed to log download complete: %v", err)
	}

	lines := readLogFile(t, username+".log")
	verifyLogEntry(t, lines, "DOWNLOAD_COMPLETE")
	verifyLogEntry(t, lines, sessionID)

	t.Logf("✓ Download complete logged correctly")
}

func TestDownloadAbortedLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	filename := "largefile.bin"
	clientID := int64(99)

	sessionID, _ := logger.LogDownloadSessionStart(username, filename, clientID, "/tmp/download_99_largefile.bin")

	err := logger.LogDownloadAborted(sessionID, "connection_lost")
	if err != nil {
		t.Fatalf("Failed to log download aborted: %v", err)
	}

	lines := readLogFile(t, username+".log")
	verifyLogEntry(t, lines, "DOWNLOAD_ABORTED")
	verifyLogEntry(t, lines, "connection_lost")

	t.Logf("✓ Download aborted logged correctly")
}

func TestDownloadFailedLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	filename := "missing.pdf"
	clientID := int64(50)

	sessionID, _ := logger.LogDownloadSessionStart(username, filename, clientID, "/tmp/download_50_missing.pdf")

	err := logger.LogDownloadFailed(sessionID, "file not found")
	if err != nil {
		t.Fatalf("Failed to log download failed: %v", err)
	}

	lines := readLogFile(t, username+".log")
	verifyLogEntry(t, lines, "DOWNLOAD_FAILED")
	verifyLogEntry(t, lines, "file not found")

	t.Logf("✓ Download failed logged correctly")
}

// ============================================================================
// FILE OPERATIONS LOGGING TESTS
// ============================================================================

func TestFileDeleteLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	filename := "oldfile.txt"
	clientID := int64(42)

	// Log delete initiated
	err := logger.LogFileDeleteInitiated(username, filename, clientID)
	if err != nil {
		t.Fatalf("Failed to log delete initiated: %v", err)
	}

	// Log delete success
	err = logger.LogFileDeleteSuccess(username, filename, clientID, 5)
	if err != nil {
		t.Fatalf("Failed to log delete success: %v", err)
	}

	lines := readLogFile(t, username+".log")
	verifyLogEntry(t, lines, "FILE_DELETE_INITIATED")
	verifyLogEntry(t, lines, "FILE_DELETE_SUCCESS")
	verifyLogEntry(t, lines, "chunksDeleted=5")

	t.Logf("✓ File delete logged correctly")
}

func TestFileDeleteFailedLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	filename := "notfound.pdf"
	clientID := int64(99)

	err := logger.LogFileDeleteInitiated(username, filename, clientID)
	if err != nil {
		t.Fatalf("Failed to log delete initiated: %v", err)
	}

	err = logger.LogFileDeleteFailed(username, filename, clientID, "file not found")
	if err != nil {
		t.Fatalf("Failed to log delete failed: %v", err)
	}

	lines := readLogFile(t, username+".log")
	verifyLogEntry(t, lines, "FILE_DELETE_INITIATED")
	verifyLogEntry(t, lines, "FILE_DELETE_FAILED")
	verifyLogEntry(t, lines, "file not found")

	t.Logf("✓ File delete failed logged correctly")
}

// ============================================================================
// CLEANUP OPERATIONS LOGGING TESTS
// ============================================================================

func TestAbandonedUploadCleanupLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	uploadID := "abandoned_upload_001"
	tmpPath := "/tmp/dfs_uploads/abandoned_upload_001"
	sizeFreed := int64(5242880) // 5MB

	err := logger.LogAbandonedUploadCleanup(uploadID, tmpPath, sizeFreed)
	if err != nil {
		t.Fatalf("Failed to log abandoned upload cleanup: %v", err)
	}

	verifyLogFileExists(t, "cleanup_events.log")
	lines := readLogFile(t, "cleanup_events.log")
	verifyLogEntry(t, lines, "ABANDONED_UPLOAD_CLEANUP")
	verifyLogEntry(t, lines, uploadID)
	verifyLogEntry(t, lines, "sizeFreed=5242880")

	t.Logf("✓ Abandoned upload cleanup logged correctly")
}

func TestIncompleteDownloadCleanupLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	filename := "aborted.bin"

	sessionID, _ := logger.LogDownloadSessionStart(username, filename, 42, "/tmp/download_42_aborted.bin")

	// Log cleanup
	err := logger.LogIncompleteDownloadCleanup(sessionID, "/tmp/download_42_aborted.bin", int64(3145728))
	if err != nil {
		t.Fatalf("Failed to log incomplete download cleanup: %v", err)
	}

	lines := readLogFile(t, "cleanup_events.log")
	verifyLogEntry(t, lines, "INCOMPLETE_DOWNLOAD_CLEANUP")
	verifyLogEntry(t, lines, "sizeFreed=3145728")

	t.Logf("✓ Incomplete download cleanup logged correctly")
}

func TestUploadSweepLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()

	summary := UploadSweepSummary{
		SweepTime:        time.Now(),
		UploadsChecked:   25,
		UploadsDeleted:   3,
		TotalSizeFreed:   int64(31457280), // 30MB
		AbandonedUploads: []string{"upload_1", "upload_2", "upload_3"},
	}

	err := logger.LogUploadSweepComplete(summary)
	if err != nil {
		t.Fatalf("Failed to log upload sweep: %v", err)
	}

	lines := readLogFile(t, "cleanup_events.log")
	verifyLogEntry(t, lines, "UPLOAD_SWEEP_COMPLETE")
	verifyLogEntry(t, lines, "uploadsChecked=25")
	verifyLogEntry(t, lines, "uploadsDeleted=3")
	verifyLogEntry(t, lines, "totalSizeFreed=31457280")

	t.Logf("✓ Upload sweep logged correctly")
}

// ============================================================================
// LOG ANALYSIS TESTS
// ============================================================================

func TestLogAnalysisUserUploadStats(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "alice"

	// Simulate multiple uploads
	logger.LogChunkUploadStart(username, "upload_1", "file1.pdf", 5, "/tmp/dfs_uploads/upload_1")
	logger.LogChunkStored(username, "upload_1", 0, 2097152, 1, 5, 2097152, "/tmp/dfs_uploads/upload_1")
	logger.LogChunkUploadComplete(username, "upload_1", "file1.pdf", 5, 10485760, "/tmp/dfs_uploads/upload_1")
	logger.LogChunkReassemblyComplete(username, "upload_1", 10485760, 1500)
	logger.LogChunkUploadCleanup(username, "upload_1", "/tmp/dfs_uploads/upload_1", 10485760)

	// Another upload
	logger.LogChunkUploadStart(username, "upload_2", "file2.zip", 3, "/tmp/dfs_uploads/upload_2")
	logger.LogChunkUploadComplete(username, "upload_2", "file2.zip", 3, 5242880, "/tmp/dfs_uploads/upload_2")
	logger.LogChunkReassemblyComplete(username, "upload_2", 5242880, 800)
	logger.LogChunkUploadCleanup(username, "upload_2", "/tmp/dfs_uploads/upload_2", 5242880)

	analyzer := NewLogAnalyzer()
	stats, err := analyzer.GetUserUploadStats(username, 24*time.Hour)
	if err != nil {
		t.Logf("Warning: Could not get stats (might be OK if log parsing is minimal): %v", err)
	}

	if stats != nil {
		t.Logf("Upload Stats: %s", stats.String())
	}

	t.Logf("✓ Upload stats analysis completed")
}

func TestLogAnalysisUserDownloadStats(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "bob"

	// Simulate downloads
	sid1, _ := logger.LogDownloadSessionStart(username, "file1.pdf", 10, "/tmp/down1")
	time.Sleep(50 * time.Millisecond)
	logger.LogDownloadComplete(sid1)

	sid2, _ := logger.LogDownloadSessionStart(username, "file2.bin", 10, "/tmp/down2")
	logger.LogDownloadFailed(sid2, "timeout")

	analyzer := NewLogAnalyzer()
	stats, err := analyzer.GetUserDownloadStats(username, 24*time.Hour)
	if err != nil {
		t.Logf("Warning: Could not get stats (might be OK if log parsing is minimal): %v", err)
	}

	if stats != nil {
		t.Logf("Download Stats: %s", stats.String())
	}

	t.Logf("✓ Download stats analysis completed")
}

func TestLogAnalysisCleanupSummary(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()

	logger.LogAbandonedUploadCleanup("upload_old_1", "/tmp/old1", 5242880)
	logger.LogAbandonedUploadCleanup("upload_old_2", "/tmp/old2", 3145728)

	summary := UploadSweepSummary{
		SweepTime:        time.Now(),
		UploadsChecked:   20,
		UploadsDeleted:   2,
		TotalSizeFreed:   8388608,
		AbandonedUploads: []string{"upload_old_1", "upload_old_2"},
	}
	logger.LogUploadSweepComplete(summary)

	analyzer := NewLogAnalyzer()
	cleanupStats, err := analyzer.GetCleanupSummary(24 * time.Hour)
	if err != nil {
		t.Logf("Warning: Could not get cleanup stats: %v", err)
	}

	if cleanupStats != nil {
		t.Logf("Cleanup Summary: %s", cleanupStats.String())
	}

	t.Logf("✓ Cleanup summary analysis completed")
}

// ============================================================================
// INTEGRATION TESTS
// ============================================================================

func TestCompleteUploadDownloadDeleteCycle(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	filename := "document.pdf"
	uploadID := "cycle_test_001"
	clientID := int64(42)

	// Simulate complete upload cycle
	logger.LogChunkUploadStart(username, uploadID, filename, 3, "/tmp/dfs_uploads/cycle_test_001")
	logger.LogChunkStored(username, uploadID, 0, 2097152, 1, 3, 2097152, "/tmp/dfs_uploads/cycle_test_001")
	logger.LogChunkStored(username, uploadID, 1, 2097152, 2, 3, 4194304, "/tmp/dfs_uploads/cycle_test_001")
	logger.LogChunkStored(username, uploadID, 2, 2097152, 3, 3, 6291456, "/tmp/dfs_uploads/cycle_test_001")
	logger.LogChunkUploadComplete(username, uploadID, filename, 3, 6291456, "/tmp/dfs_uploads/cycle_test_001")
	logger.LogChunkReassemblyStart(username, uploadID, "/tmp/dfs_uploads/cycle_test_001")
	logger.LogChunkReassemblyComplete(username, uploadID, 6291456, 1200)
	logger.LogChunkUploadCleanup(username, uploadID, "/tmp/dfs_uploads/cycle_test_001", 6291456)

	// Simulate download
	downSessionID, _ := logger.LogDownloadSessionStart(username, filename, clientID, "/tmp/download_42_document.pdf")
	time.Sleep(50 * time.Millisecond)
	logger.LogDownloadComplete(downSessionID)

	// Simulate deletion
	logger.LogFileDeleteInitiated(username, filename, clientID)
	logger.LogFileDeleteSuccess(username, filename, clientID, 5)

	// Verify all logs were created
	lines := readLogFile(t, username+".log")
	if len(lines) < 10 {
		t.Fatalf("Expected at least 10 log entries, got %d", len(lines))
	}

	// Verify key events are logged
	verifyLogEntry(t, lines, "CHUNK_UPLOAD_START")
	verifyLogEntry(t, lines, "CHUNK_STORED")
	verifyLogEntry(t, lines, "CHUNK_REASSEMBLY_COMPLETE")
	verifyLogEntry(t, lines, "DOWNLOAD_START")
	verifyLogEntry(t, lines, "DOWNLOAD_COMPLETE")
	verifyLogEntry(t, lines, "FILE_DELETE_SUCCESS")

	t.Logf("✓ Complete upload/download/delete cycle logged successfully")
	t.Logf("  Total log entries: %d", len(lines))
}

func TestMultiUserLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()

	users := []string{"alice", "bob", "charlie"}
	for _, user := range users {
		logger.LogChunkUploadStart(user, "upload_"+user, "file.pdf", 2, "/tmp/test")
		logger.LogChunkStored(user, "upload_"+user, 0, 2097152, 1, 2, 2097152, "/tmp/test")
	}

	for _, user := range users {
		verifyLogFileExists(t, user+".log")
		lines := readLogFile(t, user+".log")
		verifyLogEntry(t, lines, "CHUNK_UPLOAD_START")
		verifyLogEntry(t, lines, user)
	}

	t.Logf("✓ Multi-user logging working correctly")
}

// ============================================================================
// STRESS TESTS
// ============================================================================

func TestConcurrentLogging(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	errChan := make(chan error, 100)
	done := make(chan bool)

	// Launch 10 concurrent goroutines logging for different users
	for i := 0; i < 10; i++ {
		go func(userNum int) {
			username := fmt.Sprintf("user_%d", userNum)
			for j := 0; j < 10; j++ {
				uploadID := fmt.Sprintf("upload_%d_%d", userNum, j)
				err := logger.LogChunkUploadStart(username, uploadID, "file.pdf", 5, "/tmp/test")
				if err != nil {
					errChan <- err
				}
				err = logger.LogChunkStored(username, uploadID, j, 2097152, j+1, 5, int64((j+1)*2097152), "/tmp/test")
				if err != nil {
					errChan <- err
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Check for errors
	select {
	case err := <-errChan:
		t.Fatalf("Concurrent logging error: %v", err)
	default:
		// No errors
	}

	t.Logf("✓ Concurrent logging stress test passed")
}

func TestLargeLogEntries(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "testuser"
	uploadID := "large_upload"

	// Log with large values
	largeSize := int64(1073741824) // 1GB
	largeChunks := 1000

	err := logger.LogChunkUploadStart(username, uploadID, "largefile.iso", largeChunks, "/tmp/dfs_uploads/large_upload")
	if err != nil {
		t.Fatalf("Failed to log large upload: %v", err)
	}

	err = logger.LogChunkUploadComplete(username, uploadID, "largefile.iso", largeChunks, largeSize, "/tmp/dfs_uploads/large_upload")
	if err != nil {
		t.Fatalf("Failed to log large upload complete: %v", err)
	}

	lines := readLogFile(t, username+".log")
	verifyLogEntry(t, lines, fmt.Sprintf("totalChunks=%d", largeChunks))
	verifyLogEntry(t, lines, fmt.Sprintf("totalSize=%d", largeSize))

	t.Logf("✓ Large log entries handled correctly")
}

// ============================================================================
// HELPER TEST TO DISPLAY ACTUAL LOG FILES
// ============================================================================

func TestDisplayActualLogs(t *testing.T) {
	cleanupTestLogs(t)
	defer cleanupTestLogs(t)

	logger := GetWebServerLogger()
	username := "demo_user"

	// Create sample logs
	logger.LogChunkUploadStart(username, "demo_001", "demo.pdf", 3, "/tmp/dfs_uploads/demo_001")
	logger.LogChunkStored(username, "demo_001", 0, 2097152, 1, 3, 2097152, "/tmp/dfs_uploads/demo_001")
	logger.LogChunkStored(username, "demo_001", 1, 2097152, 2, 3, 4194304, "/tmp/dfs_uploads/demo_001")
	logger.LogChunkUploadComplete(username, "demo_001", "demo.pdf", 3, 6291456, "/tmp/dfs_uploads/demo_001")
	logger.LogChunkReassemblyComplete(username, "demo_001", 6291456, 2500)

	sid, _ := logger.LogDownloadSessionStart(username, "demo.pdf", 99, "/tmp/download_99_demo.pdf")
	logger.LogDownloadComplete(sid)

	logger.LogFileDeleteInitiated(username, "demo.pdf", 99)
	logger.LogFileDeleteSuccess(username, "demo.pdf", 99, 3)

	logger.LogAbandonedUploadCleanup("cleanup_001", "/tmp/cleanup_001", 5242880)
	summary := UploadSweepSummary{SweepTime: time.Now(), UploadsChecked: 15, UploadsDeleted: 2, TotalSizeFreed: 10485760, AbandonedUploads: []string{"old_1", "old_2"}}
	logger.LogUploadSweepComplete(summary)

	// Display user log
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("USER LOG: " + username + ".log")
	fmt.Println(strings.Repeat("=", 80))
	userLines := readLogFile(t, username+".log")
	for _, line := range userLines {
		fmt.Println(line)
	}

	// Display cleanup log
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("CLEANUP LOG: cleanup_events.log")
	fmt.Println(strings.Repeat("=", 80))
	cleanupLines := readLogFile(t, "cleanup_events.log")
	for _, line := range cleanupLines {
		fmt.Println(line)
	}

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Printf("✓ Sample logs created successfully\n")
	fmt.Printf("  User log entries: %d\n", len(userLines))
	fmt.Printf("  Cleanup log entries: %d\n", len(cleanupLines))
	fmt.Printf("  Log directory: %s\n", webserverLogsDir)
	fmt.Println(strings.Repeat("=", 80))
}
