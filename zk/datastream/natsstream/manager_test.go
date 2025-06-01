package natsstream

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_Lifecycle(t *testing.T) {
	// Create a logger for testing
	logger := log.New()
	logger.SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StderrHandler))

	// Create config with random port
	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.ServerName = "test-server"
	config.HTTPPort = 0 // Disable HTTP monitoring for tests

	// Create a temporary directory for NATS storage
	tempDir, err := os.MkdirTemp("", "nats-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config.StorageDir = tempDir

	// Create manager
	manager := NewManager(config, logger)

	// Test starting the server
	err = manager.Start()
	require.NoError(t, err)
	assert.True(t, manager.IsRunning())

	// Get URL and verify it's available
	url, err := manager.URL()
	require.NoError(t, err)
	assert.NotEmpty(t, url)

	// Test that we can connect to it
	nc, err := manager.Connect()
	require.NoError(t, err)
	defer nc.Close()

	// Test stopping the server
	manager.Stop()
	assert.False(t, manager.IsRunning())

	// Verify URL is no longer available
	_, err = manager.URL()
	assert.Error(t, err)
}

func TestManager_JetStream(t *testing.T) {
	// Create a logger for testing
	logger := log.New()
	logger.SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StderrHandler))

	// Create a temporary directory for NATS storage
	tempDir, err := os.MkdirTemp("", "nats-jetstream")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create config with JetStream enabled
	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.ServerName = "jetstream-server"
	config.HTTPPort = 0 // Disable HTTP monitoring for tests
	config.JetStreamEnabled = true
	config.StorageDir = tempDir

	// Create manager
	manager := NewManager(config, logger)

	// Start the server
	err = manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Connect to the server
	nc, err := manager.Connect()
	require.NoError(t, err)
	defer nc.Close()

	// Create JetStream context
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Create a stream
	streamName := "test_stream"
	ctx := context.Background()

	// Create a stream with default settings
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamName + ".>"},
	})
	require.NoError(t, err)

	// Message to be received by consumer
	messageReceived := make(chan bool)

	// Create a consumer
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "test_consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	require.NoError(t, err)

	// Create subscription to receive messages
	sub, err := consumer.Consume(func(msg jetstream.Msg) {
		// Process the message
		receivedMsg := string(msg.Data())
		assert.Equal(t, "hello world", receivedMsg)

		// Acknowledge the message
		err := msg.Ack()
		assert.NoError(t, err)

		// Signal that message has been received
		messageReceived <- true
	})
	require.NoError(t, err)
	defer sub.Stop()

	// Publish a message
	_, err = js.Publish(ctx, streamName+".test", []byte("hello world"))
	require.NoError(t, err)

	// Wait for the message to be received with timeout
	select {
	case <-messageReceived:
		// Test passed
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for message")
	}
}

func TestManager_Persistence(t *testing.T) {
	// Create a logger for testing
	logger := log.New()
	logger.SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StderrHandler))

	// Create a persistent storage directory
	storageDir := filepath.Join(t.TempDir(), "nats-persistence")

	// First instance: create stream and publish message
	{
		config := DefaultConfig()
		config.Port = -1
		config.ServerName = "persistence-server-1"
		config.HTTPPort = 0 // Disable HTTP monitoring for tests
		config.JetStreamEnabled = true
		config.StorageDir = storageDir

		manager := NewManager(config, logger)
		err := manager.Start()
		require.NoError(t, err)

		nc, err := manager.Connect()
		require.NoError(t, err)

		// Create JetStream context
		js, err := jetstream.New(nc)
		require.NoError(t, err)

		// Create a stream
		ctx := context.Background()
		stream, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "persistent_stream",
			Subjects: []string{"persistent.>"},
		})
		require.NoError(t, err)

		// Publish test message
		_, err = js.Publish(ctx, "persistent.test", []byte("persistent message"))
		require.NoError(t, err)

		// Clean shutdown
		stream.CachedInfo()
		nc.Close()
		manager.Stop()
	}

	// Wait briefly to ensure clean shutdown
	time.Sleep(500 * time.Millisecond)

	// Second instance: retrieve the message from persistent storage
	{
		config := DefaultConfig()
		config.Port = -1
		config.ServerName = "persistence-server-2"
		config.HTTPPort = 0 // Disable HTTP monitoring for tests
		config.JetStreamEnabled = true
		config.StorageDir = storageDir

		manager := NewManager(config, logger)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Stop()

		nc, err := manager.Connect()
		require.NoError(t, err)
		defer nc.Close()

		// Create JetStream context
		js, err := jetstream.New(nc)
		require.NoError(t, err)

		// Ensure the stream exists
		ctx := context.Background()
		stream, err := js.Stream(ctx, "persistent_stream")
		require.NoError(t, err)

		// Create a consumer
		messageReceived := make(chan bool)
		consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
			Durable:       "test_consumer",
			AckPolicy:     jetstream.AckExplicitPolicy,
			DeliverPolicy: jetstream.DeliverAllPolicy,
		})
		require.NoError(t, err)

		// Consume messages
		sub, err := consumer.Consume(func(msg jetstream.Msg) {
			receivedMsg := string(msg.Data())
			assert.Equal(t, "persistent message", receivedMsg)
			msg.Ack()
			messageReceived <- true
		})
		require.NoError(t, err)
		defer sub.Stop()

		// Wait for the message to be received
		select {
		case <-messageReceived:
			// Test passed
		case <-time.After(5 * time.Second):
			t.Fatal("Timed out waiting for persistent message")
		}
	}
}

func TestManager_StartStop(t *testing.T) {
	// Create a logger for testing
	logger := log.New()
	logger.SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StderrHandler))

	// Create config with random port
	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.ServerName = "cycle-test-server"
	config.HTTPPort = 0 // Disable HTTP monitoring for tests

	// Create manager
	manager := NewManager(config, logger)

	// Test multiple start/stop cycles
	for i := 0; i < 3; i++ {
		// Start the server
		err := manager.Start()
		require.NoError(t, err, "Failed to start on cycle %d", i)
		assert.True(t, manager.IsRunning())

		// Try to start again (should fail)
		err = manager.Start()
		assert.Error(t, err, "Expected error when starting twice")

		// Test connection
		nc, err := manager.Connect()
		require.NoError(t, err)

		// Verify connection works
		err = nc.Publish("test.ping", []byte("ping"))
		require.NoError(t, err)

		// Close connection
		nc.Close()

		// Stop the server
		manager.Stop()
		assert.False(t, manager.IsRunning())
	}
}

func TestManager_ChainStreamOperations(t *testing.T) {
	// Create a logger for testing
	logger := log.New()
	logger.SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StderrHandler))

	// Create a temporary directory for NATS storage
	tempDir, err := os.MkdirTemp("", "nats-chain-stream")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create config with JetStream enabled and specific chain ID
	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.ServerName = "chain-stream-server"
	config.HTTPPort = 0 // Disable HTTP monitoring for tests
	config.JetStreamEnabled = true
	config.StorageDir = tempDir
	config.ChainId = 12345 // Set a specific chain ID

	// Create manager
	manager := NewManager(config, logger)

	// Start the server
	err = manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Test 1: InitStreams should create streams with chain ID in the name
	err = manager.InitStreams()
	require.NoError(t, err, "Failed to initialize streams")

	// Connect to verify stream was created correctly
	nc, err := manager.Connect()
	require.NoError(t, err)
	defer nc.Close()

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// The stream name should include the chain ID
	expectedStreamName := fmt.Sprintf("DATASTREAM_%d", config.ChainId)

	// Verify stream exists and has correct configuration
	ctx := context.Background()
	stream, err := js.Stream(ctx, expectedStreamName)
	require.NoError(t, err, "Stream with chain ID was not created")

	// Check stream configuration
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, expectedStreamName, info.Config.Name, "Stream name doesn't match expected format with chain ID")
	assert.Contains(t, info.Config.Subjects, "datastream.>",
		"Stream subjects don't include chain ID pattern")

	// Test 2: GetOrCreateDataStream should return a JetStream instance that can publish
	dataStream, err := manager.GetOrCreateDataStream()
	require.NoError(t, err, "Failed to get data stream")
	assert.NotNil(t, dataStream, "Data stream should not be nil")

	// Test 3: Direct publishing to the stream to verify it works
	messageReceived := make(chan bool)

	// Create a consumer on the stream
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "test_consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	require.NoError(t, err)

	// Create subscription to receive messages
	sub, err := consumer.Consume(func(msg jetstream.Msg) {
		// Just ack and signal receipt
		err := msg.Ack()
		assert.NoError(t, err)
		messageReceived <- true
	})
	require.NoError(t, err)
	defer sub.Stop()

	// Publish a test message directly to the stream with proper subject
	_, err = js.Publish(ctx, fmt.Sprintf("datastream.%d.test", config.ChainId), []byte("test message"))
	require.NoError(t, err, "Failed to directly publish to stream")

	// Wait for the message to be received
	select {
	case <-messageReceived:
		// Test passed
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for message")
	}

	// Test 4: Error handling - Invalid chain ID
	// Create a new manager with zero chain ID
	invalidConfig := DefaultConfig()
	invalidConfig.Port = -1
	invalidConfig.ChainId = 0 // Invalid chain ID
	invalidManager := NewManager(invalidConfig, logger)

	err = invalidManager.Start()
	require.NoError(t, err)
	defer invalidManager.Stop()

	// Attempt to initialize streams, which should fail due to missing chain ID
	err = invalidManager.InitStreams()
	assert.Error(t, err, "InitStreams should fail with zero chain ID")
	assert.Contains(t, err.Error(), "chain ID not set",
		"Error message should indicate chain ID issue")

	// Attempt to get data stream, which should also fail
	_, err = invalidManager.GetOrCreateDataStream()
	assert.Error(t, err, "GetOrCreateDataStream should fail with zero chain ID")
}
