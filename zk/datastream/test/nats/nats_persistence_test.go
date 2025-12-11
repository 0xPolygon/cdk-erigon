package nats_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNatsPersistence verifies that messages are persisted to disk and can be retrieved
// after a server restart
func TestNatsPersistence(t *testing.T) {
	// Create a temporary directory for NATS storage
	storageDir, err := os.MkdirTemp("", "nats-storage")
	require.NoError(t, err)
	defer os.RemoveAll(storageDir)

	// Configure and start the first server instance
	opts := server.Options{
		Host:               "127.0.0.1",
		Port:               -1, // Use random port
		NoLog:              true,
		NoSigs:             true,
		JetStream:          true,
		StoreDir:           storageDir,
		JetStreamMaxMemory: 1024 * 1024,      // 1MB
		JetStreamMaxStore:  1024 * 1024 * 10, // 10MB
	}

	// Start server and publish messages, ignoring the returned URL since we don't need it
	_, err = startServerAndPublishMessages(t, &opts)
	require.NoError(t, err)

	// Restart the server and verify messages are still available
	err = verifyPersistedMessages(t, &opts)
	require.NoError(t, err)
}

// startServerAndPublishMessages starts a NATS server and publishes test messages
func startServerAndPublishMessages(t *testing.T, opts *server.Options) (string, error) {
	// Create and start the server
	natsServer, err := server.NewServer(opts)
	require.NoError(t, err)

	go natsServer.Start()
	defer natsServer.Shutdown()

	// Wait for server to be ready
	if !natsServer.ReadyForConnections(2 * time.Second) {
		t.Fatalf("NATS server failed to start")
	}

	// Connect to the server
	serverURL := natsServer.ClientURL()
	nc, err := nats.Connect(serverURL)
	require.NoError(t, err)
	defer nc.Close()

	// Create JetStream context
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Create a stream
	streamName := "persistence_test"
	ctx := context.Background()

	// Create a stream with file storage
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamName + ".>"},
		Storage:  jetstream.FileStorage,
	})
	require.NoError(t, err)

	// Publish test messages
	for i := 0; i < 10; i++ {
		_, err = js.Publish(ctx, streamName+".test", []byte("persistent message"))
		require.NoError(t, err)
	}

	// Wait for messages to be properly stored
	time.Sleep(500 * time.Millisecond)

	return serverURL, nil
}

// verifyPersistedMessages creates a new server instance using the same storage
// and verifies that the previously published messages are still available
func verifyPersistedMessages(t *testing.T, opts *server.Options) error {
	// Create and start a new server with the same storage
	natsServer, err := server.NewServer(opts)
	require.NoError(t, err)

	go natsServer.Start()
	defer natsServer.Shutdown()

	// Wait for server to be ready
	if !natsServer.ReadyForConnections(2 * time.Second) {
		t.Fatalf("NATS server failed to start")
	}

	// Connect to the server
	serverURL := natsServer.ClientURL()
	nc, err := nats.Connect(serverURL)
	require.NoError(t, err)
	defer nc.Close()

	// Create JetStream context
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Get stream info to verify it exists
	ctx := context.Background()
	streamInfo, err := js.Stream(ctx, "persistence_test")
	require.NoError(t, err)

	// Verify the stream has the expected number of messages
	info, err := streamInfo.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(10), info.State.Msgs, "Stream should have 10 persisted messages")

	// Create a consumer to retrieve messages
	consumer, err := js.CreateOrUpdateConsumer(ctx, "persistence_test", jetstream.ConsumerConfig{
		Durable:       "persistence_consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	require.NoError(t, err)

	// Count received messages
	messageCount := 0
	messageReceived := make(chan struct{}, 10)

	// Subscribe to consume messages
	sub, err := consumer.Consume(func(msg jetstream.Msg) {
		assert.Equal(t, "persistent message", string(msg.Data()))
		err := msg.Ack()
		assert.NoError(t, err)

		messageReceived <- struct{}{}
	})
	require.NoError(t, err)
	defer sub.Stop()

	// Wait for messages to be received
	for i := 0; i < 10; i++ {
		select {
		case <-messageReceived:
			messageCount++
		case <-time.After(5 * time.Second):
			t.Fatalf("Timed out waiting for persisted messages")
		}
	}

	assert.Equal(t, 10, messageCount, "Should receive all persisted messages")

	return nil
}
