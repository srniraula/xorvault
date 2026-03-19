package dfsclient

import "time"

// TransferStats carries detailed timing and reliability metrics from a single
// UploadFile or DownloadFile call back to the caller (e.g. the webserver).
// All duration fields are in milliseconds.
type TransferStats struct {
	// Phase timings
	MasterRPCMs      float64 // time spent in master RPCs (CreateFile/ConfirmWrite or GetFileMetadata)
	DataTransferMs   float64 // time spent writing/reading chunks to/from chunkservers
	ParityComputeMs  float64 // XOR parity compute time (upload only)
	ReconstructionMs float64 // XOR reconstruction time (degraded download only)

	// Counts
	StripeCount         int
	ChunksAttempted     int
	ChunksSucceeded     int
	ChunksReconstructed int
}

// phaseTimer is a simple stopwatch for measuring sub-phases.
type phaseTimer struct{ start time.Time }

func newPhaseTimer() *phaseTimer         { return &phaseTimer{start: time.Now()} }
func (p *phaseTimer) ElapsedMs() float64 { return float64(time.Since(p.start).Nanoseconds()) / 1e6 }
