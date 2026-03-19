// package main

// import (
// 	"encoding/csv"
// 	"encoding/json"
// 	"fmt"
// 	"net/http"
// 	"os"
// 	"strconv"
// 	"sync"
// 	"time"
// )

// // ─────────────────────────────────────────────────────────────────────────────
// // Core data structure
// // ─────────────────────────────────────────────────────────────────────────────

// // OperationMetrics holds every measurement for a single client operation.
// type OperationMetrics struct {
// 	Operation string    `json:"operation"` // "upload"|"download"|"delete"|"ls"
// 	Filename  string    `json:"filename"`
// 	Username  string    `json:"username"` // web user; empty for CLI
// 	Source    string    `json:"source"`   // "cli" | "web"
// 	Timestamp time.Time `json:"timestamp"`

// 	FileSizeBytes int64 `json:"file_size_bytes"`
// 	ChunkCount    int   `json:"chunk_count"`
// 	StripeCount   int   `json:"stripe_count"`

// 	TotalLatencyMs     float64 `json:"total_latency_ms"`
// 	MasterRPCLatencyMs float64 `json:"master_rpc_latency_ms"`
// 	DataTransferMs     float64 `json:"data_transfer_ms"`
// 	ParityComputeMs    float64 `json:"parity_compute_ms"`

// 	ThroughputMBps    float64 `json:"throughput_mbps"`
// 	BandwidthMBps     float64 `json:"bandwidth_mbps"`
// 	ParallelSpeedup   float64 `json:"parallel_speedup"`
// 	MasterOverheadPct float64 `json:"master_overhead_pct"`

// 	ReconstructionMs          float64 `json:"reconstruction_ms"`
// 	ReconstructionOverheadPct float64 `json:"reconstruction_overhead_pct"`
// 	DegradedDownload          bool    `json:"degraded_download"`

// 	TotalChunksAttempted int     `json:"chunks_attempted"`
// 	SuccessfulChunks     int     `json:"chunks_succeeded"`
// 	ReconstructedChunks  int     `json:"chunks_reconstructed"`
// 	SuccessRate          float64 `json:"success_rate_pct"`

// 	ConcurrentUsers int    `json:"concurrent_users"` // web only; 0 for CLI
// 	Error           string `json:"error"`
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // OperationContext
// // ─────────────────────────────────────────────────────────────────────────────

// type OperationContext struct {
// 	mu        sync.Mutex
// 	operation string
// 	filename  string
// 	username  string
// 	source    string
// 	fileSize  int64
// 	startTime time.Time

// 	masterRPCMs   float64
// 	dataXferMs    float64
// 	parityMs      float64
// 	reconstrMs    float64
// 	stripeCount   int
// 	chunkAttempts int
// 	chunkSuccess  int
// 	chunkReconstr int
// }

// // NewOpCtx — CLI operation.
// func NewOpCtx(operation, filename string, fileSize int64) *OperationContext {
// 	return &OperationContext{
// 		operation: operation,
// 		filename:  filename,
// 		source:    "cli",
// 		fileSize:  fileSize,
// 		startTime: time.Now(),
// 	}
// }

// // NewWebOpCtx — web operation; increments the active-user counter.
// func NewWebOpCtx(operation, filename, username string, fileSize int64) *OperationContext {
// 	activeUsers.inc(username)
// 	return &OperationContext{
// 		operation: operation,
// 		filename:  filename,
// 		username:  username,
// 		source:    "web",
// 		fileSize:  fileSize,
// 		startTime: time.Now(),
// 	}
// }

// func (c *OperationContext) SetFileSize(n int64)     { c.mu.Lock(); c.fileSize = n; c.mu.Unlock() }
// func (c *OperationContext) AddMasterRPC(ms float64) { c.mu.Lock(); c.masterRPCMs += ms; c.mu.Unlock() }
// func (c *OperationContext) AddDataXfer(ms float64)  { c.mu.Lock(); c.dataXferMs += ms; c.mu.Unlock() }
// func (c *OperationContext) AddParity(ms float64)    { c.mu.Lock(); c.parityMs += ms; c.mu.Unlock() }
// func (c *OperationContext) AddReconstruction(ms float64) {
// 	c.mu.Lock()
// 	c.reconstrMs += ms
// 	c.mu.Unlock()
// }
// func (c *OperationContext) AddStripes(n int) { c.mu.Lock(); c.stripeCount += n; c.mu.Unlock() }

// func (c *OperationContext) AddChunkResult(success, reconstructed bool) {
// 	c.mu.Lock()
// 	c.chunkAttempts++
// 	if success {
// 		c.chunkSuccess++
// 	}
// 	if reconstructed {
// 		c.chunkReconstr++
// 	}
// 	c.mu.Unlock()
// }

// // Finalise computes all derived metrics and returns a completed OperationMetrics.
// // For web contexts it decrements the active-user counter.
// func (c *OperationContext) Finalise(errMsg string) OperationMetrics {
// 	totalMs := float64(time.Since(c.startTime).Nanoseconds()) / 1e6

// 	concurrent := 0
// 	if c.source == "web" {
// 		concurrent = activeUsers.count()
// 		activeUsers.dec(c.username)
// 	}

// 	chunkCount := c.stripeCount * 3

// 	throughput := 0.0
// 	if totalMs > 0 && c.fileSize > 0 {
// 		throughput = (float64(c.fileSize) / 1e6) / (totalMs / 1e3)
// 	}
// 	bandwidth := 0.0
// 	if c.dataXferMs > 0 && c.fileSize > 0 {
// 		bandwidth = (float64(c.fileSize) / 1e6) / (c.dataXferMs / 1e3)
// 	}
// 	parallelSpeedup := 3.0
// 	if c.dataXferMs <= 0 {
// 		parallelSpeedup = 0.0
// 	}
// 	masterOH := 0.0
// 	if totalMs > 0 {
// 		masterOH = c.masterRPCMs / totalMs * 100.0
// 	}
// 	reconstrOH := 0.0
// 	if totalMs > 0 && c.reconstrMs > 0 {
// 		reconstrOH = c.reconstrMs / totalMs * 100.0
// 	}
// 	successRate := 0.0
// 	if c.chunkAttempts > 0 {
// 		successRate = float64(c.chunkSuccess) / float64(c.chunkAttempts) * 100.0
// 	}

// 	return OperationMetrics{
// 		Operation:                 c.operation,
// 		Filename:                  c.filename,
// 		Username:                  c.username,
// 		Source:                    c.source,
// 		Timestamp:                 c.startTime,
// 		FileSizeBytes:             c.fileSize,
// 		ChunkCount:                chunkCount,
// 		StripeCount:               c.stripeCount,
// 		TotalLatencyMs:            totalMs,
// 		MasterRPCLatencyMs:        c.masterRPCMs,
// 		DataTransferMs:            c.dataXferMs,
// 		ParityComputeMs:           c.parityMs,
// 		ThroughputMBps:            throughput,
// 		BandwidthMBps:             bandwidth,
// 		ParallelSpeedup:           parallelSpeedup,
// 		MasterOverheadPct:         masterOH,
// 		ReconstructionMs:          c.reconstrMs,
// 		ReconstructionOverheadPct: reconstrOH,
// 		DegradedDownload:          c.reconstrMs > 0,
// 		TotalChunksAttempted:      c.chunkAttempts,
// 		SuccessfulChunks:          c.chunkSuccess,
// 		ReconstructedChunks:       c.chunkReconstr,
// 		SuccessRate:               successRate,
// 		ConcurrentUsers:           concurrent,
// 		Error:                     errMsg,
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // Active-user tracker
// // ─────────────────────────────────────────────────────────────────────────────

// type activeUserTracker struct {
// 	mu    sync.Mutex
// 	users map[string]int
// }

// func (t *activeUserTracker) inc(u string) { t.mu.Lock(); t.users[u]++; t.mu.Unlock() }
// func (t *activeUserTracker) dec(u string) {
// 	t.mu.Lock()
// 	t.users[u]--
// 	if t.users[u] <= 0 {
// 		delete(t.users, u)
// 	}
// 	t.mu.Unlock()
// }
// func (t *activeUserTracker) count() int {
// 	t.mu.Lock()
// 	defer t.mu.Unlock()
// 	return len(t.users)
// }
// func (t *activeUserTracker) snapshot() map[string]int {
// 	t.mu.Lock()
// 	defer t.mu.Unlock()
// 	s := make(map[string]int, len(t.users))
// 	for k, v := range t.users {
// 		s[k] = v
// 	}
// 	return s
// }

// var activeUsers = &activeUserTracker{users: make(map[string]int)}

// // ─────────────────────────────────────────────────────────────────────────────
// // In-memory store — last 1000 operations for the /metrics API
// // ─────────────────────────────────────────────────────────────────────────────

// const maxStored = 1000

// type metricsStore struct {
// 	mu      sync.RWMutex
// 	entries []OperationMetrics
// }

// func (s *metricsStore) push(m OperationMetrics) {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()
// 	s.entries = append(s.entries, m)
// 	if len(s.entries) > maxStored {
// 		s.entries = s.entries[len(s.entries)-maxStored:]
// 	}
// }

// func (s *metricsStore) all() []OperationMetrics {
// 	s.mu.RLock()
// 	defer s.mu.RUnlock()
// 	cp := make([]OperationMetrics, len(s.entries))
// 	copy(cp, s.entries)
// 	return cp
// }

// var mstore = &metricsStore{}

// // ─────────────────────────────────────────────────────────────────────────────
// // phaseTimer
// // ─────────────────────────────────────────────────────────────────────────────

// type phaseTimer struct{ start time.Time }

// func newPhaseTimer() *phaseTimer         { return &phaseTimer{start: time.Now()} }
// func (p *phaseTimer) ElapsedMs() float64 { return float64(time.Since(p.start).Nanoseconds()) / 1e6 }

// // ─────────────────────────────────────────────────────────────────────────────
// // Terminal output (CLI)
// // ─────────────────────────────────────────────────────────────────────────────

// func PrintMetrics(m OperationMetrics) {
// 	statusIcon := "SUCCESS"
// 	if m.Error != "" {
// 		statusIcon = "FAILED: " + m.Error
// 	}
// 	w := os.Stderr
// 	row := func(label, val string) { fmt.Fprintf(w, "  %-24s: %s\n", label, val) }

// 	fmt.Fprintln(w, "\n=== XorFS PERFORMANCE METRICS ===")
// 	row("Operation", m.Operation)
// 	row("File", m.Filename)
// 	if m.Username != "" {
// 		row("User", m.Username)
// 	}
// 	row("Source", m.Source)
// 	row("Status", statusIcon)
// 	row("Timestamp", m.Timestamp.Format("2006-01-02 15:04:05.000"))
// 	if m.ConcurrentUsers > 0 {
// 		row("Concurrent users", strconv.Itoa(m.ConcurrentUsers))
// 	}
// 	fmt.Fprintln(w, "--- FILE ---")
// 	row("Size", formatSize(m.FileSizeBytes))
// 	row("Stripes", strconv.Itoa(m.StripeCount))
// 	row("Chunks", fmt.Sprintf("%d (data=%d parity=%d)", m.ChunkCount, m.StripeCount*2, m.StripeCount))
// 	fmt.Fprintln(w, "--- LATENCY ---")
// 	row("Total", fmt.Sprintf("%.3f ms", m.TotalLatencyMs))
// 	row("Master RPC", fmt.Sprintf("%.3f ms (%.1f%%)", m.MasterRPCLatencyMs, m.MasterOverheadPct))
// 	row("Data transfer", fmt.Sprintf("%.3f ms", m.DataTransferMs))
// 	if m.Operation == "upload" {
// 		row("Parity compute", fmt.Sprintf("%.3f ms", m.ParityComputeMs))
// 	}
// 	fmt.Fprintln(w, "--- THROUGHPUT ---")
// 	row("Throughput", fmt.Sprintf("%.4f MB/s", m.ThroughputMBps))
// 	row("Bandwidth", fmt.Sprintf("%.4f MB/s", m.BandwidthMBps))
// 	fmt.Fprintln(w, "--- RELIABILITY ---")
// 	row("Chunks attempted", strconv.Itoa(m.TotalChunksAttempted))
// 	row("Chunks succeeded", strconv.Itoa(m.SuccessfulChunks))
// 	row("RAID-5 reconstructed", strconv.Itoa(m.ReconstructedChunks))
// 	row("Success rate", fmt.Sprintf("%.1f%%", m.SuccessRate))
// 	if m.DegradedDownload {
// 		fmt.Fprintln(w, "--- RECONSTRUCTION ---")
// 		row("Mode", "DEGRADED")
// 		row("Recon time", fmt.Sprintf("%.3f ms", m.ReconstructionMs))
// 		row("Recon overhead", fmt.Sprintf("%.2f%%", m.ReconstructionOverheadPct))
// 	}
// 	fmt.Fprintln(w, "=================================\n")
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // CSV — mutex-protected for concurrent web requests
// // ─────────────────────────────────────────────────────────────────────────────

// const metricsCSVFile = "dfs_metrics.csv"

// var csvMu sync.Mutex

// var csvHeader = []string{
// 	"timestamp", "source", "username", "operation", "filename",
// 	"file_size_bytes", "file_size_human", "stripe_count", "chunk_count",
// 	"total_latency_ms", "master_rpc_latency_ms", "data_transfer_ms", "parity_compute_ms",
// 	"master_overhead_pct", "throughput_mbps", "bandwidth_mbps", "parallel_speedup",
// 	"chunks_attempted", "chunks_succeeded", "chunks_reconstructed", "success_rate_pct",
// 	"degraded_download", "reconstruction_ms", "reconstruction_overhead_pct",
// 	"concurrent_users", "status", "error",
// }

// func AppendMetricsCSV(m OperationMetrics) error {
// 	csvMu.Lock()
// 	defer csvMu.Unlock()

// 	needHeader := false
// 	if _, err := os.Stat(metricsCSVFile); os.IsNotExist(err) {
// 		needHeader = true
// 	}
// 	f, err := os.OpenFile(metricsCSVFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
// 	if err != nil {
// 		return fmt.Errorf("metrics CSV: %v", err)
// 	}
// 	defer f.Close()

// 	cw := csv.NewWriter(f)
// 	if needHeader {
// 		cw.Write(csvHeader)
// 	}

// 	statusStr := "success"
// 	if m.Error != "" {
// 		statusStr = "error"
// 	}
// 	degradedStr := "false"
// 	if m.DegradedDownload {
// 		degradedStr = "true"
// 	}

// 	cw.Write([]string{
// 		m.Timestamp.Format(time.RFC3339Nano),
// 		m.Source, m.Username, m.Operation, m.Filename,
// 		strconv.FormatInt(m.FileSizeBytes, 10),
// 		formatSize(m.FileSizeBytes),
// 		strconv.Itoa(m.StripeCount),
// 		strconv.Itoa(m.ChunkCount),
// 		fmt.Sprintf("%.3f", m.TotalLatencyMs),
// 		fmt.Sprintf("%.3f", m.MasterRPCLatencyMs),
// 		fmt.Sprintf("%.3f", m.DataTransferMs),
// 		fmt.Sprintf("%.3f", m.ParityComputeMs),
// 		fmt.Sprintf("%.2f", m.MasterOverheadPct),
// 		fmt.Sprintf("%.4f", m.ThroughputMBps),
// 		fmt.Sprintf("%.4f", m.BandwidthMBps),
// 		fmt.Sprintf("%.4f", m.ParallelSpeedup),
// 		strconv.Itoa(m.TotalChunksAttempted),
// 		strconv.Itoa(m.SuccessfulChunks),
// 		strconv.Itoa(m.ReconstructedChunks),
// 		fmt.Sprintf("%.2f", m.SuccessRate),
// 		degradedStr,
// 		fmt.Sprintf("%.3f", m.ReconstructionMs),
// 		fmt.Sprintf("%.2f", m.ReconstructionOverheadPct),
// 		strconv.Itoa(m.ConcurrentUsers),
// 		statusStr, m.Error,
// 	})
// 	cw.Flush()
// 	return cw.Error()
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // Record helpers
// // ─────────────────────────────────────────────────────────────────────────────

// // RecordMetrics — CLI: prints to stderr + CSV.
// func RecordMetrics(m OperationMetrics) {
// 	mstore.push(m)
// 	PrintMetrics(m)
// 	if err := AppendMetricsCSV(m); err != nil {
// 		fmt.Fprintf(os.Stderr, "Warning: metrics CSV write failed: %v\n", err)
// 	} else {
// 		fmt.Fprintf(os.Stderr, "Metrics saved to %s\n\n", metricsCSVFile)
// 	}
// }

// // RecordWebMetrics — web: CSV only (no terminal spam for concurrent requests).
// func RecordWebMetrics(m OperationMetrics) {
// 	mstore.push(m)
// 	if err := AppendMetricsCSV(m); err != nil {
// 		fmt.Fprintf(os.Stderr, "Warning: web metrics CSV write failed: %v\n", err)
// 	}
// }

// // ─────────────────────────────────────────────────────────────────────────────
// // HTTP handlers — plug into your existing router
// // ─────────────────────────────────────────────────────────────────────────────

// // MetricsSummary aggregates stats across all stored operations.
// type MetricsSummary struct {
// 	TotalOps      int     `json:"total_ops"`
// 	SuccessOps    int     `json:"success_ops"`
// 	FailedOps     int     `json:"failed_ops"`
// 	UploadOps     int     `json:"upload_ops"`
// 	DownloadOps   int     `json:"download_ops"`
// 	DeleteOps     int     `json:"delete_ops"`
// 	LsOps         int     `json:"ls_ops"`
// 	DegradedOps   int     `json:"degraded_ops"`
// 	AvgLatencyMs  float64 `json:"avg_latency_ms"`
// 	AvgThroughput float64 `json:"avg_throughput_mbps"`
// 	TotalBytes    int64   `json:"total_bytes_transferred"`
// 	MaxConcurrent int     `json:"max_concurrent_users_seen"`
// }

// // MetricsResponse is what GET /metrics returns.
// type MetricsResponse struct {
// 	ActiveUsers     map[string]int     `json:"active_users"`
// 	ActiveUserCount int                `json:"active_user_count"`
// 	TotalRecorded   int                `json:"total_recorded"`
// 	Summary         MetricsSummary     `json:"summary"`
// 	Recent          []OperationMetrics `json:"recent"` // last 100 entries
// }

// // HandleGetMetrics serves GET /metrics
// // Register: mux.HandleFunc("/metrics", requireAuth(HandleGetMetrics))
// func HandleGetMetrics(w http.ResponseWriter, r *http.Request) {
// 	if r.Method != http.MethodGet {
// 		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
// 		return
// 	}

// 	all := mstore.all()
// 	summary := MetricsSummary{}
// 	var sumLatency, sumThroughput float64
// 	tpCount := 0

// 	for _, m := range all {
// 		summary.TotalOps++
// 		if m.Error == "" {
// 			summary.SuccessOps++
// 		} else {
// 			summary.FailedOps++
// 		}
// 		sumLatency += m.TotalLatencyMs
// 		if m.ThroughputMBps > 0 {
// 			sumThroughput += m.ThroughputMBps
// 			tpCount++
// 		}
// 		summary.TotalBytes += m.FileSizeBytes
// 		if m.ConcurrentUsers > summary.MaxConcurrent {
// 			summary.MaxConcurrent = m.ConcurrentUsers
// 		}
// 		if m.DegradedDownload {
// 			summary.DegradedOps++
// 		}
// 		switch m.Operation {
// 		case "upload":
// 			summary.UploadOps++
// 		case "download":
// 			summary.DownloadOps++
// 		case "delete":
// 			summary.DeleteOps++
// 		case "ls":
// 			summary.LsOps++
// 		}
// 	}
// 	if summary.TotalOps > 0 {
// 		summary.AvgLatencyMs = sumLatency / float64(summary.TotalOps)
// 	}
// 	if tpCount > 0 {
// 		summary.AvgThroughput = sumThroughput / float64(tpCount)
// 	}

// 	recent := all
// 	if len(recent) > 100 {
// 		recent = recent[len(recent)-100:]
// 	}

// 	w.Header().Set("Content-Type", "application/json")
// 	w.Header().Set("Access-Control-Allow-Origin", "*")
// 	json.NewEncoder(w).Encode(MetricsResponse{
// 		ActiveUsers:     activeUsers.snapshot(),
// 		ActiveUserCount: activeUsers.count(),
// 		TotalRecorded:   len(all),
// 		Summary:         summary,
// 		Recent:          recent,
// 	})
// }

// // HandleGetMetricsCSV serves GET /metrics/csv — downloads the raw CSV.
// func HandleGetMetricsCSV(w http.ResponseWriter, r *http.Request) {
// 	if r.Method != http.MethodGet {
// 		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
// 		return
// 	}
// 	csvMu.Lock()
// 	data, err := os.ReadFile(metricsCSVFile)
// 	csvMu.Unlock()
// 	if err != nil {
// 		http.Error(w, "no metrics data yet", http.StatusNotFound)
// 		return
// 	}
// 	w.Header().Set("Content-Type", "text/csv")
// 	w.Header().Set("Content-Disposition", `attachment; filename="dfs_metrics.csv"`)
// 	w.Header().Set("Access-Control-Allow-Origin", "*")
// 	w.Write(data)
// }

package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
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
	Operation string    `json:"operation"` // "upload"|"download"|"delete"|"ls"
	Filename  string    `json:"filename"`
	Username  string    `json:"username"` // web user; empty for CLI
	Source    string    `json:"source"`   // "cli" | "web"
	Timestamp time.Time `json:"timestamp"`

	FileSizeBytes int64 `json:"file_size_bytes"`
	ChunkCount    int   `json:"chunk_count"`
	StripeCount   int   `json:"stripe_count"`

	TotalLatencyMs     float64 `json:"total_latency_ms"`
	MasterRPCLatencyMs float64 `json:"master_rpc_latency_ms"`
	DataTransferMs     float64 `json:"data_transfer_ms"`
	ParityComputeMs    float64 `json:"parity_compute_ms"`

	ThroughputMBps    float64 `json:"throughput_mbps"`
	BandwidthMBps     float64 `json:"bandwidth_mbps"`
	ParallelSpeedup   float64 `json:"parallel_speedup"`
	MasterOverheadPct float64 `json:"master_overhead_pct"`

	ReconstructionMs          float64 `json:"reconstruction_ms"`
	ReconstructionOverheadPct float64 `json:"reconstruction_overhead_pct"`
	DegradedDownload          bool    `json:"degraded_download"`

	TotalChunksAttempted int     `json:"chunks_attempted"`
	SuccessfulChunks     int     `json:"chunks_succeeded"`
	ReconstructedChunks  int     `json:"chunks_reconstructed"`
	SuccessRate          float64 `json:"success_rate_pct"`

	ConcurrentUsers int    `json:"concurrent_users"` // web only; 0 for CLI
	Error           string `json:"error"`
}

// ─────────────────────────────────────────────────────────────────────────────
// OperationContext
// ─────────────────────────────────────────────────────────────────────────────

type OperationContext struct {
	mu        sync.Mutex
	operation string
	filename  string
	username  string
	source    string
	fileSize  int64
	startTime time.Time

	masterRPCMs   float64
	dataXferMs    float64
	parityMs      float64
	reconstrMs    float64
	stripeCount   int
	chunkAttempts int
	chunkSuccess  int
	chunkReconstr int
}

// NewOpCtx — CLI operation.
func NewOpCtx(operation, filename string, fileSize int64) *OperationContext {
	return &OperationContext{
		operation: operation,
		filename:  filename,
		source:    "cli",
		fileSize:  fileSize,
		startTime: time.Now(),
	}
}

// NewWebOpCtx — web operation; increments the active-user counter.
func NewWebOpCtx(operation, filename, username string, fileSize int64) *OperationContext {
	activeUsers.inc(username)
	return &OperationContext{
		operation: operation,
		filename:  filename,
		username:  username,
		source:    "web",
		fileSize:  fileSize,
		startTime: time.Now(),
	}
}

func (c *OperationContext) SetFileSize(n int64)     { c.mu.Lock(); c.fileSize = n; c.mu.Unlock() }
func (c *OperationContext) AddMasterRPC(ms float64) { c.mu.Lock(); c.masterRPCMs += ms; c.mu.Unlock() }
func (c *OperationContext) AddDataXfer(ms float64)  { c.mu.Lock(); c.dataXferMs += ms; c.mu.Unlock() }
func (c *OperationContext) AddParity(ms float64)    { c.mu.Lock(); c.parityMs += ms; c.mu.Unlock() }
func (c *OperationContext) AddReconstruction(ms float64) {
	c.mu.Lock()
	c.reconstrMs += ms
	c.mu.Unlock()
}
func (c *OperationContext) AddStripes(n int) { c.mu.Lock(); c.stripeCount += n; c.mu.Unlock() }

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

// Finalise computes all derived metrics and returns a completed OperationMetrics.
// For web contexts it decrements the active-user counter.
func (c *OperationContext) Finalise(errMsg string) OperationMetrics {
	totalMs := float64(time.Since(c.startTime).Nanoseconds()) / 1e6

	concurrent := 0
	if c.source == "web" {
		concurrent = activeUsers.count()
		activeUsers.dec(c.username)
	}

	chunkCount := c.stripeCount * 3

	throughput := 0.0
	if totalMs > 0 && c.fileSize > 0 {
		throughput = (float64(c.fileSize) / 1e6) / (totalMs / 1e3)
	}
	bandwidth := 0.0
	if c.dataXferMs > 0 && c.fileSize > 0 {
		bandwidth = (float64(c.fileSize) / 1e6) / (c.dataXferMs / 1e3)
	}
	parallelSpeedup := 3.0
	if c.dataXferMs <= 0 {
		parallelSpeedup = 0.0
	}
	masterOH := 0.0
	if totalMs > 0 {
		masterOH = c.masterRPCMs / totalMs * 100.0
	}
	reconstrOH := 0.0
	if totalMs > 0 && c.reconstrMs > 0 {
		reconstrOH = c.reconstrMs / totalMs * 100.0
	}
	successRate := 0.0
	if c.chunkAttempts > 0 {
		successRate = float64(c.chunkSuccess) / float64(c.chunkAttempts) * 100.0
	}

	return OperationMetrics{
		Operation:                 c.operation,
		Filename:                  c.filename,
		Username:                  c.username,
		Source:                    c.source,
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
		MasterOverheadPct:         masterOH,
		ReconstructionMs:          c.reconstrMs,
		ReconstructionOverheadPct: reconstrOH,
		DegradedDownload:          c.reconstrMs > 0,
		TotalChunksAttempted:      c.chunkAttempts,
		SuccessfulChunks:          c.chunkSuccess,
		ReconstructedChunks:       c.chunkReconstr,
		SuccessRate:               successRate,
		ConcurrentUsers:           concurrent,
		Error:                     errMsg,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Active-user tracker
// ─────────────────────────────────────────────────────────────────────────────

type activeUserTracker struct {
	mu    sync.Mutex
	users map[string]int
}

func (t *activeUserTracker) inc(u string) { t.mu.Lock(); t.users[u]++; t.mu.Unlock() }
func (t *activeUserTracker) dec(u string) {
	t.mu.Lock()
	t.users[u]--
	if t.users[u] <= 0 {
		delete(t.users, u)
	}
	t.mu.Unlock()
}
func (t *activeUserTracker) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.users)
}
func (t *activeUserTracker) snapshot() map[string]int {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := make(map[string]int, len(t.users))
	for k, v := range t.users {
		s[k] = v
	}
	return s
}

var activeUsers = &activeUserTracker{users: make(map[string]int)}

// ─────────────────────────────────────────────────────────────────────────────
// In-memory store — last 1000 operations for the /metrics API
// ─────────────────────────────────────────────────────────────────────────────

const maxStored = 1000

type metricsStore struct {
	mu      sync.RWMutex
	entries []OperationMetrics
}

func (s *metricsStore) push(m OperationMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, m)
	if len(s.entries) > maxStored {
		s.entries = s.entries[len(s.entries)-maxStored:]
	}
}

func (s *metricsStore) all() []OperationMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]OperationMetrics, len(s.entries))
	copy(cp, s.entries)
	return cp
}

var mstore = &metricsStore{}

// ─────────────────────────────────────────────────────────────────────────────
// phaseTimer
// ─────────────────────────────────────────────────────────────────────────────

type phaseTimer struct{ start time.Time }

func newPhaseTimer() *phaseTimer         { return &phaseTimer{start: time.Now()} }
func (p *phaseTimer) ElapsedMs() float64 { return float64(time.Since(p.start).Nanoseconds()) / 1e6 }

// formatSize converts bytes to a human-readable string (B / KB / MB / GB).
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

// ─────────────────────────────────────────────────────────────────────────────
// Terminal output (CLI)
// ─────────────────────────────────────────────────────────────────────────────

func PrintMetrics(m OperationMetrics) {
	statusIcon := "SUCCESS"
	if m.Error != "" {
		statusIcon = "FAILED: " + m.Error
	}
	w := os.Stderr
	row := func(label, val string) { fmt.Fprintf(w, "  %-24s: %s\n", label, val) }

	fmt.Fprintln(w, "\n=== XorFS PERFORMANCE METRICS ===")
	row("Operation", m.Operation)
	row("File", m.Filename)
	if m.Username != "" {
		row("User", m.Username)
	}
	row("Source", m.Source)
	row("Status", statusIcon)
	row("Timestamp", m.Timestamp.Format("2006-01-02 15:04:05.000"))
	if m.ConcurrentUsers > 0 {
		row("Concurrent users", strconv.Itoa(m.ConcurrentUsers))
	}
	fmt.Fprintln(w, "--- FILE ---")
	row("Size", formatSize(m.FileSizeBytes))
	row("Stripes", strconv.Itoa(m.StripeCount))
	row("Chunks", fmt.Sprintf("%d (data=%d parity=%d)", m.ChunkCount, m.StripeCount*2, m.StripeCount))
	fmt.Fprintln(w, "--- LATENCY ---")
	row("Total", fmt.Sprintf("%.3f ms", m.TotalLatencyMs))
	row("Master RPC", fmt.Sprintf("%.3f ms (%.1f%%)", m.MasterRPCLatencyMs, m.MasterOverheadPct))
	row("Data transfer", fmt.Sprintf("%.3f ms", m.DataTransferMs))
	if m.Operation == "upload" {
		row("Parity compute", fmt.Sprintf("%.3f ms", m.ParityComputeMs))
	}
	fmt.Fprintln(w, "--- THROUGHPUT ---")
	row("Throughput", fmt.Sprintf("%.4f MB/s", m.ThroughputMBps))
	row("Bandwidth", fmt.Sprintf("%.4f MB/s", m.BandwidthMBps))
	fmt.Fprintln(w, "--- RELIABILITY ---")
	row("Chunks attempted", strconv.Itoa(m.TotalChunksAttempted))
	row("Chunks succeeded", strconv.Itoa(m.SuccessfulChunks))
	row("RAID-5 reconstructed", strconv.Itoa(m.ReconstructedChunks))
	row("Success rate", fmt.Sprintf("%.1f%%", m.SuccessRate))
	if m.DegradedDownload {
		fmt.Fprintln(w, "--- RECONSTRUCTION ---")
		row("Mode", "DEGRADED")
		row("Recon time", fmt.Sprintf("%.3f ms", m.ReconstructionMs))
		row("Recon overhead", fmt.Sprintf("%.2f%%", m.ReconstructionOverheadPct))
	}
	fmt.Fprintln(w, "=================================\n")
}

// ─────────────────────────────────────────────────────────────────────────────
// CSV — mutex-protected for concurrent web requests
// ─────────────────────────────────────────────────────────────────────────────

const metricsCSVFile = "dfs_metrics.csv"

var csvMu sync.Mutex

var csvHeader = []string{
	"timestamp", "source", "username", "operation", "filename",
	"file_size_bytes", "file_size_human", "stripe_count", "chunk_count",
	"total_latency_ms", "master_rpc_latency_ms", "data_transfer_ms", "parity_compute_ms",
	"master_overhead_pct", "throughput_mbps", "bandwidth_mbps", "parallel_speedup",
	"chunks_attempted", "chunks_succeeded", "chunks_reconstructed", "success_rate_pct",
	"degraded_download", "reconstruction_ms", "reconstruction_overhead_pct",
	"concurrent_users", "status", "error",
}

func AppendMetricsCSV(m OperationMetrics) error {
	csvMu.Lock()
	defer csvMu.Unlock()

	needHeader := false
	if _, err := os.Stat(metricsCSVFile); os.IsNotExist(err) {
		needHeader = true
	}
	f, err := os.OpenFile(metricsCSVFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("metrics CSV: %v", err)
	}
	defer f.Close()

	cw := csv.NewWriter(f)
	if needHeader {
		cw.Write(csvHeader)
	}

	statusStr := "success"
	if m.Error != "" {
		statusStr = "error"
	}
	degradedStr := "false"
	if m.DegradedDownload {
		degradedStr = "true"
	}

	cw.Write([]string{
		m.Timestamp.Format(time.RFC3339Nano),
		m.Source, m.Username, m.Operation, m.Filename,
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
		strconv.Itoa(m.ConcurrentUsers),
		statusStr, m.Error,
	})
	cw.Flush()
	return cw.Error()
}

// ─────────────────────────────────────────────────────────────────────────────
// Record helpers
// ─────────────────────────────────────────────────────────────────────────────

// RecordMetrics — CLI: prints to stderr + CSV.
func RecordMetrics(m OperationMetrics) {
	mstore.push(m)
	PrintMetrics(m)
	if err := AppendMetricsCSV(m); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: metrics CSV write failed: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "Metrics saved to %s\n\n", metricsCSVFile)
	}
}

// RecordWebMetrics — web: CSV only (no terminal spam for concurrent requests).
func RecordWebMetrics(m OperationMetrics) {
	mstore.push(m)
	if err := AppendMetricsCSV(m); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: web metrics CSV write failed: %v\n", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP handlers — plug into your existing router
// ─────────────────────────────────────────────────────────────────────────────

// MetricsSummary aggregates stats across all stored operations.
type MetricsSummary struct {
	TotalOps      int     `json:"total_ops"`
	SuccessOps    int     `json:"success_ops"`
	FailedOps     int     `json:"failed_ops"`
	UploadOps     int     `json:"upload_ops"`
	DownloadOps   int     `json:"download_ops"`
	DeleteOps     int     `json:"delete_ops"`
	LsOps         int     `json:"ls_ops"`
	DegradedOps   int     `json:"degraded_ops"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	AvgThroughput float64 `json:"avg_throughput_mbps"`
	TotalBytes    int64   `json:"total_bytes_transferred"`
	MaxConcurrent int     `json:"max_concurrent_users_seen"`
}

// MetricsResponse is what GET /metrics returns.
type MetricsResponse struct {
	ActiveUsers     map[string]int     `json:"active_users"`
	ActiveUserCount int                `json:"active_user_count"`
	TotalRecorded   int                `json:"total_recorded"`
	Summary         MetricsSummary     `json:"summary"`
	Recent          []OperationMetrics `json:"recent"` // last 100 entries
}

// HandleGetMetrics serves GET /metrics
// Register: mux.HandleFunc("/metrics", requireAuth(HandleGetMetrics))
func HandleGetMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	all := mstore.all()
	summary := MetricsSummary{}
	var sumLatency, sumThroughput float64
	tpCount := 0

	for _, m := range all {
		summary.TotalOps++
		if m.Error == "" {
			summary.SuccessOps++
		} else {
			summary.FailedOps++
		}
		sumLatency += m.TotalLatencyMs
		if m.ThroughputMBps > 0 {
			sumThroughput += m.ThroughputMBps
			tpCount++
		}
		summary.TotalBytes += m.FileSizeBytes
		if m.ConcurrentUsers > summary.MaxConcurrent {
			summary.MaxConcurrent = m.ConcurrentUsers
		}
		if m.DegradedDownload {
			summary.DegradedOps++
		}
		switch m.Operation {
		case "upload":
			summary.UploadOps++
		case "download":
			summary.DownloadOps++
		case "delete":
			summary.DeleteOps++
		case "ls":
			summary.LsOps++
		}
	}
	if summary.TotalOps > 0 {
		summary.AvgLatencyMs = sumLatency / float64(summary.TotalOps)
	}
	if tpCount > 0 {
		summary.AvgThroughput = sumThroughput / float64(tpCount)
	}

	recent := all
	if len(recent) > 100 {
		recent = recent[len(recent)-100:]
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(MetricsResponse{
		ActiveUsers:     activeUsers.snapshot(),
		ActiveUserCount: activeUsers.count(),
		TotalRecorded:   len(all),
		Summary:         summary,
		Recent:          recent,
	})
}

// HandleGetMetricsCSV serves GET /metrics/csv — downloads the raw CSV.
func HandleGetMetricsCSV(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	csvMu.Lock()
	data, err := os.ReadFile(metricsCSVFile)
	csvMu.Unlock()
	if err != nil {
		http.Error(w, "no metrics data yet", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="dfs_metrics.csv"`)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(data)
}
