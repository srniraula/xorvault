package webserver

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const webserverLogsDir = "webserver_logs"

// WebServerLogger manages per-user and system-wide log files
type WebServerLogger struct {
	mu           sync.Mutex
	logMutex     map[string]*sync.Mutex      // per-user mutex for thread safety
	cleanupMutex *sync.Mutex                 // mutex for cleanup events log
	sessions     map[string]*DownloadSession // active download sessions
	sessionMutex *sync.Mutex
}

var webserverLogger *WebServerLogger
var once sync.Once

// GetWebServerLogger returns singleton instance of WebServerLogger
func GetWebServerLogger() *WebServerLogger {
	once.Do(func() {
		webserverLogger = &WebServerLogger{
			logMutex:     make(map[string]*sync.Mutex),
			cleanupMutex: &sync.Mutex{},
			sessions:     make(map[string]*DownloadSession),
			sessionMutex: &sync.Mutex{},
		}
		// Ensure webserver_logs directory exists
		_ = os.MkdirAll(webserverLogsDir, 0755)
	})
	return webserverLogger
}

// getOrCreateUserMutex returns the mutex for a specific user, creating if needed
func (wl *WebServerLogger) getOrCreateUserMutex(username string) *sync.Mutex {
	wl.mu.Lock()
	defer wl.mu.Unlock()

	if m, exists := wl.logMutex[username]; exists {
		return m
	}
	m := &sync.Mutex{}
	wl.logMutex[username] = m
	return m
}

// writeToUserLog appends a log entry to the user's main log file
func (wl *WebServerLogger) writeToUserLog(username, operation, details string) error {
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	userMutex := wl.getOrCreateUserMutex(username)
	userMutex.Lock()
	defer userMutex.Unlock()

	// Ensure logs directory exists
	if err := os.MkdirAll(webserverLogsDir, 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	logFilePath := filepath.Join(webserverLogsDir, username+".log")

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

// writeToCleanupLog appends a log entry to the system-wide cleanup events log
func (wl *WebServerLogger) writeToCleanupLog(operation, details string) error {
	wl.cleanupMutex.Lock()
	defer wl.cleanupMutex.Unlock()

	// Ensure logs directory exists
	if err := os.MkdirAll(webserverLogsDir, 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	logFilePath := filepath.Join(webserverLogsDir, "cleanup_events.log")

	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open cleanup log: %w", err)
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logEntry := fmt.Sprintf("[%s] %s: %s\n", timestamp, operation, details)

	if _, err := f.WriteString(logEntry); err != nil {
		return fmt.Errorf("failed to write cleanup log: %w", err)
	}

	return nil
}

// ============================================================================
// CHUNK UPLOAD LOGGING
// ============================================================================

// LogChunkUploadStart logs the start of a chunked upload session
// uploadID: unique identifier for this upload session
// username: authenticated user
// filename: name of file being uploaded
// totalChunks: total number of chunks expected
// tmpPath: path where chunks are being stored (/tmp/dfs_uploads/{uploadID})
func (wl *WebServerLogger) LogChunkUploadStart(username, uploadID, filename string, totalChunks int, tmpPath string) error {
	details := fmt.Sprintf("uploadId=%s, filename=%s, totalChunks=%d, tmpPath=%s", uploadID, filename, totalChunks, tmpPath)
	return wl.writeToUserLog(username, "CHUNK_UPLOAD_START", details)
}

// LogChunkStored logs when a single chunk is successfully stored in /tmp/
// uploadID: upload session identifier
// chunkIndex: index of this chunk (0-based)
// chunkSize: size of this chunk in bytes
// uploadedCount: total chunks uploaded so far
// totalSize: cumulative size of all uploaded chunks so far
// tmpPath: path where chunk is stored
func (wl *WebServerLogger) LogChunkStored(username, uploadID string, chunkIndex int, chunkSize int64, uploadedCount, totalChunks int, totalSize int64, tmpPath string) error {
	details := fmt.Sprintf("uploadId=%s, chunkIndex=%d, chunkSize=%d bytes, uploadedChunks=%d/%d, totalSize=%d bytes, tmpPath=%s", uploadID, chunkIndex, chunkSize, uploadedCount, totalChunks, totalSize, tmpPath)
	return wl.writeToUserLog(username, "CHUNK_STORED", details)
}

// LogChunkUploadComplete logs when all chunks for an upload have been received
// and reassembly is about to begin
// uploadID: upload session identifier
// totalChunks: total number of chunks
// totalSize: total file size in bytes
// tmpPath: path containing all chunks
func (wl *WebServerLogger) LogChunkUploadComplete(username, uploadID, filename string, totalChunks int, totalSize int64, tmpPath string) error {
	details := fmt.Sprintf("uploadId=%s, filename=%s, totalChunks=%d, totalSize=%d bytes, tmpPath=%s", uploadID, filename, totalChunks, totalSize, tmpPath)
	return wl.writeToUserLog(username, "CHUNK_UPLOAD_COMPLETE", details)
}

// LogChunkReassemblyStart logs when reassembly of chunks begins
func (wl *WebServerLogger) LogChunkReassemblyStart(username, uploadID string, tmpPath string) error {
	details := fmt.Sprintf("uploadId=%s, tmpPath=%s", uploadID, tmpPath)
	return wl.writeToUserLog(username, "CHUNK_REASSEMBLY_START", details)
}

// LogChunkReassemblyComplete logs when chunks have been reassembled into single file
// totalSize: total size of reassembled file
// reassemblyTime: time taken for reassembly in milliseconds
func (wl *WebServerLogger) LogChunkReassemblyComplete(username, uploadID string, totalSize int64, reassemblyTimeMs int64) error {
	details := fmt.Sprintf("uploadId=%s, totalSize=%d bytes, reassemblyTimeMs=%d", uploadID, totalSize, reassemblyTimeMs)
	return wl.writeToUserLog(username, "CHUNK_REASSEMBLY_COMPLETE", details)
}

// LogChunkUploadCleanup logs when chunk temporary files are cleaned up
// uploadID: upload session identifier
// tmpPath: path to chunks directory
// sizeFreed: total bytes freed
func (wl *WebServerLogger) LogChunkUploadCleanup(username, uploadID string, tmpPath string, sizeFreed int64) error {
	details := fmt.Sprintf("uploadId=%s, tmpPath=%s, sizeFreed=%d bytes", uploadID, tmpPath, sizeFreed)
	return wl.writeToUserLog(username, "CHUNK_CLEANUP", details)

}

// ============================================================================
// SIMPLE UPLOAD LOGGING (for files < 10MB, non-chunked)
// ============================================================================

// LogSimpleUploadStart logs the start of a simple file upload
// filename: name of file being uploaded
// fileSize: size of file in bytes
func (wl *WebServerLogger) LogSimpleUploadStart(username, filename string, fileSize int64) error {
	details := fmt.Sprintf("filename=%s, fileSize=%d bytes", filename, fileSize)
	return wl.writeToUserLog(username, "SIMPLE_UPLOAD_START", details)
}

// LogSimpleUploadComplete logs when a simple file upload completes successfully
// filename: name of file uploaded
// fileSize: size of file in bytes
func (wl *WebServerLogger) LogSimpleUploadComplete(username, filename string, fileSize int64) error {
	details := fmt.Sprintf("filename=%s, fileSize=%d bytes", filename, fileSize)
	return wl.writeToUserLog(username, "SIMPLE_UPLOAD_COMPLETE", details)
}

// LogSimpleUploadFailed logs when a simple file upload fails
// filename: name of file that failed to upload
// fileSize: size of file in bytes
// reason: reason for failure
func (wl *WebServerLogger) LogSimpleUploadFailed(username, filename string, fileSize int64, reason string) error {
	details := fmt.Sprintf("filename=%s, fileSize=%d bytes, reason=%s", filename, fileSize, reason)
	return wl.writeToUserLog(username, "SIMPLE_UPLOAD_FAILED", details)
}

// ============================================================================
// DOWNLOAD SESSION LOGGING
// ============================================================================

// LogDownloadSessionStart logs the start of a file download
// Returns a session ID to track this download
func (wl *WebServerLogger) LogDownloadSessionStart(username, filename string, clientID int64, tmpPath string) (string, error) {
	sessionID := fmt.Sprintf("sess_%d_%s_%d", clientID, filename, time.Now().Unix())

	session := &DownloadSession{
		SessionID:        sessionID,
		Username:         username,
		Filename:         filename,
		ClientID:         clientID,
		TmpPath:          tmpPath,
		StartTime:        time.Now(),
		Status:           "started",
		LastProgressTime: time.Now(),
	}

	// Store session in memory for tracking
	wl.sessionMutex.Lock()
	wl.sessions[sessionID] = session
	wl.sessionMutex.Unlock()

	details := fmt.Sprintf("sessionId=%s, filename=%s, clientId=%d, tmpPath=%s", sessionID, filename, clientID, tmpPath)
	err := wl.writeToUserLog(username, "DOWNLOAD_START", details)
	return sessionID, err
}

// LogDownloadProgress logs download progress
func (wl *WebServerLogger) LogDownloadProgress(username, sessionID string, bytesDownloaded, totalBytes int64) error {
	wl.sessionMutex.Lock()
	session, exists := wl.sessions[sessionID]
	if !exists {
		wl.sessionMutex.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}
	session.BytesDownloaded = bytesDownloaded
	session.TotalBytes = totalBytes
	session.LastProgressTime = time.Now()
	wl.sessionMutex.Unlock()

	// Only log every 10% or every 30 seconds to avoid too many logs
	progressPercent := int((bytesDownloaded * 100) / totalBytes)
	if progressPercent%10 == 0 || time.Since(session.LastProgressTime) > 30*time.Second {
		details := fmt.Sprintf("sessionId=%s, bytesDownloaded=%d, totalBytes=%d, progress=%d%%", sessionID, bytesDownloaded, totalBytes, progressPercent)
		return wl.writeToUserLog(username, "DOWNLOAD_PROGRESS", details)
	}
	return nil
}

// LogDownloadComplete logs when a download is successfully completed
func (wl *WebServerLogger) LogDownloadComplete(sessionID string) error {
	wl.sessionMutex.Lock()
	session, exists := wl.sessions[sessionID]
	if !exists {
		wl.sessionMutex.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}
	session.Status = "completed"
	session.EndTime = time.Now()
	duration := session.EndTime.Sub(session.StartTime).Milliseconds()
	wl.sessionMutex.Unlock()

	details := fmt.Sprintf("sessionId=%s, totalBytes=%d, durationMs=%d", sessionID, session.BytesDownloaded, duration)
	return wl.writeToUserLog(session.Username, "DOWNLOAD_COMPLETE", details)
}

// LogDownloadAborted logs when a download is aborted (browser closed, connection lost, etc.)
// reason: why the download was aborted
func (wl *WebServerLogger) LogDownloadAborted(sessionID, reason string) error {
	wl.sessionMutex.Lock()
	session, exists := wl.sessions[sessionID]
	if !exists {
		wl.sessionMutex.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}
	session.Status = "aborted"
	session.ErrorMsg = reason
	session.EndTime = time.Now()
	duration := session.EndTime.Sub(session.StartTime).Milliseconds()
	wl.sessionMutex.Unlock()

	details := fmt.Sprintf("sessionId=%s, reason=%s, bytesDownloaded=%d, durationMs=%d, tmpPath=%s", sessionID, reason, session.BytesDownloaded, duration, session.TmpPath)
	return wl.writeToUserLog(session.Username, "DOWNLOAD_ABORTED", details)
}

// LogDownloadFailed logs when a download fails (error occurred)
func (wl *WebServerLogger) LogDownloadFailed(sessionID, errorMsg string) error {
	wl.sessionMutex.Lock()
	session, exists := wl.sessions[sessionID]
	if !exists {
		wl.sessionMutex.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}
	session.Status = "failed"
	session.ErrorMsg = errorMsg
	session.EndTime = time.Now()
	wl.sessionMutex.Unlock()

	details := fmt.Sprintf("sessionId=%s, error=%s, tmpPath=%s", sessionID, errorMsg, session.TmpPath)
	return wl.writeToUserLog(session.Username, "DOWNLOAD_FAILED", details)
}

// ============================================================================
// FILE OPERATIONS LOGGING
// ============================================================================

// LogFileDeleteInitiated logs when a file delete operation is requested
func (wl *WebServerLogger) LogFileDeleteInitiated(username, filename string, clientID int64) error {
	details := fmt.Sprintf("filename=%s, clientId=%d", filename, clientID)
	return wl.writeToUserLog(username, "FILE_DELETE_INITIATED", details)
}

// LogFileDeleteSuccess logs when a file and its chunks are successfully deleted
// chunksDeleted: number of chunks removed
func (wl *WebServerLogger) LogFileDeleteSuccess(username, filename string, clientID int64, chunksDeleted int) error {
	details := fmt.Sprintf("filename=%s, clientId=%d, chunksDeleted=%d", filename, clientID, chunksDeleted)
	return wl.writeToUserLog(username, "FILE_DELETE_SUCCESS", details)
}

// LogFileDeleteFailed logs when a file delete operation fails
func (wl *WebServerLogger) LogFileDeleteFailed(username, filename string, clientID int64, errorMsg string) error {
	details := fmt.Sprintf("filename=%s, clientId=%d, error=%s", filename, clientID, errorMsg)
	return wl.writeToUserLog(username, "FILE_DELETE_FAILED", details)
}

// ============================================================================
// CLEANUP OPERATIONS LOGGING
// ============================================================================

// LogIncompleteDownloadCleanup logs when an incomplete download is cleaned from /tmp/
// sessionID: download session that was cleaned
// tmpPath: path to temporary file
// sizeFreed: bytes freed
func (wl *WebServerLogger) LogIncompleteDownloadCleanup(sessionID, tmpPath string, sizeFreed int64) error {
	wl.sessionMutex.Lock()
	session, exists := wl.sessions[sessionID]
	wl.sessionMutex.Unlock()

	if !exists {
		// Cleanup log for unknown session
		return wl.writeToCleanupLog("INCOMPLETE_DOWNLOAD_CLEANUP", fmt.Sprintf("sessionId=%s, tmpPath=%s, sizeFreed=%d bytes", sessionID, tmpPath, sizeFreed))
	}

	// Clean up session from memory after logging
	wl.sessionMutex.Lock()
	delete(wl.sessions, sessionID)
	wl.sessionMutex.Unlock()

	details := fmt.Sprintf("sessionId=%s, tmpPath=%s, sizeFreed=%d bytes, username=%s", sessionID, tmpPath, sizeFreed, session.Username)
	return wl.writeToCleanupLog("INCOMPLETE_DOWNLOAD_CLEANUP", details)
}

// LogDownloadCleanup logs when a successful download is cleaned up from /tmp/
// sessionID: download session that was cleaned
// tmpPath: path to temporary file
// sizeFreed: bytes freed
func (wl *WebServerLogger) LogDownloadCleanup(sessionID, tmpPath string, sizeFreed int64) error {
	wl.sessionMutex.Lock()
	session, exists := wl.sessions[sessionID]
	wl.sessionMutex.Unlock()

	if !exists {
		// Cleanup log for unknown session
		return wl.writeToCleanupLog("DOWNLOAD_CLEANUP", fmt.Sprintf("sessionId=%s, tmpPath=%s, sizeFreed=%d bytes", sessionID, tmpPath, sizeFreed))
	}

	// Clean up session from memory after logging
	wl.sessionMutex.Lock()
	delete(wl.sessions, sessionID)
	wl.sessionMutex.Unlock()

	// Log to user log (not cleanup.log)
	details := fmt.Sprintf("sessionId=%s, tmpPath=%s, sizeFreed=%d bytes", sessionID, tmpPath, sizeFreed)
	return wl.writeToUserLog(session.Username, "DOWNLOAD_CLEANUP", details)
}

// LogAbandonedUploadCleanup logs when an abandoned upload is cleaned from /tmp/
// uploadID: upload session identifier
// tmpPath: path to upload directory
// sizeFreed: bytes freed
func (wl *WebServerLogger) LogAbandonedUploadCleanup(uploadID, tmpPath string, sizeFreed int64) error {
	details := fmt.Sprintf("uploadId=%s, tmpPath=%s, sizeFreed=%d bytes", uploadID, tmpPath, sizeFreed)
	return wl.writeToCleanupLog("ABANDONED_UPLOAD_CLEANUP", details)
}

// LogUploadSweepComplete logs completion of an upload sweep operation
// summary: UploadSweepSummary with sweep statistics
func (wl *WebServerLogger) LogUploadSweepComplete(summary UploadSweepSummary) error {
	details := fmt.Sprintf("uploadsChecked=%d, uploadsDeleted=%d, totalSizeFreed=%d bytes, deletedIds=%v", summary.UploadsChecked, summary.UploadsDeleted, summary.TotalSizeFreed, summary.AbandonedUploads)
	return wl.writeToCleanupLog("UPLOAD_SWEEP_COMPLETE", details)
}

// LogChunkUploadAbandoned logs when an upload is abandoned by tracking cleanup
// uploadID: upload session identifier
// tmpPath: path to upload directory
// sizeFreed: bytes freed
func (wl *WebServerLogger) LogChunkUploadAbandoned(username, uploadID string, tmpPath string, sizeFreed int64) error {
	details := fmt.Sprintf("uploadId=%s, tmpPath=%s, sizeFreed=%d bytes", uploadID, tmpPath, sizeFreed)
	return wl.writeToUserLog(username, "CHUNK_UPLOAD_ABANDONED", details)
}

// ============================================================================
// SESSION MANAGEMENT
// ============================================================================

// GetDownloadSession returns a download session by ID
func (wl *WebServerLogger) GetDownloadSession(sessionID string) *DownloadSession {
	wl.sessionMutex.Lock()
	defer wl.sessionMutex.Unlock()
	return wl.sessions[sessionID]
}

// CloseDownloadSession closes and removes a download session from tracking
func (wl *WebServerLogger) CloseDownloadSession(sessionID string) {
	wl.sessionMutex.Lock()
	defer wl.sessionMutex.Unlock()
	delete(wl.sessions, sessionID)
}

// ============================================================================
// TEST HELPERS (for testing only)
// ============================================================================

// ResetLoggerForTesting resets the logger singleton. Only use for testing!
func ResetLoggerForTesting() {
	once = sync.Once{}
	webserverLogger = nil
}
