// // package main

// // import (
// // 	"context"
// // 	"dfs-project/dfspb"
// // 	"encoding/json"
// // 	"fmt"
// // 	"os"
// // 	"sync"
// // 	"time"
// // )

// // // SecondaryMaster implements the SecondaryMasterServer gRPC service.
// // // It receives WAL entries and heartbeats from the primary.
// // // When heartbeats stop arriving it promotes itself to primary.
// // type SecondaryMaster struct {
// // 	dfspb.UnimplementedSecondaryMasterServerServer

// // 	mu              sync.Mutex
// // 	lastHeartbeat   time.Time
// // 	lastSeqReceived uint64
// // 	primaryAddr     string

// // 	// Pointer to the shared MasterServer so that on promotion we can
// // 	// flip isPrimary and start serving client RPCs from the same state.
// // 	master *MasterServer
// // }

// // // NewSecondaryMaster creates a SecondaryMaster watching over the given MasterServer state.
// // func NewSecondaryMaster(master *MasterServer) *SecondaryMaster {
// // 	return &SecondaryMaster{
// // 		master:        master,
// // 		lastHeartbeat: time.Now(), // grace period on startup
// // 	}
// // }

// // // ReplicateWAL is called by the primary after every durable WAL write.
// // // We decode the payload and replay it into our in-memory state so we stay in sync.
// // func (s *SecondaryMaster) ReplicateWAL(ctx context.Context, req *dfspb.ReplicateWALRequest) (*dfspb.ReplicateWALResponse, error) {
// // 	entry := req.Entry

// // 	// Decode the JSON WAL entry the primary serialised into payload
// // 	var walEntry WALEntry
// // 	if err := json.Unmarshal(entry.Payload, &walEntry); err != nil {
// // 		return nil, fmt.Errorf("failed to decode WAL payload: %v", err)
// // 	}

// // 	// Replay into master state (same functions used during crash recovery)
// // 	s.master.walMu.Lock()
// // 	s.master.walSeq = entry.SequenceNumber
// // 	s.master.walMu.Unlock()

// // 	// Replay uses the existing WAL recovery helpers — no lock needed here
// // 	// because replayOperation acquires no locks itself (it only updates maps).
// // 	// We take the master's data lock for safety.
// // 	s.master.mu.Lock()
// // 	err := s.master.replayOperation(&walEntry)
// // 	s.master.mu.Unlock()

// // 	if err != nil {
// // 		s.master.logger.Printf("Secondary: failed to replay WAL seq %d: %v", entry.SequenceNumber, err)
// // 		return &dfspb.ReplicateWALResponse{Success: false}, err
// // 	}

// // 	s.mu.Lock()
// // 	s.lastSeqReceived = entry.SequenceNumber
// // 	s.mu.Unlock()

// // 	s.master.logger.Printf("Secondary: applied WAL seq %d (op type %v)", entry.SequenceNumber, entry.EntryType)

// // 	return &dfspb.ReplicateWALResponse{
// // 		Success:         true,
// // 		LastSequenceAck: entry.SequenceNumber,
// // 	}, nil
// // }

// // // SendMasterHeartbeat is called by the primary every few seconds.
// // // We just record the timestamp; the watchdog goroutine checks it.
// // func (s *SecondaryMaster) SendMasterHeartbeat(ctx context.Context, req *dfspb.MasterHeartbeatRequest) (*dfspb.MasterHeartbeatResponse, error) {
// // 	s.mu.Lock()
// // 	defer s.mu.Unlock()

// // 	s.lastHeartbeat = time.Now()
// // 	s.primaryAddr = req.PrimaryAddr

// // 	s.master.logger.Printf("Secondary: heartbeat from primary %s (primary wal_seq=%d, my wal_seq=%d)",
// // 		req.PrimaryAddr, req.LastWalSequence, s.lastSeqReceived)

// // 	return &dfspb.MasterHeartbeatResponse{
// // 		Success:              true,
// // 		LastReceivedSequence: s.lastSeqReceived,
// // 	}, nil
// // }

// // // ApplyCheckpoint loads a full state snapshot sent by the primary.
// // // Called periodically so secondary doesn't need to replay WAL from scratch.
// // func (s *SecondaryMaster) ApplyCheckpoint(ctx context.Context, req *dfspb.CheckpointRequest) (*dfspb.CheckpointResponse, error) {
// // 	s.master.logger.Printf("Secondary: applying checkpoint at seq %d (%d bytes)",
// // 		req.SequenceNumber, len(req.StateData))

// // 	var checkpoint Checkpoint
// // 	if err := json.Unmarshal(req.StateData, &checkpoint); err != nil {
// // 		return &dfspb.CheckpointResponse{Success: false}, fmt.Errorf("bad checkpoint payload: %v", err)
// // 	}

// // 	s.master.mu.Lock()
// // 	defer s.master.mu.Unlock()

// // 	// Restore all maps (same logic as LoadCheckpoint)
// // 	s.master.clientIDs = checkpoint.ClientIDs
// // 	s.master.fileSizes = checkpoint.FileSizes
// // 	s.master.chunkStatus = checkpoint.ChunkStatus
// // 	s.master.generation = checkpoint.Generation
// // 	if checkpoint.ClientFolders != nil {
// // 		s.master.clientFolders = checkpoint.ClientFolders
// // 	}
// // 	if checkpoint.FileUploadTimes != nil {
// // 		s.master.fileUploadTimes = checkpoint.FileUploadTimes
// // 	}
// // 	if checkpoint.ClientUsernames != nil {
// // 		s.master.clientUsernames = checkpoint.ClientUsernames
// // 	}
// // 	s.master.fileInfo = make(map[int64]map[string]map[int32]*dfspb.StripeMetadata)

// // 	for clientID, filesJSON := range checkpoint.FileInfo {
// // 		s.master.fileInfo[clientID] = make(map[string]map[int32]*dfspb.StripeMetadata)
// // 		for filename, stripesJSON := range filesJSON {
// // 			s.master.fileInfo[clientID][filename] = make(map[int32]*dfspb.StripeMetadata)
// // 			for stripeNum, sj := range stripesJSON {
// // 				s.master.fileInfo[clientID][filename][stripeNum] = &dfspb.StripeMetadata{
// // 					StripeNum: sj.StripeNum,
// // 					ChunkIds:  sj.ChunkIds,
// // 					Servers:   sj.Servers,
// // 				}
// // 			}
// // 		}
// // 	}

// // 	s.master.walSeq = req.SequenceNumber
// // 	s.master.logger.Printf("Secondary: checkpoint applied — %d clients, %d chunks",
// // 		len(s.master.clientIDs), len(s.master.chunkStatus))

// // 	return &dfspb.CheckpointResponse{Success: true}, nil
// // }

// // // RequestStateSync is called by a returning/restarting master that was previously primary.
// // // It returns the full current state (checkpoint bytes + sequence + generation) so the
// // // returning node can catch up before deciding whether to resume or stay as secondary.
// // func (s *SecondaryMaster) RequestStateSync(ctx context.Context, req *dfspb.GetActiveMasterRequest) (*dfspb.CheckpointRequest, error) {
// // 	s.master.logger.Printf("Secondary: RequestStateSync called — serialising full state (gen=%d, wal_seq=%d)",
// // 		s.master.generation, s.master.walSeq)

// // 	// Produce a fresh checkpoint bytes
// // 	s.master.mu.Lock()

// // 	// Build fileInfoJSON (same as CreateCheckpoint)
// // 	fileInfoJSON := make(map[int64]map[string]map[int32]*StripeMetadataJSON)
// // 	for clientID := range s.master.fileInfo {
// // 		fileInfoJSON[clientID] = make(map[string]map[int32]*StripeMetadataJSON)
// // 		for filename, stripes := range s.master.fileInfo[clientID] {
// // 			fileInfoJSON[clientID][filename] = make(map[int32]*StripeMetadataJSON)
// // 			for stripeNum, stripe := range stripes {
// // 				fileInfoJSON[clientID][filename][stripeNum] = &StripeMetadataJSON{
// // 					StripeNum: stripe.StripeNum,
// // 					ChunkIds:  stripe.ChunkIds,
// // 					Servers:   stripe.Servers,
// // 				}
// // 			}
// // 		}
// // 	}

// // 	checkpoint := Checkpoint{
// // 		Timestamp:       time.Now().Unix(),
// // 		Generation:      s.master.generation,
// // 		WALSeq:          s.master.walSeq,
// // 		FileInfo:        fileInfoJSON,
// // 		ClientIDs:       s.master.clientIDs,
// // 		FileSizes:       s.master.fileSizes,
// // 		ChunkStatus:     s.master.chunkStatus,
// // 		ClientFolders:   s.master.clientFolders,
// // 		FileUploadTimes: s.master.fileUploadTimes,
// // 		ClientUsernames: s.master.clientUsernames,
// // 	}
// // 	seq := s.master.walSeq
// // 	s.master.mu.Unlock()

// // 	data, err := json.Marshal(checkpoint)
// // 	if err != nil {
// // 		return nil, fmt.Errorf("RequestStateSync: failed to marshal state: %v", err)
// // 	}

// // 	s.master.logger.Printf("Secondary: RequestStateSync response ready (%d bytes, gen=%d, seq=%d)",
// // 		len(data), checkpoint.Generation, seq)

// // 	return &dfspb.CheckpointRequest{
// // 		SequenceNumber: seq,
// // 		StateData:      data,
// // 	}, nil
// // }

// // // WatchdogLoop runs in a goroutine on the secondary.
// // // If no heartbeat arrives from the primary within the timeout it promotes itself.
// // func (s *SecondaryMaster) WatchdogLoop(timeoutSeconds int) {
// // 	ticker := time.NewTicker(time.Second)
// // 	defer ticker.Stop()

// // 	s.master.logger.Printf("Secondary watchdog started (timeout=%ds)", timeoutSeconds)

// // 	for range ticker.C {
// // 		s.mu.Lock()
// // 		elapsed := time.Since(s.lastHeartbeat)
// // 		s.mu.Unlock()

// // 		if elapsed > time.Duration(timeoutSeconds)*time.Second {
// // 			s.master.logger.Printf("FAILOVER: primary heartbeat missing for %.1fs — promoting to primary", elapsed.Seconds())
// // 			s.promote()
// // 			return // watchdog exits after promotion
// // 		}
// // 	}
// // }

// // // promote flips this node from standby to active primary.
// // // After this call, the node's MasterServer will start accepting full client RPCs
// // // and begin sending heartbeats/WAL to the peer in case it comes back online.
// // func (s *SecondaryMaster) promote() {
// // 	s.master.mu.Lock()
// // 	s.master.isPrimary = true
// // 	s.master.generation++ // New epoch: every promotion increments generation

// // 	// Set secondaryAddr to the peer so WAL replication targets the correct node.
// // 	// Use peerAddr (always set from -secondary flag); fall back to the address
// // 	// we received in heartbeats from the old primary.
// // 	peerAddr := s.master.peerAddr
// // 	if peerAddr == "" {
// // 		peerAddr = s.primaryAddr
// // 	}
// // 	s.master.secondaryAddr = peerAddr
// // 	s.master.mu.Unlock()

// // 	// Persist our current state as a checkpoint so we can recover if WE crash
// // 	if err := s.master.CreateCheckpoint("master.checkpoint"); err != nil {
// // 		s.master.logger.Printf("FAILOVER: checkpoint on promotion failed: %v", err)
// // 	}

// // 	// Start sending heartbeats to the peer (old primary) so it can sync when
// // 	// it comes back online and remain as standby.
// // 	if peerAddr != "" {
// // 		go s.master.SendHeartbeatsToSecondary(peerAddr)
// // 		s.master.logger.Printf("FAILOVER: started sending heartbeats to peer %s", peerAddr)
// // 	}

// // 	s.master.logger.Printf("FAILOVER COMPLETE: this node is now the active primary (wal_seq=%d, generation=%d)", s.master.walSeq, s.master.generation)
// // 	fmt.Fprintf(os.Stderr, "\n")
// // 	fmt.Fprintf(os.Stderr, "╔══════════════════════════════════════════════════════════╗\n")
// // 	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
// // 	fmt.Fprintf(os.Stderr, "║   🔴  FAILOVER — THIS NODE IS NOW THE ACTIVE PRIMARY  🔴 ║\n")
// // 	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
// // 	fmt.Fprintf(os.Stderr, "║   Address  : %-42s  ║\n", s.master.myAddr)
// // 	fmt.Fprintf(os.Stderr, "║   Generation: %-41d  ║\n", s.master.generation)
// // 	fmt.Fprintf(os.Stderr, "║   WAL Seq  : %-42d  ║\n", s.master.walSeq)
// // 	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
// // 	fmt.Fprintf(os.Stderr, "╚══════════════════════════════════════════════════════════╝\n")
// // 	fmt.Fprintf(os.Stderr, "\n")
// // }

// package main

// import (
// 	"context"
// 	"dfs-project/dfspb"
// 	"encoding/json"
// 	"fmt"
// 	"os"
// 	"sync"
// 	"time"

// 	"google.golang.org/grpc"
// 	"google.golang.org/grpc/credentials/insecure"
// )

// // SecondaryMaster implements the SecondaryMasterServer gRPC service.
// // It receives WAL entries and heartbeats from the primary.
// // When heartbeats stop arriving it promotes itself to primary.
// type SecondaryMaster struct {
// 	dfspb.UnimplementedSecondaryMasterServerServer

// 	mu              sync.Mutex
// 	lastHeartbeat   time.Time
// 	lastSeqReceived uint64
// 	primaryAddr     string

// 	// Pointer to the shared MasterServer so that on promotion we can
// 	// flip isPrimary and start serving client RPCs from the same state.
// 	master *MasterServer
// }

// // NewSecondaryMaster creates a SecondaryMaster watching over the given MasterServer state.
// func NewSecondaryMaster(master *MasterServer) *SecondaryMaster {
// 	return &SecondaryMaster{
// 		master:        master,
// 		lastHeartbeat: time.Now(), // grace period on startup
// 	}
// }

// // ReplicateWAL is called by the primary after every durable WAL write.
// // We decode the payload and replay it into our in-memory state so we stay in sync.
// func (s *SecondaryMaster) ReplicateWAL(ctx context.Context, req *dfspb.ReplicateWALRequest) (*dfspb.ReplicateWALResponse, error) {
// 	entry := req.Entry

// 	// Decode the JSON WAL entry the primary serialised into payload
// 	var walEntry WALEntry
// 	if err := json.Unmarshal(entry.Payload, &walEntry); err != nil {
// 		return nil, fmt.Errorf("failed to decode WAL payload: %v", err)
// 	}

// 	// Replay into master state (same functions used during crash recovery)
// 	s.master.walMu.Lock()
// 	s.master.walSeq = entry.SequenceNumber
// 	s.master.walMu.Unlock()

// 	// Replay uses the existing WAL recovery helpers — no lock needed here
// 	// because replayOperation acquires no locks itself (it only updates maps).
// 	// We take the master's data lock for safety.
// 	s.master.mu.Lock()
// 	err := s.master.replayOperation(&walEntry)
// 	s.master.mu.Unlock()

// 	if err != nil {
// 		s.master.logger.Printf("Secondary: failed to replay WAL seq %d: %v", entry.SequenceNumber, err)
// 		return &dfspb.ReplicateWALResponse{Success: false}, err
// 	}

// 	s.mu.Lock()
// 	s.lastSeqReceived = entry.SequenceNumber
// 	s.mu.Unlock()

// 	s.master.logger.Printf("Secondary: applied WAL seq %d (op type %v)", entry.SequenceNumber, entry.EntryType)

// 	return &dfspb.ReplicateWALResponse{
// 		Success:         true,
// 		LastSequenceAck: entry.SequenceNumber,
// 	}, nil
// }

// // SendMasterHeartbeat is called by the primary every few seconds.
// // We just record the timestamp; the watchdog goroutine checks it.
// func (s *SecondaryMaster) SendMasterHeartbeat(ctx context.Context, req *dfspb.MasterHeartbeatRequest) (*dfspb.MasterHeartbeatResponse, error) {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	s.lastHeartbeat = time.Now()
// 	s.primaryAddr = req.PrimaryAddr

// 	s.master.logger.Printf("Secondary: heartbeat from primary %s (primary wal_seq=%d, my wal_seq=%d)",
// 		req.PrimaryAddr, req.LastWalSequence, s.lastSeqReceived)

// 	return &dfspb.MasterHeartbeatResponse{
// 		Success:              true,
// 		LastReceivedSequence: s.lastSeqReceived,
// 	}, nil
// }

// // ApplyCheckpoint loads a full state snapshot sent by the primary.
// // Called periodically so secondary doesn't need to replay WAL from scratch.
// func (s *SecondaryMaster) ApplyCheckpoint(ctx context.Context, req *dfspb.CheckpointRequest) (*dfspb.CheckpointResponse, error) {
// 	s.master.logger.Printf("Secondary: applying checkpoint at seq %d (%d bytes)",
// 		req.SequenceNumber, len(req.StateData))

// 	var checkpoint Checkpoint
// 	if err := json.Unmarshal(req.StateData, &checkpoint); err != nil {
// 		return &dfspb.CheckpointResponse{Success: false}, fmt.Errorf("bad checkpoint payload: %v", err)
// 	}

// 	s.master.mu.Lock()
// 	defer s.master.mu.Unlock()

// 	// Restore all maps (same logic as LoadCheckpoint)
// 	s.master.clientIDs = checkpoint.ClientIDs
// 	s.master.fileSizes = checkpoint.FileSizes
// 	s.master.chunkStatus = checkpoint.ChunkStatus
// 	s.master.generation = checkpoint.Generation
// 	if checkpoint.ClientFolders != nil {
// 		s.master.clientFolders = checkpoint.ClientFolders
// 	}
// 	if checkpoint.FileUploadTimes != nil {
// 		s.master.fileUploadTimes = checkpoint.FileUploadTimes
// 	}
// 	if checkpoint.ClientUsernames != nil {
// 		s.master.clientUsernames = checkpoint.ClientUsernames
// 	}
// 	s.master.fileInfo = make(map[int64]map[string]map[int32]*dfspb.StripeMetadata)

// 	for clientID, filesJSON := range checkpoint.FileInfo {
// 		s.master.fileInfo[clientID] = make(map[string]map[int32]*dfspb.StripeMetadata)
// 		for filename, stripesJSON := range filesJSON {
// 			s.master.fileInfo[clientID][filename] = make(map[int32]*dfspb.StripeMetadata)
// 			for stripeNum, sj := range stripesJSON {
// 				s.master.fileInfo[clientID][filename][stripeNum] = &dfspb.StripeMetadata{
// 					StripeNum: sj.StripeNum,
// 					ChunkIds:  sj.ChunkIds,
// 					Servers:   sj.Servers,
// 				}
// 			}
// 		}
// 	}

// 	s.master.walSeq = req.SequenceNumber
// 	s.master.logger.Printf("Secondary: checkpoint applied — %d clients, %d chunks",
// 		len(s.master.clientIDs), len(s.master.chunkStatus))

// 	return &dfspb.CheckpointResponse{Success: true}, nil
// }

// // RequestStateSync is called by a returning/restarting master that was previously primary.
// // It returns the full current state (checkpoint bytes + sequence + generation) so the
// // returning node can catch up before deciding whether to resume or stay as secondary.
// func (s *SecondaryMaster) RequestStateSync(ctx context.Context, req *dfspb.GetActiveMasterRequest) (*dfspb.CheckpointRequest, error) {
// 	s.master.logger.Printf("Secondary: RequestStateSync called — serialising full state (gen=%d, wal_seq=%d)",
// 		s.master.generation, s.master.walSeq)

// 	// Produce a fresh checkpoint bytes
// 	s.master.mu.Lock()

// 	// Build fileInfoJSON (same as CreateCheckpoint)
// 	fileInfoJSON := make(map[int64]map[string]map[int32]*StripeMetadataJSON)
// 	for clientID := range s.master.fileInfo {
// 		fileInfoJSON[clientID] = make(map[string]map[int32]*StripeMetadataJSON)
// 		for filename, stripes := range s.master.fileInfo[clientID] {
// 			fileInfoJSON[clientID][filename] = make(map[int32]*StripeMetadataJSON)
// 			for stripeNum, stripe := range stripes {
// 				fileInfoJSON[clientID][filename][stripeNum] = &StripeMetadataJSON{
// 					StripeNum: stripe.StripeNum,
// 					ChunkIds:  stripe.ChunkIds,
// 					Servers:   stripe.Servers,
// 				}
// 			}
// 		}
// 	}

// 	checkpoint := Checkpoint{
// 		Timestamp:       time.Now().Unix(),
// 		Generation:      s.master.generation,
// 		WALSeq:          s.master.walSeq,
// 		FileInfo:        fileInfoJSON,
// 		ClientIDs:       s.master.clientIDs,
// 		FileSizes:       s.master.fileSizes,
// 		ChunkStatus:     s.master.chunkStatus,
// 		ClientFolders:   s.master.clientFolders,
// 		FileUploadTimes: s.master.fileUploadTimes,
// 		ClientUsernames: s.master.clientUsernames,
// 	}
// 	seq := s.master.walSeq
// 	s.master.mu.Unlock()

// 	data, err := json.Marshal(checkpoint)
// 	if err != nil {
// 		return nil, fmt.Errorf("RequestStateSync: failed to marshal state: %v", err)
// 	}

// 	s.master.logger.Printf("Secondary: RequestStateSync response ready (%d bytes, gen=%d, seq=%d)",
// 		len(data), checkpoint.Generation, seq)

// 	return &dfspb.CheckpointRequest{
// 		SequenceNumber: seq,
// 		StateData:      data,
// 	}, nil
// }

// // primaryIsReachable dials the primary with a short timeout and returns true if
// // it responds to a real gRPC call.  Used by the watchdog to distinguish a
// // genuine primary failure from a transient cross-machine packet loss.
// func (s *SecondaryMaster) primaryIsReachable(addr string) bool {
// 	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
// 	defer cancel()

// 	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		return false
// 	}
// 	defer conn.Close()

// 	client := dfspb.NewMasterServerClient(conn)
// 	// Use GetActiveMaster as a lightweight ping — every master implements it.
// 	_, err = client.GetActiveMaster(ctx, &dfspb.GetActiveMasterRequest{})
// 	return err == nil
// }

// // WatchdogLoop runs in a goroutine on the secondary.
// // If no heartbeat arrives from the primary within the timeout it performs a
// // direct liveness probe before promoting.  This prevents false failovers caused
// // by transient cross-machine packet loss (e.g. Mac ↔ Lenovo LAN hiccups).
// func (s *SecondaryMaster) WatchdogLoop(timeoutSeconds int) {
// 	ticker := time.NewTicker(time.Second)
// 	defer ticker.Stop()

// 	s.master.logger.Printf("Secondary watchdog started (timeout=%ds)", timeoutSeconds)

// 	for range ticker.C {
// 		s.mu.Lock()
// 		elapsed := time.Since(s.lastHeartbeat)
// 		primaryAddr := s.primaryAddr
// 		s.mu.Unlock()

// 		if elapsed <= time.Duration(timeoutSeconds)*time.Second {
// 			continue
// 		}

// 		// Heartbeat timeout reached — do a direct probe before committing to failover.
// 		// This catches the "heartbeat packets dropped but primary is still running" case.
// 		s.master.logger.Printf("Watchdog: heartbeat silent for %.1fs — probing primary %s directly...",
// 			elapsed.Seconds(), primaryAddr)

// 		if primaryAddr != "" && s.primaryIsReachable(primaryAddr) {
// 			// Primary answered the probe: it's alive, just the heartbeat stream lagged.
// 			// Reset the timer so we don't re-probe every second.
// 			s.master.logger.Printf("Watchdog: primary %s responded to probe — resetting heartbeat timer (no failover)", primaryAddr)
// 			s.mu.Lock()
// 			s.lastHeartbeat = time.Now()
// 			s.mu.Unlock()
// 			continue
// 		}

// 		s.master.logger.Printf("FAILOVER: primary %s unreachable (heartbeat silent %.1fs, probe failed) — promoting to primary",
// 			primaryAddr, elapsed.Seconds())
// 		s.promote()
// 		return // watchdog exits after promotion
// 	}
// }

// // promote flips this node from standby to active primary.
// // After this call, the node's MasterServer will start accepting full client RPCs
// // and begin sending heartbeats/WAL to the peer in case it comes back online.
// func (s *SecondaryMaster) promote() {
// 	s.master.mu.Lock()
// 	s.master.isPrimary = true
// 	s.master.generation++ // New epoch: every promotion increments generation

// 	// Set secondaryAddr to the peer so WAL replication targets the correct node.
// 	// Use peerAddr (always set from -secondary flag); fall back to the address
// 	// we received in heartbeats from the old primary.
// 	peerAddr := s.master.peerAddr
// 	if peerAddr == "" {
// 		peerAddr = s.primaryAddr
// 	}
// 	s.master.secondaryAddr = peerAddr
// 	s.master.mu.Unlock()

// 	// Persist our current state as a checkpoint so we can recover if WE crash
// 	if err := s.master.CreateCheckpoint("master.checkpoint"); err != nil {
// 		s.master.logger.Printf("FAILOVER: checkpoint on promotion failed: %v", err)
// 	}

// 	// Start sending heartbeats to the peer (old primary) so it can sync when
// 	// it comes back online and remain as standby.
// 	if peerAddr != "" {
// 		go s.master.SendHeartbeatsToSecondary(peerAddr)
// 		s.master.logger.Printf("FAILOVER: started sending heartbeats to peer %s", peerAddr)
// 	}

// 	s.master.logger.Printf("FAILOVER COMPLETE: this node is now the active primary (wal_seq=%d, generation=%d)", s.master.walSeq, s.master.generation)
// 	fmt.Fprintf(os.Stderr, "\n")
// 	fmt.Fprintf(os.Stderr, "╔══════════════════════════════════════════════════════════╗\n")
// 	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
// 	fmt.Fprintf(os.Stderr, "║   🔴  FAILOVER — THIS NODE IS NOW THE ACTIVE PRIMARY  🔴 ║\n")
// 	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
// 	fmt.Fprintf(os.Stderr, "║   Address  : %-42s  ║\n", s.master.myAddr)
// 	fmt.Fprintf(os.Stderr, "║   Generation: %-41d  ║\n", s.master.generation)
// 	fmt.Fprintf(os.Stderr, "║   WAL Seq  : %-42d  ║\n", s.master.walSeq)
// 	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
// 	fmt.Fprintf(os.Stderr, "╚══════════════════════════════════════════════════════════╝\n")
// 	fmt.Fprintf(os.Stderr, "\n")
// }

// package main

// import (
// 	"context"
// 	"dfs-project/dfspb"
// 	"encoding/json"
// 	"fmt"
// 	"os"
// 	"sync"
// 	"time"
// )

// // SecondaryMaster implements the SecondaryMasterServer gRPC service.
// // It receives WAL entries and heartbeats from the primary.
// // When heartbeats stop arriving it promotes itself to primary.
// type SecondaryMaster struct {
// 	dfspb.UnimplementedSecondaryMasterServerServer

// 	mu              sync.Mutex
// 	lastHeartbeat   time.Time
// 	lastSeqReceived uint64
// 	primaryAddr     string

// 	// Pointer to the shared MasterServer so that on promotion we can
// 	// flip isPrimary and start serving client RPCs from the same state.
// 	master *MasterServer
// }

// // NewSecondaryMaster creates a SecondaryMaster watching over the given MasterServer state.
// func NewSecondaryMaster(master *MasterServer) *SecondaryMaster {
// 	return &SecondaryMaster{
// 		master:        master,
// 		lastHeartbeat: time.Now(), // grace period on startup
// 	}
// }

// // ReplicateWAL is called by the primary after every durable WAL write.
// // We decode the payload and replay it into our in-memory state so we stay in sync.
// func (s *SecondaryMaster) ReplicateWAL(ctx context.Context, req *dfspb.ReplicateWALRequest) (*dfspb.ReplicateWALResponse, error) {
// 	entry := req.Entry

// 	// Decode the JSON WAL entry the primary serialised into payload
// 	var walEntry WALEntry
// 	if err := json.Unmarshal(entry.Payload, &walEntry); err != nil {
// 		return nil, fmt.Errorf("failed to decode WAL payload: %v", err)
// 	}

// 	// Replay into master state (same functions used during crash recovery)
// 	s.master.walMu.Lock()
// 	s.master.walSeq = entry.SequenceNumber
// 	s.master.walMu.Unlock()

// 	// Replay uses the existing WAL recovery helpers — no lock needed here
// 	// because replayOperation acquires no locks itself (it only updates maps).
// 	// We take the master's data lock for safety.
// 	s.master.mu.Lock()
// 	err := s.master.replayOperation(&walEntry)
// 	s.master.mu.Unlock()

// 	if err != nil {
// 		s.master.logger.Printf("Secondary: failed to replay WAL seq %d: %v", entry.SequenceNumber, err)
// 		return &dfspb.ReplicateWALResponse{Success: false}, err
// 	}

// 	s.mu.Lock()
// 	s.lastSeqReceived = entry.SequenceNumber
// 	s.mu.Unlock()

// 	s.master.logger.Printf("Secondary: applied WAL seq %d (op type %v)", entry.SequenceNumber, entry.EntryType)

// 	return &dfspb.ReplicateWALResponse{
// 		Success:         true,
// 		LastSequenceAck: entry.SequenceNumber,
// 	}, nil
// }

// // SendMasterHeartbeat is called by the primary every few seconds.
// // We just record the timestamp; the watchdog goroutine checks it.
// func (s *SecondaryMaster) SendMasterHeartbeat(ctx context.Context, req *dfspb.MasterHeartbeatRequest) (*dfspb.MasterHeartbeatResponse, error) {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	s.lastHeartbeat = time.Now()
// 	s.primaryAddr = req.PrimaryAddr

// 	s.master.logger.Printf("Secondary: heartbeat from primary %s (primary wal_seq=%d, my wal_seq=%d)",
// 		req.PrimaryAddr, req.LastWalSequence, s.lastSeqReceived)

// 	return &dfspb.MasterHeartbeatResponse{
// 		Success:              true,
// 		LastReceivedSequence: s.lastSeqReceived,
// 	}, nil
// }

// // ApplyCheckpoint loads a full state snapshot sent by the primary.
// // Called periodically so secondary doesn't need to replay WAL from scratch.
// func (s *SecondaryMaster) ApplyCheckpoint(ctx context.Context, req *dfspb.CheckpointRequest) (*dfspb.CheckpointResponse, error) {
// 	s.master.logger.Printf("Secondary: applying checkpoint at seq %d (%d bytes)",
// 		req.SequenceNumber, len(req.StateData))

// 	var checkpoint Checkpoint
// 	if err := json.Unmarshal(req.StateData, &checkpoint); err != nil {
// 		return &dfspb.CheckpointResponse{Success: false}, fmt.Errorf("bad checkpoint payload: %v", err)
// 	}

// 	s.master.mu.Lock()
// 	defer s.master.mu.Unlock()

// 	// Restore all maps (same logic as LoadCheckpoint)
// 	s.master.clientIDs = checkpoint.ClientIDs
// 	s.master.fileSizes = checkpoint.FileSizes
// 	s.master.chunkStatus = checkpoint.ChunkStatus
// 	s.master.generation = checkpoint.Generation
// 	if checkpoint.ClientFolders != nil {
// 		s.master.clientFolders = checkpoint.ClientFolders
// 	}
// 	if checkpoint.FileUploadTimes != nil {
// 		s.master.fileUploadTimes = checkpoint.FileUploadTimes
// 	}
// 	if checkpoint.ClientUsernames != nil {
// 		s.master.clientUsernames = checkpoint.ClientUsernames
// 	}
// 	s.master.fileInfo = make(map[int64]map[string]map[int32]*dfspb.StripeMetadata)

// 	for clientID, filesJSON := range checkpoint.FileInfo {
// 		s.master.fileInfo[clientID] = make(map[string]map[int32]*dfspb.StripeMetadata)
// 		for filename, stripesJSON := range filesJSON {
// 			s.master.fileInfo[clientID][filename] = make(map[int32]*dfspb.StripeMetadata)
// 			for stripeNum, sj := range stripesJSON {
// 				s.master.fileInfo[clientID][filename][stripeNum] = &dfspb.StripeMetadata{
// 					StripeNum: sj.StripeNum,
// 					ChunkIds:  sj.ChunkIds,
// 					Servers:   sj.Servers,
// 				}
// 			}
// 		}
// 	}

// 	s.master.walSeq = req.SequenceNumber
// 	s.master.logger.Printf("Secondary: checkpoint applied — %d clients, %d chunks",
// 		len(s.master.clientIDs), len(s.master.chunkStatus))

// 	return &dfspb.CheckpointResponse{Success: true}, nil
// }

// // RequestStateSync is called by a returning/restarting master that was previously primary.
// // It returns the full current state (checkpoint bytes + sequence + generation) so the
// // returning node can catch up before deciding whether to resume or stay as secondary.
// func (s *SecondaryMaster) RequestStateSync(ctx context.Context, req *dfspb.GetActiveMasterRequest) (*dfspb.CheckpointRequest, error) {
// 	s.master.logger.Printf("Secondary: RequestStateSync called — serialising full state (gen=%d, wal_seq=%d)",
// 		s.master.generation, s.master.walSeq)

// 	// Produce a fresh checkpoint bytes
// 	s.master.mu.Lock()

// 	// Build fileInfoJSON (same as CreateCheckpoint)
// 	fileInfoJSON := make(map[int64]map[string]map[int32]*StripeMetadataJSON)
// 	for clientID := range s.master.fileInfo {
// 		fileInfoJSON[clientID] = make(map[string]map[int32]*StripeMetadataJSON)
// 		for filename, stripes := range s.master.fileInfo[clientID] {
// 			fileInfoJSON[clientID][filename] = make(map[int32]*StripeMetadataJSON)
// 			for stripeNum, stripe := range stripes {
// 				fileInfoJSON[clientID][filename][stripeNum] = &StripeMetadataJSON{
// 					StripeNum: stripe.StripeNum,
// 					ChunkIds:  stripe.ChunkIds,
// 					Servers:   stripe.Servers,
// 				}
// 			}
// 		}
// 	}

// 	checkpoint := Checkpoint{
// 		Timestamp:       time.Now().Unix(),
// 		Generation:      s.master.generation,
// 		WALSeq:          s.master.walSeq,
// 		FileInfo:        fileInfoJSON,
// 		ClientIDs:       s.master.clientIDs,
// 		FileSizes:       s.master.fileSizes,
// 		ChunkStatus:     s.master.chunkStatus,
// 		ClientFolders:   s.master.clientFolders,
// 		FileUploadTimes: s.master.fileUploadTimes,
// 		ClientUsernames: s.master.clientUsernames,
// 	}
// 	seq := s.master.walSeq
// 	s.master.mu.Unlock()

// 	data, err := json.Marshal(checkpoint)
// 	if err != nil {
// 		return nil, fmt.Errorf("RequestStateSync: failed to marshal state: %v", err)
// 	}

// 	s.master.logger.Printf("Secondary: RequestStateSync response ready (%d bytes, gen=%d, seq=%d)",
// 		len(data), checkpoint.Generation, seq)

// 	return &dfspb.CheckpointRequest{
// 		SequenceNumber: seq,
// 		StateData:      data,
// 	}, nil
// }

// // WatchdogLoop runs in a goroutine on the secondary.
// // If no heartbeat arrives from the primary within the timeout it promotes itself.
// func (s *SecondaryMaster) WatchdogLoop(timeoutSeconds int) {
// 	ticker := time.NewTicker(time.Second)
// 	defer ticker.Stop()

// 	s.master.logger.Printf("Secondary watchdog started (timeout=%ds)", timeoutSeconds)

// 	for range ticker.C {
// 		s.mu.Lock()
// 		elapsed := time.Since(s.lastHeartbeat)
// 		s.mu.Unlock()

// 		if elapsed > time.Duration(timeoutSeconds)*time.Second {
// 			s.master.logger.Printf("FAILOVER: primary heartbeat missing for %.1fs — promoting to primary", elapsed.Seconds())
// 			s.promote()
// 			return // watchdog exits after promotion
// 		}
// 	}
// }

// // promote flips this node from standby to active primary.
// // After this call, the node's MasterServer will start accepting full client RPCs
// // and begin sending heartbeats/WAL to the peer in case it comes back online.
// func (s *SecondaryMaster) promote() {
// 	s.master.mu.Lock()
// 	s.master.isPrimary = true
// 	s.master.generation++ // New epoch: every promotion increments generation

// 	// Set secondaryAddr to the peer so WAL replication targets the correct node.
// 	// Use peerAddr (always set from -secondary flag); fall back to the address
// 	// we received in heartbeats from the old primary.
// 	peerAddr := s.master.peerAddr
// 	if peerAddr == "" {
// 		peerAddr = s.primaryAddr
// 	}
// 	s.master.secondaryAddr = peerAddr
// 	s.master.mu.Unlock()

// 	// Persist our current state as a checkpoint so we can recover if WE crash
// 	if err := s.master.CreateCheckpoint("master.checkpoint"); err != nil {
// 		s.master.logger.Printf("FAILOVER: checkpoint on promotion failed: %v", err)
// 	}

// 	// Start sending heartbeats to the peer (old primary) so it can sync when
// 	// it comes back online and remain as standby.
// 	if peerAddr != "" {
// 		go s.master.SendHeartbeatsToSecondary(peerAddr)
// 		s.master.logger.Printf("FAILOVER: started sending heartbeats to peer %s", peerAddr)
// 	}

// 	s.master.logger.Printf("FAILOVER COMPLETE: this node is now the active primary (wal_seq=%d, generation=%d)", s.master.walSeq, s.master.generation)
// 	fmt.Fprintf(os.Stderr, "\n")
// 	fmt.Fprintf(os.Stderr, "╔══════════════════════════════════════════════════════════╗\n")
// 	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
// 	fmt.Fprintf(os.Stderr, "║   🔴  FAILOVER — THIS NODE IS NOW THE ACTIVE PRIMARY  🔴 ║\n")
// 	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
// 	fmt.Fprintf(os.Stderr, "║   Address  : %-42s  ║\n", s.master.myAddr)
// 	fmt.Fprintf(os.Stderr, "║   Generation: %-41d  ║\n", s.master.generation)
// 	fmt.Fprintf(os.Stderr, "║   WAL Seq  : %-42d  ║\n", s.master.walSeq)
// 	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
// 	fmt.Fprintf(os.Stderr, "╚══════════════════════════════════════════════════════════╝\n")
// 	fmt.Fprintf(os.Stderr, "\n")
// }

package main

import (
	"context"
	"dfs-project/dfspb"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
		lastHeartbeat: time.Now(),
	}
}

// ReplicateWAL is called by the primary after every durable WAL write.
// We decode the payload and replay it into our in-memory state so we stay in sync.
func (s *SecondaryMaster) ReplicateWAL(ctx context.Context, req *dfspb.ReplicateWALRequest) (*dfspb.ReplicateWALResponse, error) {
	entry := req.Entry

	var walEntry WALEntry
	if err := json.Unmarshal(entry.Payload, &walEntry); err != nil {
		return nil, fmt.Errorf("failed to decode WAL payload: %v", err)
	}

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
	s.lastHeartbeat = time.Now()
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

	s.master.mu.Lock()

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

// primaryIsReachable dials the primary with a short timeout and returns true if
// it responds to a real gRPC call.  Used by the watchdog to distinguish a
// genuine primary failure from a transient cross-machine packet loss.
func (s *SecondaryMaster) primaryIsReachable(addr string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false
	}
	defer conn.Close()

	client := dfspb.NewMasterServerClient(conn)
	_, err = client.GetActiveMaster(ctx, &dfspb.GetActiveMasterRequest{}, grpc.WaitForReady(true))
	return err == nil
}

// WatchdogLoop runs in a goroutine on the secondary.
// On reliable host-only networking the timing is:
//   - Heartbeat every 1s from primary
//   - Timeout = 15s = 15 missed beats → likely dead
//   - Confirmation = 3 probes × 1s = 3s window to rule out a momentary hiccup
//   - Total time from primary death to promotion: ~18s worst case
func (s *SecondaryMaster) WatchdogLoop(timeoutSeconds int) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	s.master.logger.Printf("Secondary watchdog started (timeout=%ds, then 3×1s confirmation probes)", timeoutSeconds)

	for range ticker.C {
		s.mu.Lock()
		elapsed := time.Since(s.lastHeartbeat)
		primaryAddr := s.primaryAddr
		s.mu.Unlock()
		if primaryAddr == "" {
			primaryAddr = s.master.peerAddr
		}

		if elapsed <= time.Duration(timeoutSeconds)*time.Second {
			continue
		}

		s.master.logger.Printf("Watchdog: heartbeat silent for %.1fs — running 3 confirmation probes on %s",
			elapsed.Seconds(), primaryAddr)

		alive := false
		for i := 1; i <= 3; i++ {
			if primaryAddr != "" && s.primaryIsReachable(primaryAddr) {
				s.master.logger.Printf("Watchdog: probe %d/3 succeeded — primary %s is alive, resetting timer", i, primaryAddr)
				alive = true
				break
			}
			s.master.logger.Printf("Watchdog: probe %d/3 failed — primary %s not responding", i, primaryAddr)
			if i < 3 {
				time.Sleep(1 * time.Second)
			}
		}

		if alive {
			s.mu.Lock()
			s.lastHeartbeat = time.Now()
			s.mu.Unlock()
			continue
		}

		s.master.logger.Printf("FAILOVER: all 3 probes failed — primary %s is dead (silent for %.1fs) — promoting",
			primaryAddr, elapsed.Seconds())
		s.promote()
		return
	}
}

// promote flips this node from standby to active primary.
// After this call, the node's MasterServer will start accepting full client RPCs
// and begin sending heartbeats/WAL to the peer in case it comes back online.
func (s *SecondaryMaster) promote() {
	s.master.mu.Lock()
	s.master.isPrimary = true
	s.master.generation++

	peerAddr := s.master.peerAddr
	if peerAddr == "" {
		peerAddr = s.primaryAddr
	}
	s.master.secondaryAddr = peerAddr
	s.master.mu.Unlock()

	if err := s.master.CreateCheckpoint("master.checkpoint"); err != nil {
		s.master.logger.Printf("FAILOVER: checkpoint on promotion failed: %v", err)
	}

	if peerAddr != "" {
		go s.master.SendHeartbeatsToSecondary(peerAddr)
		s.master.logger.Printf("FAILOVER: started sending heartbeats to peer %s", peerAddr)
	}

	s.master.logger.Printf("FAILOVER COMPLETE: this node is now the active primary (wal_seq=%d, generation=%d)", s.master.walSeq, s.master.generation)
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "╔══════════════════════════════════════════════════════════╗\n")
	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
	fmt.Fprintf(os.Stderr, "║   🔴  FAILOVER — THIS NODE IS NOW THE ACTIVE PRIMARY  🔴 ║\n")
	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
	fmt.Fprintf(os.Stderr, "║   Address  : %-42s  ║\n", s.master.myAddr)
	fmt.Fprintf(os.Stderr, "║   Generation: %-41d  ║\n", s.master.generation)
	fmt.Fprintf(os.Stderr, "║   WAL Seq  : %-42d  ║\n", s.master.walSeq)
	fmt.Fprintf(os.Stderr, "║                                                          ║\n")
	fmt.Fprintf(os.Stderr, "╚══════════════════════════════════════════════════════════╝\n")
	fmt.Fprintf(os.Stderr, "\n")
}
