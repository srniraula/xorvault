package dfsclient

import (
	"dfs-project/dfspb"
	"fmt"
	"io"
)

// streamFileInStripes reads from io.Reader and emits Stripe objects based on master's stripes map
func (g *GrpcClient) streamFileInStripes(r io.Reader, stripes map[int32]*dfspb.StripeMetadata, stripeChan chan<- Stripe, errChan chan<- error) {
	defer close(stripeChan)
	defer close(errChan)

	stripeNum := int32(1)
	for {
		// read chunk1
		buf1 := make([]byte, CHUNK_SIZE)
		n1, err1 := io.ReadFull(r, buf1)
		if err1 == io.EOF || (err1 == io.ErrUnexpectedEOF && n1 == 0) {
			return
		}
		if err1 != nil && err1 != io.ErrUnexpectedEOF {
			errChan <- fmt.Errorf("error reading chunk1: %v", err1)
			return
		}
		buf1 = buf1[:n1]

		// read chunk2
		buf2 := make([]byte, CHUNK_SIZE)
		n2, err2 := io.ReadFull(r, buf2)
		if err2 != nil && err2 != io.ErrUnexpectedEOF && err2 != io.EOF {
			errChan <- fmt.Errorf("error reading chunk2: %v", err2)
			return
		}
		buf2HasData := n2 > 0
		if !buf2HasData {
			// Truly empty – last stripe has only one data chunk.
			// Don't upload a fake zero chunk; leave ChunkIds[1] empty.
			buf2 = []byte{}
		} else {
			buf2 = buf2[:n2]
		}

		// pad buf1 if it is a short read (partial last chunk)
		if len(buf1) < CHUNK_SIZE {
			buf1 = padChunk(buf1, CHUNK_SIZE)
		}
		// Only pad buf2 when it is a *partial* read (0 < n2 < CHUNK_SIZE).
		// When buf2 is empty we leave it as []byte{} so parity = XOR(buf1, []) = buf1.
		if buf2HasData && len(buf2) < CHUNK_SIZE {
			buf2 = padChunk(buf2, CHUNK_SIZE)
		}

		// parity
		parity := calculateParity(buf1, buf2)

		// lookup stripe metadata
		sm, ok := stripes[stripeNum]
		if !ok {
			errChan <- fmt.Errorf("no allocation for stripe %d", stripeNum)
			return
		}

		// Build the stripe. When buf2 is empty, clear the second slot so the
		// uploader goroutine skips writing a zero-filled chunk to the server.
		data2ChunkID := sm.ChunkIds[1]
		data2Server := sm.Servers[1]
		if !buf2HasData {
			data2ChunkID = ""
			data2Server = ""
		}

		s := Stripe{
			StripeNum:   int(stripeNum),
			DataChunk1:  buf1,
			DataChunk2:  buf2,
			ParityChunk: parity,
			ChunkIDs:    [3]string{sm.ChunkIds[0], data2ChunkID, sm.ChunkIds[2]},
			Servers:     [3]string{sm.Servers[0], data2Server, sm.Servers[2]},
			Checksums:   [3]string{calculateChecksum(buf1), calculateChecksum(buf2), calculateChecksum(parity)},
		}

		stripeChan <- s

		stripeNum++
		if err2 == io.EOF || err2 == io.ErrUnexpectedEOF {
			return
		}
	}
}
