package natsstream

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNATSClient_ReadAllEntriesToChannel_EmptyStreamWithoutMetadata tests that
// ReadAllEntriesToChannel completes quickly on empty streams when KV metadata is missing
func TestNATSClient_ReadAllEntriesToChannel_EmptyStreamWithoutMetadata(t *testing.T) {
	ctx := context.Background()

	// Use setupTestClientWithStream for proper Manager setup
	client, ns, _ := setupTestClientWithStream(t, ctx)
	defer ns.Shutdown()
	defer client.Stop()

	// Reset to test from stopped state
	client.Stop()
	client.started = false

	err := client.Start()
	require.NoError(t, err)

	// Test that ReadAllEntriesToChannel completes quickly (doesn't hang)
	// when stream is empty and no KV metadata exists
	done := make(chan error, 1)
	go func() {
		done <- client.ReadAllEntriesToChannel()
	}()

	// This should complete quickly (within 5 seconds), not hang
	select {
	case err := <-done:
		// Should complete without error with proper Manager setup
		assert.NoError(t, err, "ReadAllEntriesToChannel should complete without error on empty stream")
		// Should not be in reading state after completion
		assert.False(t, client.reading.Load(), "Client should not be in reading state after completion")
	case <-time.After(5 * time.Second):
		t.Fatal("ReadAllEntriesToChannel took too long - should complete quickly on empty stream")
	}
}

// TestNATSClient_ReadAllEntriesToChannel_HandlesMetadataErrors tests that
// ReadAllEntriesToChannel handles GetTotalEntries errors gracefully
func TestNATSClient_ReadAllEntriesToChannel_HandlesMetadataErrors(t *testing.T) {
	ctx := context.Background()

	// Use setupTestClientWithStream for proper Manager setup
	client, ns, _ := setupTestClientWithStream(t, ctx)
	defer ns.Shutdown()
	defer client.Stop()

	// Reset to test from stopped state
	client.Stop()
	client.started = false

	err := client.Start()
	require.NoError(t, err)

	// Verify that GetTotalEntries fails (due to our fallback removal)
	_, err = client.GetTotalEntries()
	assert.Error(t, err, "GetTotalEntries should fail when KV metadata is missing")

	// ReadAllEntriesToChannel should still work despite GetTotalEntries failing
	err = client.ReadAllEntriesToChannel()
	assert.NoError(t, err, "ReadAllEntriesToChannel should handle GetTotalEntries errors gracefully")
}
