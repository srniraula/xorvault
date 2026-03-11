package main

import (
	"fmt"
	"sync"
	"time"
)

// MetricsCollector collects detailed metrics for file operations
type MetricsCollector struct {
	mu sync.Mutex

	// Timing
	StartTime         time.Time
	EndTime           time.Time
	TotalDuration     time.Duration
	MasterCallLatency time.Duration

	// Upload metrics
	TotalBytesUploaded   int64
	TotalChunksUploaded  int
	ChunkUploadDuration  time.Duration
	ParityCalcDuration   time.Duration
	ChecksumCalcDuration time.Duration

	// Download metrics
	TotalBytesDownloaded   int64
	TotalChunksDownloaded  int
	ChunkDownloadDuration  time.Duration
	ReconstructionCount    int
	ReconstructionDuration time.Duration

	// Checksum metrics
	TotalChecksumVerified   int
	FailedChecksumVerify    int
	ChecksumMismatchDetails []string
	CorruptionRecoveryCount int

	// Network metrics
	TotalNetworkLatency   time.Duration
	MaxNetworkLatency     time.Duration
	MinNetworkLatency     time.Duration
	AverageNetworkLatency time.Duration
	NetworkLatencies      []time.Duration
	GrpcConnectionTime    time.Duration
	GrpcDisconnectionTime time.Duration

	// Parity recovery metrics
	ParityChunksUsed     int
	DataRecoverySuccess  int
	DataRecoveryFailures int

	// ACK metrics
	TotalAckReceived int
	TotalAckTimeout  int

	// Delete operation metrics
	DeleteChunkCount int

	// List operation metrics
	FilesListed int

	// Error tracking
	Errors []string
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		StartTime:               time.Now(),
		NetworkLatencies:        make([]time.Duration, 0),
		ChecksumMismatchDetails: make([]string, 0),
		Errors:                  make([]string, 0),
	}
}

// RecordOperationStart marks the start of an operation
func (m *MetricsCollector) RecordOperationStart() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StartTime = time.Now()
}

// RecordOperationEnd marks the end of an operation
func (m *MetricsCollector) RecordOperationEnd() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EndTime = time.Now()
	m.TotalDuration = m.EndTime.Sub(m.StartTime)
}

// RecordMasterCallLatency records latency of master RPC call
func (m *MetricsCollector) RecordMasterCallLatency(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.MasterCallLatency = duration
	m.recordNetworkLatency(duration)
}

// RecordChunkUpload records metrics for a chunk upload
func (m *MetricsCollector) RecordChunkUpload(bytes int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalBytesUploaded += int64(bytes)
	m.TotalChunksUploaded++
	m.ChunkUploadDuration += duration
	m.recordNetworkLatency(duration)
}

// RecordChunkDownload records metrics for a chunk download
func (m *MetricsCollector) RecordChunkDownload(bytes int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalBytesDownloaded += int64(bytes)
	m.TotalChunksDownloaded++
	m.ChunkDownloadDuration += duration
	m.recordNetworkLatency(duration)
}

// RecordParityCalculation records time spent calculating parity
func (m *MetricsCollector) RecordParityCalculation(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ParityCalcDuration += duration
}

// RecordChecksumCalculation records time spent calculating checksums
func (m *MetricsCollector) RecordChecksumCalculation(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ChecksumCalcDuration += duration
	m.TotalChecksumVerified++
}

// RecordChecksumMismatch records a checksum verification failure
func (m *MetricsCollector) RecordChecksumMismatch(chunkID, expected, actual string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FailedChecksumVerify++
	detail := fmt.Sprintf("Chunk %s: expected=%s, actual=%s", chunkID, expected, actual)
	m.ChecksumMismatchDetails = append(m.ChecksumMismatchDetails, detail)
}

// RecordReconstruction records a parity-based reconstruction
func (m *MetricsCollector) RecordReconstruction(chunkID string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReconstructionCount++
	m.ReconstructionDuration += duration
	m.ParityChunksUsed++
	m.DataRecoverySuccess++
}

// RecordReconstructionFailure records a failed reconstruction attempt
func (m *MetricsCollector) RecordReconstructionFailure(chunkID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DataRecoveryFailures++
	m.Errors = append(m.Errors, fmt.Sprintf("reconstruction failed for chunk %s", chunkID))
}

// RecordGrpcConnection records gRPC connection time
func (m *MetricsCollector) RecordGrpcConnection(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GrpcConnectionTime += duration
	m.recordNetworkLatency(duration)
}

// RecordAckReceived records an ACK receipt
func (m *MetricsCollector) RecordAckReceived() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalAckReceived++
}

// RecordAckTimeout records an ACK timeout
func (m *MetricsCollector) RecordAckTimeout() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalAckTimeout++
}

// RecordDeleteOperation records deletion of chunks
func (m *MetricsCollector) RecordDeleteOperation(chunkCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DeleteChunkCount += chunkCount
}

// RecordListOperation records the list operation
func (m *MetricsCollector) RecordListOperation(fileCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FilesListed = fileCount
}

// RecordError records an error that occurred
func (m *MetricsCollector) RecordError(err string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Errors = append(m.Errors, err)
}

// recordNetworkLatency is a helper to track individual network latencies
func (m *MetricsCollector) recordNetworkLatency(latency time.Duration) {
	m.NetworkLatencies = append(m.NetworkLatencies, latency)

	if m.MaxNetworkLatency == 0 || latency > m.MaxNetworkLatency {
		m.MaxNetworkLatency = latency
	}

	if m.MinNetworkLatency == 0 || latency < m.MinNetworkLatency {
		m.MinNetworkLatency = latency
	}

	// Calculate average
	total := time.Duration(0)
	for _, l := range m.NetworkLatencies {
		total += l
	}
	if len(m.NetworkLatencies) > 0 {
		m.AverageNetworkLatency = total / time.Duration(len(m.NetworkLatencies))
	}
}

// PrintReport prints a comprehensive metrics report
func (m *MetricsCollector) PrintReport(operation string, filename string, fileSize int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fmt.Println("\n╔════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        COMPREHENSIVE METRICS REPORT                          ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════════════════╝")

	// Operation information
	fmt.Printf("\n📋 OPERATION INFORMATION\n")
	fmt.Printf("  Operation:      %s\n", operation)
	fmt.Printf("  Filename:       %s\n", filename)
	fmt.Printf("  File Size:      %.2f MB (%.0f bytes)\n", float64(fileSize)/(1024*1024), float64(fileSize))

	// Timing metrics
	fmt.Printf("\n⏱️  TIMING METRICS\n")
	latencySec := m.TotalDuration.Seconds()
	if latencySec == 0 {
		latencySec = 0.001
	}
	fmt.Printf("  Total Duration: %.2f ms (%.3f seconds)\n", float64(m.TotalDuration.Milliseconds()), latencySec)
	fmt.Printf("  Master Call:    %v\n", m.MasterCallLatency)

	// Throughput and Bandwidth
	fmt.Printf("\n📊 THROUGHPUT & BANDWIDTH\n")
	if operation == "UPLOAD" {
		fmt.Printf("  Bytes Uploaded: %.2f MB (%.0f bytes)\n", float64(m.TotalBytesUploaded)/(1024*1024), float64(m.TotalBytesUploaded))
		fmt.Printf("  Chunks Uploaded: %d\n", m.TotalChunksUploaded)
		if m.TotalChunksUploaded > 0 {
			chunkThroughput := float64(m.TotalChunksUploaded) / latencySec
			fmt.Printf("  Chunk Throughput: %.2f chunks/sec\n", chunkThroughput)
		}
	} else if operation == "DOWNLOAD" {
		fmt.Printf("  Bytes Downloaded: %.2f MB (%.0f bytes)\n", float64(m.TotalBytesDownloaded)/(1024*1024), float64(m.TotalBytesDownloaded))
		fmt.Printf("  Chunks Downloaded: %d\n", m.TotalChunksDownloaded)
		if m.TotalChunksDownloaded > 0 {
			chunkThroughput := float64(m.TotalChunksDownloaded) / latencySec
			fmt.Printf("  Chunk Throughput: %.2f chunks/sec\n", chunkThroughput)
		}
	}

	if m.TotalBytesUploaded > 0 {
		bandwidthMBps := (float64(m.TotalBytesUploaded) / (1024 * 1024)) / latencySec
		fmt.Printf("  Upload Bandwidth: %.2f MB/sec\n", bandwidthMBps)
	}

	if m.TotalBytesDownloaded > 0 {
		bandwidthMBps := (float64(m.TotalBytesDownloaded) / (1024 * 1024)) / latencySec
		fmt.Printf("  Download Bandwidth: %.2f MB/sec\n", bandwidthMBps)
	}

	// Computation metrics
	fmt.Printf("\n⚙️  COMPUTATION METRICS\n")
	fmt.Printf("  Parity Calc Time:     %v (%.2f ms)\n", m.ParityCalcDuration, float64(m.ParityCalcDuration.Milliseconds()))
	fmt.Printf("  Checksum Calc Time:   %v (%.2f ms)\n", m.ChecksumCalcDuration, float64(m.ChecksumCalcDuration.Milliseconds()))
	fmt.Printf("  Chunk Ops Time:       %v (%.2f ms)\n", m.ChunkUploadDuration+m.ChunkDownloadDuration, float64((m.ChunkUploadDuration + m.ChunkDownloadDuration).Milliseconds()))

	// Network metrics
	fmt.Printf("\n🌐 NETWORK METRICS\n")
	fmt.Printf("  Max Latency:    %v\n", m.MaxNetworkLatency)
	fmt.Printf("  Min Latency:    %v\n", m.MinNetworkLatency)
	fmt.Printf("  Avg Latency:    %v\n", m.AverageNetworkLatency)
	fmt.Printf("  Total Latency:  %v\n", m.TotalNetworkLatency)
	fmt.Printf("  gRPC Conn Time: %v\n", m.GrpcConnectionTime)

	// Data integrity metrics
	fmt.Printf("\n🔒 DATA INTEGRITY METRICS\n")
	fmt.Printf("  Checksums Calculated: %d\n", m.TotalChecksumVerified)
	fmt.Printf("  Checksums Failed:     %d\n", m.FailedChecksumVerify)
	if m.FailedChecksumVerify > 0 {
		fmt.Printf("  Failed Checksum Details:\n")
		for _, detail := range m.ChecksumMismatchDetails {
			fmt.Printf("    - %s\n", detail)
		}
	}

	// RAID-5 recovery metrics
	fmt.Printf("\n🔄 RAID-5 RECOVERY METRICS\n")
	fmt.Printf("  Reconstructions:      %d\n", m.ReconstructionCount)
	fmt.Printf("  Reconstruction Time:  %v (%.2f ms)\n", m.ReconstructionDuration, float64(m.ReconstructionDuration.Milliseconds()))
	fmt.Printf("  Parity Chunks Used:   %d\n", m.ParityChunksUsed)
	fmt.Printf("  Recovery Success:     %d\n", m.DataRecoverySuccess)
	fmt.Printf("  Recovery Failures:    %d\n", m.DataRecoveryFailures)

	// ACK metrics
	fmt.Printf("\n✅ ACKNOWLEDGEMENT METRICS\n")
	fmt.Printf("  ACKs Received: %d\n", m.TotalAckReceived)
	fmt.Printf("  ACK Timeouts:  %d\n", m.TotalAckTimeout)

	// Operation-specific metrics
	if operation == "DELETE" {
		fmt.Printf("\n🗑️  DELETE OPERATION METRICS\n")
		fmt.Printf("  Chunks Deleted: %d\n", m.DeleteChunkCount)
	}

	if operation == "LIST" {
		fmt.Printf("\n📂 LIST OPERATION METRICS\n")
		fmt.Printf("  Files Listed: %d\n", m.FilesListed)
	}

	// Error summary
	if len(m.Errors) > 0 {
		fmt.Printf("\n❌ ERRORS ENCOUNTERED\n")
		for i, err := range m.Errors {
			fmt.Printf("  %d. %s\n", i+1, err)
		}
	} else {
		fmt.Printf("\n✓ No errors encountered\n")
	}

	fmt.Println("\n╚════════════════════════════════════════════════════════════════════════════╝")
}

// PrintSummary prints a brief one-line summary
func (m *MetricsCollector) PrintSummary(operation string, fileSize int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	latencySec := m.TotalDuration.Seconds()
	if latencySec == 0 {
		latencySec = 0.001
	}

	fileSizeMB := float64(fileSize) / (1024 * 1024)
	fmt.Printf("✓ %s: %.2f MB in %.3f seconds (%.2f MB/sec, %.0f ms)\n",
		operation, fileSizeMB, latencySec, fileSizeMB/latencySec, float64(m.TotalDuration.Milliseconds()))
}

// ExportJSON exports metrics as JSON for external analysis
func (m *MetricsCollector) ExportJSON() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return fmt.Sprintf(`{
  "timing": {
    "total_duration_ms": %d,
    "master_call_latency_ms": %d,
    "parity_calc_ms": %d,
    "checksum_calc_ms": %d,
    "chunk_upload_ms": %d,
    "chunk_download_ms": %d,
    "reconstruction_ms": %d
  },
  "upload": {
    "total_bytes": %d,
    "total_chunks": %d
  },
  "download": {
    "total_bytes": %d,
    "total_chunks": %d
  },
  "network": {
    "max_latency_ms": %d,
    "min_latency_ms": %d,
    "avg_latency_ms": %d,
    "total_latency_ms": %d,
    "grpc_connection_ms": %d
  },
  "integrity": {
    "checksums_verified": %d,
    "checksums_failed": %d,
    "reconstructions": %d,
    "recovery_success": %d,
    "recovery_failures": %d
  },
  "acks": {
    "received": %d,
    "timeouts": %d
  },
  "errors": %d
}`,
		m.TotalDuration.Milliseconds(),
		m.MasterCallLatency.Milliseconds(),
		m.ParityCalcDuration.Milliseconds(),
		m.ChecksumCalcDuration.Milliseconds(),
		m.ChunkUploadDuration.Milliseconds(),
		m.ChunkDownloadDuration.Milliseconds(),
		m.ReconstructionDuration.Milliseconds(),
		m.TotalBytesUploaded,
		m.TotalChunksUploaded,
		m.TotalBytesDownloaded,
		m.TotalChunksDownloaded,
		m.MaxNetworkLatency.Milliseconds(),
		m.MinNetworkLatency.Milliseconds(),
		m.AverageNetworkLatency.Milliseconds(),
		m.TotalNetworkLatency.Milliseconds(),
		m.GrpcConnectionTime.Milliseconds(),
		m.TotalChecksumVerified,
		m.FailedChecksumVerify,
		m.ReconstructionCount,
		m.DataRecoverySuccess,
		m.DataRecoveryFailures,
		m.TotalAckReceived,
		m.TotalAckTimeout,
		len(m.Errors),
	)
}
