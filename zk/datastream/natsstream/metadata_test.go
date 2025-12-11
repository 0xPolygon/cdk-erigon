package natsstream

import (
	"context"
	"testing"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrintDump tests the PrintDump function
func TestPrintDump(t *testing.T) {
	logger := log.New()
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams first
	ctx := context.Background()
	err = manager.InitStreams(ctx)
	require.NoError(t, err)

	// Create metadata instance
	metadata, err := NewMetadataManager(ctx, manager, logger)
	require.NoError(t, err)

	// Add some test data
	testBookmark := []byte("test_bookmark")
	err = metadata.AddBookmark(ctx, testBookmark, 100)
	require.NoError(t, err)

	// Test PrintDump
	err = metadata.PrintDump(ctx)
	assert.NoError(t, err)
}

// TestPrintDump_InitializationFailure tests PrintDump when initialization fails
func TestPrintDump_InitializationFailure(t *testing.T) {
	logger := log.New()

	// Create metadata instance but don't start manager (should cause init failure)
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, logger)
	// Don't start the manager

	// Test PrintDump with uninitialized manager - should fail at creation
	ctx := context.Background()
	_, err := NewMetadataManager(ctx, manager, logger)
	// This should fail because manager is not started
	assert.Error(t, err)
}

// TestGetLatestBlockBookmark tests the GetLatestBlockBookmark function
func TestGetLatestBlockBookmark(t *testing.T) {
	logger := log.New()
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams first
	ctx := context.Background()
	err = manager.InitStreams(ctx)
	require.NoError(t, err)

	// Create metadata instance
	metadata, err := NewMetadataManager(ctx, manager, logger)
	require.NoError(t, err)

	// Test case: Set and get bookmark (focusing on success path)
	testBookmark := []byte("latest_block_bookmark_data")
	err = metadata.SetLatestBlockBookmark(ctx, testBookmark)
	require.NoError(t, err)

	// Retrieve the bookmark
	retrievedBookmark, err := metadata.GetLatestBlockBookmark(ctx)
	require.NoError(t, err)
	assert.Equal(t, testBookmark, retrievedBookmark)
}

// TestGetLatestBlockBookmark_NotFound tests the GetLatestBlockBookmark function when no bookmark exists
func TestGetLatestBlockBookmark_NotFound(t *testing.T) {
	logger := log.New()
	config := DefaultConfig()
	config.Port = -1
	// Use a different storage directory to isolate from other tests
	config.StorageDir = t.TempDir()
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	ctx := context.Background()
	err = manager.InitStreams(ctx)
	require.NoError(t, err)

	// Create metadata instance
	metadata, err := NewMetadataManager(ctx, manager, logger)
	require.NoError(t, err)

	// Test case: No bookmark exists (should return error)
	bookmark, err := metadata.GetLatestBlockBookmark(ctx)
	assert.Error(t, err)
	assert.Nil(t, bookmark)
	assert.Contains(t, err.Error(), "not found")
}

// TestGetLatestBlockBookmark_InitializationFailure tests GetLatestBlockBookmark when initialization fails
func TestGetLatestBlockBookmark_InitializationFailure(t *testing.T) {
	logger := log.New()

	// Create metadata instance but don't start manager (should cause init failure)
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, logger)
	// Don't start the manager

	// Test GetLatestBlockBookmark with uninitialized manager - should fail at creation
	ctx := context.Background()
	_, err := NewMetadataManager(ctx, manager, logger)
	// This should fail because manager is not started
	assert.Error(t, err)
}
