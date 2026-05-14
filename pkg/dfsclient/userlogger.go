package dfsclient

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const clientLogsDir = "client_logs"

// UserLogger manages per-user log files for tracking operations in the DFS
type UserLogger struct {
	mu       sync.Mutex
	logMutex map[string]*sync.Mutex // per-user mutex for thread safety
}

var userLogger *UserLogger
var once sync.Once

// GetUserLogger returns the singleton instance of UserLogger
func GetUserLogger() *UserLogger {
	once.Do(func() {
		userLogger = &UserLogger{
			logMutex: make(map[string]*sync.Mutex),
		}
		_ = os.MkdirAll(clientLogsDir, 0755)
	})
	return userLogger
}

// getOrCreateUserMutex returns the mutex for a specific user, creating if needed
func (ul *UserLogger) getOrCreateUserMutex(username string) *sync.Mutex {
	ul.mu.Lock()
	defer ul.mu.Unlock()

	if m, exists := ul.logMutex[username]; exists {
		return m
	}
	m := &sync.Mutex{}
	ul.logMutex[username] = m
	return m
}

// WriteLog appends a log entry to the user's log file
// Format: [timestamp] operation: details
func (ul *UserLogger) WriteLog(username, operation, details string) error {
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	userMutex := ul.getOrCreateUserMutex(username)
	userMutex.Lock()
	defer userMutex.Unlock()

	logFilePath := filepath.Join(clientLogsDir, username+".log")

	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file for user %s: %w", username, err)
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logEntry := fmt.Sprintf("[%s] %s: %s\n", timestamp, operation, details)

	if _, err := f.WriteString(logEntry); err != nil {
		return fmt.Errorf("failed to write log for user %s: %w", username, err)
	}

	return nil
}

func (ul *UserLogger) LogChunkUploadStart(username, filename string) error {
	details := fmt.Sprintf("file=%s", filename)
	return ul.WriteLog(username, "UPLOAD_START", details)
}

func (ul *UserLogger) LogChunkUploaded(username, filename, chunkID string, chunkSize int64, stripeNum int) error {
	details := fmt.Sprintf("file=%s, chunk_id=%s, stripe=%d, size=%d", filename, chunkID, stripeNum, chunkSize)
	return ul.WriteLog(username, "CHUNK_UPLOADED", details)
}

func (ul *UserLogger) LogChunkUploadFailed(username, filename, chunkID string, stripeNum int, errMsg string) error {
	details := fmt.Sprintf("file=%s, chunk_id=%s, stripe=%d, error=%s", filename, chunkID, stripeNum, errMsg)
	return ul.WriteLog(username, "CHUNK_UPLOAD_FAILED", details)
}

func (ul *UserLogger) LogFileUploadComplete(username, filename string, totalStripes int) error {
	details := fmt.Sprintf("file=%s, total_stripes=%d", filename, totalStripes)
	return ul.WriteLog(username, "UPLOAD_COMPLETE", details)
}

func (ul *UserLogger) LogDownloadStart(username, filename string) error {
	details := fmt.Sprintf("file=%s", filename)
	return ul.WriteLog(username, "DOWNLOAD_START", details)
}

func (ul *UserLogger) LogChunkDownloaded(username, filename, chunkID string, chunkSize int64, stripeNum int) error {
	details := fmt.Sprintf("file=%s, chunk_id=%s, stripe=%d, size=%d", filename, chunkID, stripeNum, chunkSize)
	return ul.WriteLog(username, "CHUNK_DOWNLOADED", details)
}

func (ul *UserLogger) LogChunkDownloadFailed(username, filename, chunkID string, stripeNum int, errMsg string) error {
	details := fmt.Sprintf("file=%s, chunk_id=%s, stripe=%d, error=%s", filename, chunkID, stripeNum, errMsg)
	return ul.WriteLog(username, "CHUNK_DOWNLOAD_FAILED", details)
}

func (ul *UserLogger) LogFileDownloadComplete(username, filename string, totalStripes int) error {
	details := fmt.Sprintf("file=%s, total_stripes=%d", filename, totalStripes)
	return ul.WriteLog(username, "DOWNLOAD_COMPLETE", details)
}

func (ul *UserLogger) LogFileDeleted(username, filename string, chunksDeleted int) error {
	details := fmt.Sprintf("file=%s, chunks_deleted=%d", filename, chunksDeleted)
	return ul.WriteLog(username, "FILE_DELETED", details)
}

func (ul *UserLogger) LogFileRefresh(username string, fileCount int) error {
	details := fmt.Sprintf("file_count=%d", fileCount)
	return ul.WriteLog(username, "FILE_REFRESH", details)
}
