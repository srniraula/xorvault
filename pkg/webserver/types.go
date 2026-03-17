package webserver

import "time"

// ChunkStorageEvent represents a chunk upload event
type ChunkStorageEvent struct {
	UploadID   string
	Filename   string
	ChunkIndex int
	ChunkSize  int64
	TmpPath    string
	EventTime  time.Time
}

// ChunkStorageSummary tracks overall upload progress
type ChunkStorageSummary struct {
	UploadID       string
	Filename       string
	TotalChunks    int
	UploadedChunks int
	TotalSize      int64
	TmpPath        string
	CreatedAt      time.Time
	LastUpdated    time.Time
}

// DownloadSession tracks a single download operation
type DownloadSession struct {
	SessionID        string
	Username         string
	Filename         string
	ClientID         int64
	TmpPath          string
	StartTime        time.Time
	EndTime          time.Time
	Status           string // "started", "in_progress", "completed", "aborted", "failed"
	BytesDownloaded  int64
	TotalBytes       int64
	ErrorMsg         string
	LastProgressTime time.Time
}

// FileDeleteEvent represents a file deletion operation
type FileDeleteEvent struct {
	Username      string
	Filename      string
	ClientID      int64
	ChunksDeleted int
	DeleteTime    time.Time
}

// CleanupEvent represents cleanup operations for abandoned resources
type CleanupEvent struct {
	EventType    string // "upload_abandoned", "download_incomplete", "upload_sweep"
	TmpPath      string
	FilesDeleted int
	SizeFreed    int64
	CleanupTime  time.Time
	Details      string // Additional details
}

// UploadSweepSummary summarizes a sweep operation
type UploadSweepSummary struct {
	SweepTime        time.Time
	UploadsChecked   int
	UploadsDeleted   int
	TotalSizeFreed   int64
	AbandonedUploads []string // IDs of deleted uploads
}
