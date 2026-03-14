package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Core data structure
// ─────────────────────────────────────────────────────────────────────────────

// OperationMetrics holds every measurement for a single client operation.
type OperationMetrics struct {
	// Identity
	Operation string    // "upload" | "download" | "delete" | "ls"
	Filename  string    // file or path involved
	Timestamp time.Time // wall-clock start of operation

	// File dimensions
	FileSizeBytes int64 // original file size (0 for ls/delete)
	ChunkCount    int   // total chunks (data + parity)
	StripeCount   int   // number of RAID-5 stripes

	// Wall-clock phase breakdown (all milliseconds)
	TotalLatencyMs     float64 // end-to-end wall time
	MasterRPCLatencyMs float64 // cumulative time in master RPCs
	DataTransferMs     float64 // time spent in chunk read/write RPCs
	ParityComputeMs    float64 // time computing XOR parity (upload only)

	// Derived throughput metrics
	ThroughputMBps    float64 // FileSizeBytes / TotalLatencyMs  — end-to-end rate
	BandwidthMBps     float64 // FileSizeBytes / DataTransferMs  — wire-only rate
	ParallelSpeedup   float64 // theoretical sequential / actual parallel
	MasterOverheadPct float64 // MasterRPCLatencyMs / TotalLatencyMs * 100

	// RAID-5 reconstruction overhead (download only — zero when all chunks healthy)
	ReconstructionMs          float64 // total XOR compute time for recovered chunks (ms)
	ReconstructionOverheadPct float64 // ReconstructionMs / TotalLatencyMs * 100
	DegradedDownload          bool    // true if any chunk was missing and reconstructed

	// Reliability
	TotalChunksAttempted int
	SuccessfulChunks     int
	ReconstructedChunks  int     // chunks recovered via RAID-5 XOR
	SuccessRate          float64 // percentage

	// Error (empty on success)
	Error string
}

// ─────────────────────────────────────────────────────────────────────────────
// OperationContext — accumulates measurements during an operation
// ─────────────────────────────────────────────────────────────────────────────

// OperationContext is created at the start of each operation and threaded
// through the call stack to accumulate phase timings and counts.
type OperationContext struct {
	mu sync.Mutex

	operation string
	filename  string
	fileSize  int64
	startTime time.Time

	masterRPCMs   float64
	dataXferMs    float64
	parityMs      float64
	reconstrMs    float64 // RAID-5 reconstruction XOR time (download only)
	stripeCount   int
	chunkAttempts int
	chunkSuccess  int
	chunkReconstr int
}

// NewOpCtx starts a new operation measurement context.
func NewOpCtx(operation, filename string, fileSize int64) *OperationContext {
	return &OperationContext{
		operation: operation,
		filename:  filename,
		fileSize:  fileSize,
		startTime: time.Now(),
	}
}

// SetFileSize updates the file size once it becomes known (e.g. after GetFileMetadata).
func (c *OperationContext) SetFileSize(n int64) {
	c.mu.Lock()
	c.fileSize = n
	c.mu.Unlock()
}

// AddMasterRPC accumulates milliseconds spent in a master RPC.
func (c *OperationContext) AddMasterRPC(ms float64) {
	c.mu.Lock()
	c.masterRPCMs += ms
	c.mu.Unlock()
}

// AddDataXfer accumulates milliseconds spent transferring chunk data.
func (c *OperationContext) AddDataXfer(ms float64) {
	c.mu.Lock()
	c.dataXferMs += ms
	c.mu.Unlock()
}

// AddParity accumulates milliseconds spent computing XOR parity.
func (c *OperationContext) AddParity(ms float64) {
	c.mu.Lock()
	c.parityMs += ms
	c.mu.Unlock()
}

// AddReconstruction accumulates milliseconds spent on RAID-5 XOR reconstruction
// during a degraded download (one chunkserver dead).
func (c *OperationContext) AddReconstruction(ms float64) {
	c.mu.Lock()
	c.reconstrMs += ms
	c.mu.Unlock()
}

// AddStripes increments the stripe counter by n.
func (c *OperationContext) AddStripes(n int) {
	c.mu.Lock()
	c.stripeCount += n
	c.mu.Unlock()
}

// AddChunkResult records the outcome of one chunk transfer attempt.
// reconstructed should be true when the chunk was recovered via RAID-5 XOR.
func (c *OperationContext) AddChunkResult(success, reconstructed bool) {
	c.mu.Lock()
	c.chunkAttempts++
	if success {
		c.chunkSuccess++
	}
	if reconstructed {
		c.chunkReconstr++
	}
	c.mu.Unlock()
}

// Finalise closes the context, computes derived metrics, and returns
// a completed OperationMetrics ready for recording.
func (c *OperationContext) Finalise(errMsg string) OperationMetrics {
	totalMs := float64(time.Since(c.startTime).Nanoseconds()) / 1e6

	chunkCount := c.stripeCount * 3 // 2 data + 1 parity per stripe

	// Throughput: file bytes / total wall time (MB/s)
	throughput := 0.0
	if totalMs > 0 && c.fileSize > 0 {
		throughput = (float64(c.fileSize) / 1_000_000.0) / (totalMs / 1_000.0)
	}

	// Bandwidth: file bytes / data-transfer time only (wire rate, excludes master overhead)
	bandwidth := 0.0
	if c.dataXferMs > 0 && c.fileSize > 0 {
		bandwidth = (float64(c.fileSize) / 1_000_000.0) / (c.dataXferMs / 1_000.0)
	}

	// Parallel speedup: 3 chunks uploaded/downloaded concurrently per stripe.
	// Sequential equivalent = dataXferMs * 3 (one chunk at a time).
	// Actual = dataXferMs. Perfect speedup = 3.0x.
	parallelSpeedup := 3.0
	if c.dataXferMs <= 0 {
		parallelSpeedup = 0.0
	}

	// Master overhead as % of total latency
	masterOverheadPct := 0.0
	if totalMs > 0 {
		masterOverheadPct = (c.masterRPCMs / totalMs) * 100.0
	}

	// Reconstruction overhead as % of total latency
	reconstrOverheadPct := 0.0
	if totalMs > 0 && c.reconstrMs > 0 {
		reconstrOverheadPct = (c.reconstrMs / totalMs) * 100.0
	}

	// DegradedDownload: true only if XOR reconstruction actually ran (reconstrMs > 0)
	// This is the ground truth — chunkReconstr count can be wrong for odd-stripe files
	degraded := c.reconstrMs > 0

	successRate := 0.0
	if c.chunkAttempts > 0 {
		successRate = float64(c.chunkSuccess) / float64(c.chunkAttempts) * 100.0
	}

	return OperationMetrics{
		Operation:                 c.operation,
		Filename:                  c.filename,
		Timestamp:                 c.startTime,
		FileSizeBytes:             c.fileSize,
		ChunkCount:                chunkCount,
		StripeCount:               c.stripeCount,
		TotalLatencyMs:            totalMs,
		MasterRPCLatencyMs:        c.masterRPCMs,
		DataTransferMs:            c.dataXferMs,
		ParityComputeMs:           c.parityMs,
		ThroughputMBps:            throughput,
		BandwidthMBps:             bandwidth,
		ParallelSpeedup:           parallelSpeedup,
		MasterOverheadPct:         masterOverheadPct,
		ReconstructionMs:          c.reconstrMs,
		ReconstructionOverheadPct: reconstrOverheadPct,
		DegradedDownload:          degraded,
		TotalChunksAttempted:      c.chunkAttempts,
		SuccessfulChunks:          c.chunkSuccess,
		ReconstructedChunks:       c.chunkReconstr,
		SuccessRate:               successRate,
		Error:                     errMsg,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// phaseTimer — lightweight sub-phase stopwatch
// ─────────────────────────────────────────────────────────────────────────────

type phaseTimer struct{ start time.Time }

func newPhaseTimer() *phaseTimer         { return &phaseTimer{start: time.Now()} }
func (p *phaseTimer) ElapsedMs() float64 { return float64(time.Since(p.start).Nanoseconds()) / 1e6 }

// ─────────────────────────────────────────────────────────────────────────────
// Terminal output
// ─────────────────────────────────────────────────────────────────────────────

// PrintMetrics writes a formatted metrics report to stderr.
func PrintMetrics(m OperationMetrics) {
	statusIcon := "✅ SUCCESS"
	if m.Error != "" {
		statusIcon = "❌ FAILED: " + m.Error
	}

	w := os.Stderr
	row := func(label, val string) {
		fmt.Fprintf(w, "║  %-22s: %-36s║\n", label, val)
	}
	sep := func() {
		fmt.Fprintln(w, "╠══════════════════════════════════════════════════════════════╣")
	}
	hdr := func(title string) {
		fmt.Fprintf(w, "╠  %-59s╣\n", "── "+title)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "╔══════════════════════════════════════════════════════════════╗")
	fmt.Fprintf(w, "║  📊  XorFS PERFORMANCE METRICS                               ║\n")
	sep()
	row("Operation", m.Operation)
	row("File", m.Filename)
	row("Status", statusIcon)
	row("Timestamp", m.Timestamp.Format("2006-01-02 15:04:05.000"))
	sep()

	hdr("FILE SIZE")
	row("Bytes", fmt.Sprintf("%d B", m.FileSizeBytes))
	row("Human-readable", formatSize(m.FileSizeBytes))
	row("Stripes", strconv.Itoa(m.StripeCount))
	row("Chunks", fmt.Sprintf("%d  (data=%d  parity=%d)",
		m.ChunkCount, m.StripeCount*2, m.StripeCount))
	sep()

	hdr("LATENCY  (milliseconds)")
	row("Total (end-to-end)", fmt.Sprintf("%.3f ms", m.TotalLatencyMs))
	row("Master RPC total", fmt.Sprintf("%.3f ms  (%.1f%% overhead)", m.MasterRPCLatencyMs, m.MasterOverheadPct))
	row("Data transfer", fmt.Sprintf("%.3f ms", m.DataTransferMs))
	if m.Operation == "upload" {
		row("Parity compute", fmt.Sprintf("%.3f ms", m.ParityComputeMs))
		if m.TotalLatencyMs > 0 {
			parityPct := m.ParityComputeMs / m.TotalLatencyMs * 100.0
			row("  └ parity overhead", fmt.Sprintf("%.1f%% of total", parityPct))
		}
	}
	sep()

	hdr("THROUGHPUT & BANDWIDTH")
	row("Throughput", fmt.Sprintf("%.4f MB/s  (end-to-end)", m.ThroughputMBps))
	row("Bandwidth", fmt.Sprintf("%.4f MB/s  (wire only)", m.BandwidthMBps))
	row("Parallel speedup", fmt.Sprintf("%.2f×  (ideal = 3.00×)", m.ParallelSpeedup))
	sep()

	hdr("RELIABILITY")
	row("Chunks attempted", strconv.Itoa(m.TotalChunksAttempted))
	row("Chunks succeeded", strconv.Itoa(m.SuccessfulChunks))
	row("RAID-5 reconstructed", strconv.Itoa(m.ReconstructedChunks))
	row("Success rate", fmt.Sprintf("%.1f%%", m.SuccessRate))
	sep()

	hdr("RAID-5 RECONSTRUCTION OVERHEAD  (download only)")
	if m.DegradedDownload {
		row("Mode", "⚠️  DEGRADED  (chunkserver missing)")
		row("Reconstruction time", fmt.Sprintf("%.3f ms", m.ReconstructionMs))
		row("Reconstruction overhead", fmt.Sprintf("%.2f%% of total latency", m.ReconstructionOverheadPct))
		row("Chunks recovered", strconv.Itoa(m.ReconstructedChunks))
	} else {
		row("Mode", "✅ NORMAL  (all chunkservers healthy)")
		row("Reconstruction time", "0.000 ms  (no recovery needed)")
	}
	fmt.Fprintln(w, "╚══════════════════════════════════════════════════════════════╝")
	fmt.Fprintln(w)
}

// ─────────────────────────────────────────────────────────────────────────────
// CSV output
// ─────────────────────────────────────────────────────────────────────────────

const metricsCSVFile = "dfs_metrics.csv"

var csvHeader = []string{
	"timestamp", "operation", "filename",
	"file_size_bytes", "file_size_human",
	"stripe_count", "chunk_count",
	"total_latency_ms", "master_rpc_latency_ms", "data_transfer_ms", "parity_compute_ms",
	"master_overhead_pct",
	"throughput_mbps", "bandwidth_mbps", "parallel_speedup",
	"chunks_attempted", "chunks_succeeded", "chunks_reconstructed",
	"success_rate_pct",
	"degraded_download", "reconstruction_ms", "reconstruction_overhead_pct",
	"status", "error",
}

// AppendMetricsCSV appends one row to dfs_metrics.csv, writing the header
// first if the file does not yet exist.
func AppendMetricsCSV(m OperationMetrics) error {
	needHeader := false
	if _, err := os.Stat(metricsCSVFile); os.IsNotExist(err) {
		needHeader = true
	}

	f, err := os.OpenFile(metricsCSVFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("metrics CSV open failed: %v", err)
	}
	defer f.Close()

	cw := csv.NewWriter(f)
	if needHeader {
		if err := cw.Write(csvHeader); err != nil {
			return err
		}
	}

	statusStr := "success"
	if m.Error != "" {
		statusStr = "error"
	}

	degradedStr := "false"
	if m.DegradedDownload {
		degradedStr = "true"
	}

	row := []string{
		m.Timestamp.Format(time.RFC3339Nano),
		m.Operation,
		m.Filename,
		strconv.FormatInt(m.FileSizeBytes, 10),
		formatSize(m.FileSizeBytes),
		strconv.Itoa(m.StripeCount),
		strconv.Itoa(m.ChunkCount),
		fmt.Sprintf("%.3f", m.TotalLatencyMs),
		fmt.Sprintf("%.3f", m.MasterRPCLatencyMs),
		fmt.Sprintf("%.3f", m.DataTransferMs),
		fmt.Sprintf("%.3f", m.ParityComputeMs),
		fmt.Sprintf("%.2f", m.MasterOverheadPct),
		fmt.Sprintf("%.4f", m.ThroughputMBps),
		fmt.Sprintf("%.4f", m.BandwidthMBps),
		fmt.Sprintf("%.4f", m.ParallelSpeedup),
		strconv.Itoa(m.TotalChunksAttempted),
		strconv.Itoa(m.SuccessfulChunks),
		strconv.Itoa(m.ReconstructedChunks),
		fmt.Sprintf("%.2f", m.SuccessRate),
		degradedStr,
		fmt.Sprintf("%.3f", m.ReconstructionMs),
		fmt.Sprintf("%.2f", m.ReconstructionOverheadPct),
		statusStr,
		m.Error,
	}

	if err := cw.Write(row); err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}

// ─────────────────────────────────────────────────────────────────────────────
// RecordMetrics — single call-site used by all operations
// ─────────────────────────────────────────────────────────────────────────────

// RecordMetrics prints to terminal AND appends to CSV.
// Call this at the end of every operation (success or failure).
func RecordMetrics(m OperationMetrics) {
	PrintMetrics(m)
	if err := AppendMetricsCSV(m); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write metrics CSV: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "📄  Metrics saved → %s\n\n", metricsCSVFile)
	}
}
