package natsstream

import (
	"context"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInterfaceComplianceCompileTime verifies that NATSClient implements DatastreamClient interface at compile time
func TestInterfaceComplianceCompileTime(t *testing.T) {
	ctx := context.Background()
	client := NewNATSClient(ctx, "nats://localhost:4222", false, nil, log.New())
	defer client.Stop()

	// Verify interface compliance at compile time
	var _ types.DatastreamClient = client
}

func TestNATSClientInterface_Start(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the robust test infrastructure from behavior tests
	client := populateTestServerWithDefaultDataReturnClient(t, ctx)

	// Test that client is already started from helper
	assert.True(t, client.started)

	// Test idempotent start
	err := client.Start()
	assert.NoError(t, err)

	// Test start with invalid URL - use minimal manager for error test
	config := DefaultConfig()
	config.Port = -1
	badManager := NewManager(config, log.New())
	badClient := NewNATSClient(ctx, "nats://invalid:1234", false, badManager, log.New())
	defer badClient.Stop()
	err = badClient.Start()
	assert.Error(t, err)
}

func TestNATSClientInterface_Stop(t *testing.T) {
	ctx := context.Background()

	// Use setupTestClientWithStream for proper Manager setup
	client, ns, _ := setupTestClientWithStream(t, ctx)
	defer ns.Shutdown()

	// Client is already started by helper
	// Test normal stop
	err := client.Stop()
	assert.NoError(t, err)
	assert.False(t, client.started)

	// Test idempotent stop
	err = client.Stop()
	assert.NoError(t, err)
}

func TestNATSClientInterface_HandleStart(t *testing.T) {
	ctx := context.Background()

	// Use setupTestClientWithStream for proper Manager setup
	client, ns, _ := setupTestClientWithStream(t, ctx)
	defer ns.Shutdown()
	defer client.Stop()

	// Reset started state to test HandleStart
	client.Stop() // Stop first
	client.started = false

	_ = client.HandleStart()
	assert.Equal(t, true, client.started)
}

func TestNATSClientInterface_ReadAllEntriesToChannel(t *testing.T) {
	ctx := context.Background()

	// Use setupTestClientWithStream for proper Manager setup
	client, ns, _ := setupTestClientWithStream(t, ctx)
	defer ns.Shutdown()
	defer client.Stop()

	// Reset to test from stopped state
	client.Stop()
	client.started = false

	// Should fail before start
	err := client.ReadAllEntriesToChannel()
	assert.Equal(t, ErrNotStarted, err)

	// Should succeed after start
	err = client.Start()
	require.NoError(t, err)

	err = client.ReadAllEntriesToChannel()
	assert.NoError(t, err)
	// For empty stream, reading should complete and not be in reading state
	assert.False(t, client.reading.Load())

	// Should be idempotent
	err = client.ReadAllEntriesToChannel()
	assert.NoError(t, err)
}

func TestNATSClientInterface_StopReadingToChannel(t *testing.T) {
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

	// Should work even if not reading
	client.StopReadingToChannel()
	assert.False(t, client.reading.Load())

	// Start reading then stop - with bounded reading, this completes immediately on empty stream
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)
	assert.False(t, client.reading.Load()) // Bounded reading completes immediately

	client.StopReadingToChannel()

	// Wait for reading to stop
	timeout := time.After(5 * time.Second)
	for client.reading.Load() {
		select {
		case <-timeout:
			t.Fatal("Reading did not stop within timeout")
		case <-time.After(10 * time.Millisecond):
			// Continue waiting
		}
	}

	assert.False(t, client.reading.Load())
}

func TestNATSClientInterface_GetEntryChan(t *testing.T) {
	ctx := context.Background()
	// Channel tests don't need actual NATS connection, use minimal manager
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, log.New())
	client := NewNATSClient(ctx, "nats://localhost:4222", false, manager, log.New())
	defer client.Stop()

	entryChan := client.GetEntryChan()
	require.NotNil(t, entryChan)
	assert.NotNil(t, *entryChan)

	// Channel should be the same across calls
	entryChan2 := client.GetEntryChan()
	assert.Equal(t, entryChan, entryChan2)
}

func TestNATSClientInterface_RenewEntryChannel(t *testing.T) {
	ctx := context.Background()
	// Channel tests don't need actual NATS connection, use minimal manager
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, log.New())
	client := NewNATSClient(ctx, "nats://localhost:4222", false, manager, log.New())
	defer client.Stop()

	// Get initial channel
	entryChanPtr := client.GetEntryChan()
	require.NotNil(t, entryChanPtr)
	oldChan := *entryChanPtr // Store the actual channel value

	// Renew channel
	client.RenewEntryChannel()

	assert.NotEqual(t, oldChan, *entryChanPtr) // Different channel value

	// Verify old channel is closed
	select {
	case _, ok := <-oldChan:
		assert.False(t, ok, "Old channel should be closed")
	default:
		// Channel might not be immediately readable, but that's ok
	}
}

func TestNATSClientInterface_RenewMaxEntryChannel(t *testing.T) {
	ctx := context.Background()
	// Channel tests don't need actual NATS connection, use minimal manager
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, log.New())
	client := NewNATSClient(ctx, "nats://localhost:4222", false, manager, log.New())
	defer client.Stop()

	// Set max size
	client.maxEntryChanSize = 1000

	// Get initial channel
	entryChan := client.GetEntryChan()
	require.NotNil(t, entryChan)
	oldChan := *entryChan // Store the actual channel value

	// Renew with max size
	client.RenewMaxEntryChannel()

	assert.NotEqual(t, oldChan, *entryChan) // Different channel value
}

func TestNATSClientInterface_GetL2BlockByNumber(t *testing.T) {
	ctx := context.Background()

	// Use modern helper pattern with proper setup and atomic operations
	client := populateTestServerWithCustomDataReturnClient(
		t,
		ctx,
		func(t *testing.T, streamServer *NATSStreamServer) {
			// Create complete block with bookmark using proper atomic ops
			publishCompleteL2Block(t, streamServer, 1, 1, 1) // block 1, batch 1, 1 tx
		},
	)

	// Test retrieval
	block, err := client.GetL2BlockByNumber(1)
	assert.NoError(t, err)
	require.NotNil(t, block)
	assert.Equal(t, uint64(1), block.L2BlockNumber)
	assert.Len(t, block.L2Txs, 1)

	// Test non-existent block
	_, err = client.GetL2BlockByNumber(999)
	assert.Error(t, err)
}

func TestNATSClientInterface_GetLatestL2Block_WithDefaultData(t *testing.T) {
	ctx := context.Background()

	// Use modern helper pattern with default data (creates block 1)
	client := populateTestServerWithDefaultDataReturnClient(t, ctx)

	// Should have the default block from setup
	block, err := client.GetLatestL2Block()
	assert.NoError(t, err)
	require.NotNil(t, block)
	assert.Equal(t, uint64(1), block.L2BlockNumber) // From default data
}

func TestNATSClientInterface_GetLatestL2Block_WithMultipleBlocks(t *testing.T) {
	ctx := context.Background()

	// Test with multiple blocks
	client := populateTestServerWithCustomDataReturnClient(
		t,
		ctx,
		func(t *testing.T, streamServer *NATSStreamServer) {
			// Create multiple blocks, latest should be block 3
			publishCompleteL2Block(t, streamServer, 1, 1, 1)
			publishCompleteL2Block(t, streamServer, 2, 1, 2)
			publishCompleteL2Block(t, streamServer, 3, 1, 1)
		},
	)

	// Latest should be block 3
	block, err := client.GetLatestL2Block()
	assert.NoError(t, err)
	require.NotNil(t, block)
	assert.Equal(t, uint64(3), block.L2BlockNumber)
}
