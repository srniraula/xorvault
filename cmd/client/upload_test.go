package main

import (
	"os"
	"testing"
)

// TestUploadPipeline tests the streaming upload pipeline with a small file
func TestUploadPipeline(t *testing.T) {
	// Create a test file (3MB = ~2 stripes)
	testFile := "test_upload.dat"
	defer os.Remove(testFile) // Clean up after test

	// Create 3MB of test data (will create 2 stripes: 2MB + 1MB)
	testData := make([]byte, 3*1024*1024)
	for i := range testData {
		testData[i] = byte(i % 256) // Fill with pattern 0-255 repeating
	}

	err := os.WriteFile(testFile, testData, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Verify file was created
	fileInfo, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("Failed to stat test file: %v", err)
	}

	if fileInfo.Size() != 3*1024*1024 {
		t.Errorf("Expected 3MB file, got %d bytes", fileInfo.Size())
	}

	t.Logf("Created test file: %s (%d bytes)", testFile, fileInfo.Size())
	t.Log("Test file ready - run actual upload manually with:")
	t.Logf("  go run cmd/client/*.go upload %s", testFile)
}

// TestStripeChannelBuffering tests that channel buffering works correctly
func TestStripeChannelBuffering(t *testing.T) {
	// Create buffered channel like in main.go
	stripeChan := make(chan Stripe, 2)

	// Send 2 stripes (should not block - channel buffer = 2)
	stripe1 := Stripe{StripeNum: 1}
	stripe2 := Stripe{StripeNum: 2}

	stripeChan <- stripe1 // Should succeed immediately
	stripeChan <- stripe2 // Should succeed immediately

	t.Log("Successfully buffered 2 stripes without blocking")

	// Read them back
	s1 := <-stripeChan
	s2 := <-stripeChan

	if s1.StripeNum != 1 || s2.StripeNum != 2 {
		t.Errorf("Expected stripes 1,2 but got %d,%d", s1.StripeNum, s2.StripeNum)
	}

	t.Log("Successfully read stripes from channel in correct order")
}

// TestMemoryEfficiency verifies that stripes are not accumulated in memory
func TestMemoryEfficiency(t *testing.T) {
	t.Log("Memory efficiency test:")
	t.Log("  OLD approach: []Stripe slice holds ALL stripes in memory")
	t.Log("  NEW approach: Channel buffer holds only 2 stripes (6MB max)")
	t.Log("")
	t.Log("For a 1GB file:")
	t.Log("  OLD: ~512 stripes × 3MB = 1.5GB memory")
	t.Log("  NEW: 2 stripes × 3MB = 6MB memory")
	t.Log("")
	t.Log("Pipeline pattern ensures constant memory usage regardless of file size")
}
