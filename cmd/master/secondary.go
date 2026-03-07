package main

import (
	"context"
	"dfs-project/dfspb"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// SecondaryMaster implements the SecondaryMasterServer gRPC service.
// It receives WAL entries and heartbeats from the primary.
// When heartbeats stop arriving it promotes itself to primary.
type SecondaryMaster struct {
	dfspb.UnimplementedSecondaryMasterServerServer

	mu              sync.Mutex
	lastHeartbeat   time.Time
	lastSeqReceived uint64
	primaryAddr     string

	// Pointer to the shared MasterServer so that on promotion we can
	// flip isPrimary and start serving client RPCs from the same state.
	master *MasterServer
}

// NewSecondaryMaster creates a SecondaryMaster watching over the given MasterServer state.
func NewSecondaryMaster(master *MasterServer) *SecondaryMaster {
	return &SecondaryMaster{
		master:        master,
		lastHeartbeat: time.Now(), // grace period on startup
	}
}

// ReplicateWAL is called by the primary after every durable WAL write.
// We decode the payload and replay it into our in-memory state so we stay in sync.
func (s *SecondaryMaster) ReplicateWAL(ctx context.Context, req *dfspb.ReplicateWALRequest) (*dfspb.ReplicateWALResponse, error) {
	entry := req.Entry

	// Decode the JSON WAL entry the primary serialised into payload
	var walEntry WALEntry
	if err := json.Unmarshal(entry.Payload, &walEntry); err != nil {
		return nil, fmt.Errorf("failed to decode WAL payload: %v", err)
	}

	// Replay into master state (same functions used during crash recovery)
	s.master.walMu.Lock()
	s.master.walSeq = entry.SequenceNumber
	s.master.walMu.Unlock()

	// Replay uses the existing WAL recovery helpers — no lock needed here
	// because replayOperation acquires no locks itself (it only updates maps).
	// We take the master's data lock for safety.
	s.master.mu.Lock()
	err := s.master.replayOperation(&walEntry)
	s.master.mu.Unlock()

	if err != nil {
		s.master.logger.Printf("Secondary: failed to replay WAL seq %d: %v", entry.SequenceNumber, err)
		return &dfspb.ReplicateWALResponse{Success: false}, err
	}

	s.mu.Lock()
	s.lastSeqReceived = entry.SequenceNumber
	s.mu.Unlock()

	s.master.logger.Printf("Secondary: applied WAL seq %d (op type %v)", entry.SequenceNumber, entry.EntryType)

	return &dfspb.ReplicateWALResponse{
		Success:         true,
		LastSequenceAck: entry.SequenceNumber,
	}, nil
}

// SendMasterHeartbeat is called by the primary every few seconds.
// We just record the timestamp; the watchdog goroutine checks it.
func (s *SecondaryMaster) SendMasterHeartbeat(ctx context.Context, req *dfspb.MasterHeartbeatRequest) (*dfspb.MasterHeartbeatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastHeartbeat = time.Now()
	s.primaryAddr = req.PrimaryAddr

	s.master.logger.Printf("Secondary: heartbeat from primary %s (primary wal_seq=%d, my wal_seq=%d)",
		req.PrimaryAddr, req.LastWalSequence, s.lastSeqReceived)

	return &dfspb.MasterHeartbeatResponse{
		Success:              true,
		LastReceivedSequence: s.lastSeqReceived,
	}, nil
}

// ApplyCheckpoint loads a full state snapshot sent by the primary.
// Called periodically so secondary doesn't need to replay WAL from scratch.
func (s *SecondaryMaster) ApplyCheckpoint(ctx context.Context, req *dfspb.CheckpointRequest) (*dfspb.CheckpointResponse, error) {
	s.master.logger.Printf("Secondary: applying checkpoint at seq %d (%d bytes)",
		req.SequenceNumber, len(req.StateData))

	var checkpoint Checkpoint
	if err := json.Unmarshal(req.StateData, &checkpoint); err != nil {
		return &dfspb.CheckpointResponse{Success: false}, fmt.Errorf("bad checkpoint payload: %v", err)
	}

	s.master.mu.Lock()
	defer s.master.mu.Unlock()

	// Restore all maps (same logic as LoadCheckpoint)
	s.master.clientIDs = checkpoint.ClientIDs
	s.master.fileSizes = checkpoint.FileSizes
	s.master.chunkStatus = checkpoint.ChunkStatus
	s.master.generation = checkpoint.Generation
	if checkpoint.ClientFolders != nil {
		s.master.clientFolders = checkpoint.ClientFolders
	}
	if checkpoint.FileUploadTimes != nil {
		s.master.fileUploadTimes = checkpoint.FileUploadTimes
	}
	if checkpoint.ClientUsernames != nil {
		s.master.clientUsernames = checkpoint.ClientUsernames
	}
	s.master.fileInfo = make(map[int64]map[string]map[int32]*dfspb.StripeMetadata)

	for clientID, filesJSON := range checkpoint.FileInfo {
		s.master.fileInfo[clientID] = make(map[string]map[int32]*dfspb.StripeMetadata)
		for filename, stripesJSON := range filesJSON {
			s.master.fileInfo[clientID][filename] = make(map[int32]*dfspb.StripeMetadata)
			for stripeNum, sj := range stripesJSON {
				s.master.fileInfo[clientID][filename][stripeNum] = &dfspb.StripeMetadata{
					StripeNum: sj.StripeNum,
					ChunkIds:  sj.ChunkIds,
					Servers:   sj.Servers,
				}
			}
		}
	}

	s.master.walSeq = req.SequenceNumber
	s.master.logger.Printf("Secondary: checkpoint applied — %d clients, %d chunks",
		len(s.master.clientIDs), len(s.master.chunkStatus))

	return &dfspb.CheckpointResponse{Success: true}, nil
}

// RequestStateSync is called by a returning/restarting master that was previously primary.
// It returns the full current state (checkpoint bytes + sequence + generation) so the
// returning node can catch up before deciding whether to resume or stay as secondary.
func (s *SecondaryMaster) RequestStateSync(ctx context.Context, req *dfspb.GetActiveMasterRequest) (*dfspb.CheckpointRequest, error) {
	s.master.logger.Printf("Secondary: RequestStateSync called — serialising full state (gen=%d, wal_seq=%d)",
		s.master.generation, s.master.walSeq)

	// Produce a fresh checkpoint bytes
	s.master.mu.Lock()

	// Build fileInfoJSON (same as CreateCheckpoint)
	fileInfoJSON := make(map[int64]map[string]map[int32]*StripeMetadataJSON)
	for clientID := range s.master.fileInfo {
		fileInfoJSON[clientID] = make(map[string]map[int32]*StripeMetadataJSON)
		for filename, stripes := range s.master.fileInfo[clientID] {
			fileInfoJSON[clientID][filename] = make(map[int32]*StripeMetadataJSON)
			for stripeNum, stripe := range stripes {
				fileInfoJSON[clientID][filename][stripeNum] = &StripeMetadataJSON{
					StripeNum: stripe.StripeNum,
					ChunkIds:  stripe.ChunkIds,
					Servers:   stripe.Servers,
				}
			}
		}
	}

	checkpoint := Checkpoint{
		Timestamp:       time.Now().Unix(),
		Generation:      s.master.generation,
		WALSeq:          s.master.walSeq,
		FileInfo:        fileInfoJSON,
		ClientIDs:       s.master.clientIDs,
		FileSizes:       s.master.fileSizes,
		ChunkStatus:     s.master.chunkStatus,
		ClientFolders:   s.master.clientFolders,
		FileUploadTimes: s.master.fileUploadTimes,
		ClientUsernames: s.master.clientUsernames,
	}
	seq := s.master.walSeq
	s.master.mu.Unlock()

	data, err := json.Marshal(checkpoint)
	if err != nil {
		return nil, fmt.Errorf("RequestStateSync: failed to marshal state: %v", err)
	}

	s.master.logger.Printf("Secondary: RequestStateSync response ready (%d bytes, gen=%d, seq=%d)",
		len(data), checkpoint.Generation, seq)

	return &dfspb.CheckpointRequest{
		SequenceNumber: seq,
		StateData:      data,
	}, nil
}

// WatchdogLoop runs in a goroutine on the secondary.
// If no heartbeat arrives from the primary within the timeout it promotes itself.
func (s *SecondaryMaster) WatchdogLoop(timeoutSeconds int) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	s.master.logger.Printf("Secondary watchdog started (timeout=%ds)", timeoutSeconds)

	for range ticker.C {
		s.mu.Lock()
		elapsed := time.Since(s.lastHeartbeat)
		s.mu.Unlock()

		if elapsed > time.Duration(timeoutSeconds)*time.Second {
			s.master.logger.Printf("FAILOVER: primary heartbeat missing for %.1fs — promoting to primary", elapsed.Seconds())
			s.promote()
			return // watchdog exits after promotion
		}
	}
}

// promote flips this node from standby to active primary.
// After this call, the node's MasterServer will start accepting full client RPCs.
func (s *SecondaryMaster) promote() {
	s.master.mu.Lock()
	s.master.isPrimary = true
	s.master.generation++       // New epoch: every promotion increments generation
	s.master.secondaryAddr = "" // no secondary below us (single failover for now)
	s.master.mu.Unlock()

	// Persist our current state as a checkpoint so we can recover if WE crash
	if err := s.master.CreateCheckpoint("master.checkpoint"); err != nil {
		s.master.logger.Printf("FAILOVER: checkpoint on promotion failed: %v", err)
	}

	s.master.logger.Printf("FAILOVER COMPLETE: this node is now the active primary (wal_seq=%d, generation=%d)", s.master.walSeq, s.master.generation)
	fmt.Println(">>> THIS NODE IS NOW THE ACTIVE PRIMARY MASTER <<<")
}
