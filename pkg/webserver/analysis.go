package webserver

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LogAnalyzer provides utilities for analyzing and querying webserver logs
type LogAnalyzer struct {
	logsDir string
}

// NewLogAnalyzer creates a new log analyzer
func NewLogAnalyzer() *LogAnalyzer {
	return &LogAnalyzer{
		logsDir: webserverLogsDir,
	}
}

// UserUploadStats contains statistics about a user's uploads
type UserUploadStats struct {
	Username          string
	TotalUploads      int
	CompletedUploads  int
	AbandonedUploads  int
	TotalChunksStored int
	TotalBytesStored  int64
	TotalSpaceFreed   int64
	AvgReassemblyMs   int64
	FirstUploadTime   time.Time
	LastUploadTime    time.Time
}

// UserDownloadStats contains statistics about a user's downloads
type UserDownloadStats struct {
	Username             string
	TotalDownloads       int
	SuccessfulDownloads  int
	FailedDownloads      int
	AbortedDownloads     int
	TotalBytesDownloaded int64
	AvgDownloadTimeMs    int64
	FirstDownloadTime    time.Time
	LastDownloadTime     time.Time
}

// UserFileStats contains statistics about a user's file operations
type UserFileStats struct {
	Username           string
	FilesDeleted       int
	TotalChunksDeleted int
	FirstDeleteTime    time.Time
	LastDeleteTime     time.Time
}

// GetUserUploadStats analyzes user's upload activity from logs
func (la *LogAnalyzer) GetUserUploadStats(username string, within time.Duration) (*UserUploadStats, error) {
	logFile := filepath.Join(la.logsDir, username+".log")
	stats := &UserUploadStats{
		Username: username,
	}

	file, err := os.Open(logFile)
	if err != nil {
		return stats, fmt.Errorf("could not open log file: %w", err)
	}
	defer file.Close()

	cutoff := time.Now().Add(-within)
	scanner := bufio.NewScanner(file)
	var totalReassemblyMs int64 = 0
	var reassemblyCount int = 0

	for scanner.Scan() {
		line := scanner.Text()

		// Parse timestamp
		if len(line) < 20 {
			continue
		}
		timestampStr := line[1:20] // Format: "2006-01-02 15:04:05"
		timestamp, err := time.Parse("2006-01-02 15:04:05", timestampStr)
		if err != nil {
			continue
		}

		if timestamp.Before(cutoff) {
			continue
		}

		// Extract operation type
		if !strings.Contains(line, ":") {
			continue
		}

		parts := strings.Split(line, ": ")
		if len(parts) < 2 {
			continue
		}

		operation := strings.TrimSpace(strings.TrimPrefix(parts[0], "]"))

		// Update first/last times
		if stats.FirstUploadTime.IsZero() {
			stats.FirstUploadTime = timestamp
		}
		stats.LastUploadTime = timestamp

		// Parse operations
		switch {
		case strings.Contains(operation, "CHUNK_UPLOAD_START"):
			stats.TotalUploads++
		case strings.Contains(operation, "CHUNK_STORED"):
			// Extract size from details
			if strings.Contains(line, "chunkSize=") {
				sizeStr := extractValue(line, "chunkSize=")
				if size, err := strconv.ParseInt(strings.TrimSuffix(sizeStr, " bytes"), 10, 64); err == nil {
					stats.TotalBytesStored += size
				}
			}
		case strings.Contains(operation, "CHUNK_REASSEMBLY_COMPLETE"):
			if strings.Contains(line, "reassemblyTimeMs=") {
				msStr := extractValue(line, "reassemblyTimeMs=")
				if ms, err := strconv.ParseInt(msStr, 10, 64); err == nil {
					totalReassemblyMs += ms
					reassemblyCount++
				}
			}
		case strings.Contains(operation, "CHUNK_UPLOAD_ABANDONED"):
			stats.AbandonedUploads++
		case strings.Contains(operation, "CHUNK_CLEANUP"):
			if strings.Contains(line, "sizeFreed=") {
				sizeStr := extractValue(line, "sizeFreed=")
				if size, err := strconv.ParseInt(strings.TrimSuffix(sizeStr, " bytes"), 10, 64); err == nil {
					stats.TotalSpaceFreed += size
				}
			}
		}
	}

	if reassemblyCount > 0 {
		stats.AvgReassemblyMs = totalReassemblyMs / int64(reassemblyCount)
	}

	stats.CompletedUploads = stats.TotalUploads - stats.AbandonedUploads

	return stats, scanner.Err()
}

// GetUserDownloadStats analyzes user's download activity from logs
func (la *LogAnalyzer) GetUserDownloadStats(username string, within time.Duration) (*UserDownloadStats, error) {
	logFile := filepath.Join(la.logsDir, username+".log")
	stats := &UserDownloadStats{
		Username: username,
	}

	file, err := os.Open(logFile)
	if err != nil {
		return stats, fmt.Errorf("could not open log file: %w", err)
	}
	defer file.Close()

	cutoff := time.Now().Add(-within)
	scanner := bufio.NewScanner(file)
	var totalDownloadTimeMs int64 = 0
	var downloadCount int = 0

	for scanner.Scan() {
		line := scanner.Text()

		if len(line) < 20 {
			continue
		}

		timestampStr := line[1:20]
		timestamp, err := time.Parse("2006-01-02 15:04:05", timestampStr)
		if err != nil {
			continue
		}

		if timestamp.Before(cutoff) {
			continue
		}

		parts := strings.Split(line, ": ")
		if len(parts) < 2 {
			continue
		}

		operation := strings.TrimSpace(strings.TrimPrefix(parts[0], "]"))

		if stats.FirstDownloadTime.IsZero() {
			stats.FirstDownloadTime = timestamp
		}
		stats.LastDownloadTime = timestamp

		switch {
		case strings.Contains(operation, "DOWNLOAD_START"):
			stats.TotalDownloads++
		case strings.Contains(operation, "DOWNLOAD_COMPLETE"):
			stats.SuccessfulDownloads++
			downloadCount++
			if strings.Contains(line, "totalBytes=") && strings.Contains(line, "durationMs=") {
				bytesStr := extractValue(line, "totalBytes=")
				durationStr := extractValue(line, "durationMs=")
				if bytes, err := strconv.ParseInt(bytesStr, 10, 64); err == nil {
					stats.TotalBytesDownloaded += bytes
				}
				if duration, err := strconv.ParseInt(durationStr, 10, 64); err == nil {
					totalDownloadTimeMs += duration
				}
			}
		case strings.Contains(operation, "DOWNLOAD_FAILED"):
			stats.FailedDownloads++
		case strings.Contains(operation, "DOWNLOAD_ABORTED"):
			stats.AbortedDownloads++
		}
	}

	if downloadCount > 0 {
		stats.AvgDownloadTimeMs = totalDownloadTimeMs / int64(downloadCount)
	}

	return stats, scanner.Err()
}

// GetUserFileStats analyzes file operations for a user
func (la *LogAnalyzer) GetUserFileStats(username string, within time.Duration) (*UserFileStats, error) {
	logFile := filepath.Join(la.logsDir, username+".log")
	stats := &UserFileStats{
		Username: username,
	}

	file, err := os.Open(logFile)
	if err != nil {
		return stats, fmt.Errorf("could not open log file: %w", err)
	}
	defer file.Close()

	cutoff := time.Now().Add(-within)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		if len(line) < 20 {
			continue
		}

		timestampStr := line[1:20]
		timestamp, err := time.Parse("2006-01-02 15:04:05", timestampStr)
		if err != nil {
			continue
		}

		if timestamp.Before(cutoff) {
			continue
		}

		parts := strings.Split(line, ": ")
		if len(parts) < 2 {
			continue
		}

		operation := strings.TrimSpace(strings.TrimPrefix(parts[0], "]"))

		if strings.Contains(operation, "FILE_DELETE_SUCCESS") {
			stats.FilesDeleted++
			if stats.FirstDeleteTime.IsZero() {
				stats.FirstDeleteTime = timestamp
			}
			stats.LastDeleteTime = timestamp

			if strings.Contains(line, "chunksDeleted=") {
				chunksStr := extractValue(line, "chunksDeleted=")
				if chunks, err := strconv.Atoi(chunksStr); err == nil {
					stats.TotalChunksDeleted += chunks
				}
			}
		}
	}

	return stats, scanner.Err()
}

// GetCleanupSummary analyzes cleanup operations
type CleanupSummary struct {
	AbandonedUploadsCleaned    int
	IncompleteDownloadsCleaned int
	TotalSpaceFreed            int64
	UploadSweepsRan            int
	LastCleanupTime            time.Time
}

func (la *LogAnalyzer) GetCleanupSummary(within time.Duration) (*CleanupSummary, error) {
	logFile := filepath.Join(la.logsDir, "cleanup_events.log")
	summary := &CleanupSummary{}

	file, err := os.Open(logFile)
	if err != nil {
		// Log file might not exist yet
		return summary, nil
	}
	defer file.Close()

	cutoff := time.Now().Add(-within)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		if len(line) < 20 {
			continue
		}

		timestampStr := line[1:20]
		timestamp, err := time.Parse("2006-01-02 15:04:05", timestampStr)
		if err != nil {
			continue
		}

		if timestamp.Before(cutoff) {
			continue
		}

		summary.LastCleanupTime = timestamp

		if strings.Contains(line, "ABANDONED_UPLOAD_CLEANUP") {
			summary.AbandonedUploadsCleaned++
			if strings.Contains(line, "sizeFreed=") {
				sizeStr := extractValue(line, "sizeFreed=")
				if size, err := strconv.ParseInt(strings.TrimSuffix(sizeStr, " bytes"), 10, 64); err == nil {
					summary.TotalSpaceFreed += size
				}
			}
		} else if strings.Contains(line, "INCOMPLETE_DOWNLOAD_CLEANUP") {
			summary.IncompleteDownloadsCleaned++
			if strings.Contains(line, "sizeFreed=") {
				sizeStr := extractValue(line, "sizeFreed=")
				if size, err := strconv.ParseInt(strings.TrimSuffix(sizeStr, " bytes"), 10, 64); err == nil {
					summary.TotalSpaceFreed += size
				}
			}
		} else if strings.Contains(line, "UPLOAD_SWEEP_COMPLETE") {
			summary.UploadSweepsRan++
		}
	}

	return summary, scanner.Err()
}

// extractValue extracts a value from a log line given a key
// Example: extractValue("key=value, other=data", "key=") returns "value"
func extractValue(line, key string) string {
	idx := strings.Index(line, key)
	if idx == -1 {
		return ""
	}

	start := idx + len(key)
	remaining := line[start:]

	// Find end of value (space, comma, or end of string)
	endIdx := strings.IndexAny(remaining, ", ")
	if endIdx == -1 {
		return remaining
	}

	return remaining[:endIdx]
}

// PrintUserStats prints a formatted summary of user stats
func (us *UserUploadStats) String() string {
	return fmt.Sprintf(`
User Upload Statistics: %s
  Total Uploads: %d
  Completed: %d
  Abandoned: %d
  Total Bytes Stored: %d
  Total Space Freed: %d
  Avg Reassembly Time: %dms
  Time Range: %s to %s`,
		us.Username,
		us.TotalUploads,
		us.CompletedUploads,
		us.AbandonedUploads,
		us.TotalBytesStored,
		us.TotalSpaceFreed,
		us.AvgReassemblyMs,
		us.FirstUploadTime.Format("2006-01-02 15:04"),
		us.LastUploadTime.Format("2006-01-02 15:04"),
	)
}

func (ds *UserDownloadStats) String() string {
	return fmt.Sprintf(`
User Download Statistics: %s
  Total Downloads: %d
  Successful: %d
  Failed: %d
  Aborted: %d
  Total Bytes Downloaded: %d
  Avg Download Time: %dms
  Time Range: %s to %s`,
		ds.Username,
		ds.TotalDownloads,
		ds.SuccessfulDownloads,
		ds.FailedDownloads,
		ds.AbortedDownloads,
		ds.TotalBytesDownloaded,
		ds.AvgDownloadTimeMs,
		ds.FirstDownloadTime.Format("2006-01-02 15:04"),
		ds.LastDownloadTime.Format("2006-01-02 15:04"),
	)
}

func (fs *UserFileStats) String() string {
	return fmt.Sprintf(`
User File Delete Statistics: %s
  Files Deleted: %d
  Total Chunks Deleted: %d
  Time Range: %s to %s`,
		fs.Username,
		fs.FilesDeleted,
		fs.TotalChunksDeleted,
		fs.FirstDeleteTime.Format("2006-01-02 15:04"),
		fs.LastDeleteTime.Format("2006-01-02 15:04"),
	)
}

func (cs *CleanupSummary) String() string {
	return fmt.Sprintf(`
Cleanup Summary
  Abandoned Uploads Cleaned: %d
  Incomplete Downloads Cleaned: %d
  Total Space Freed: %d bytes
  Upload Sweeps Run: %d
  Last Cleanup: %s`,
		cs.AbandonedUploadsCleaned,
		cs.IncompleteDownloadsCleaned,
		cs.TotalSpaceFreed,
		cs.UploadSweepsRan,
		cs.LastCleanupTime.Format("2006-01-02 15:04:05"),
	)
}
