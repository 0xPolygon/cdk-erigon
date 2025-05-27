package nats_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbeddedNatsServer tests basic functionality of an embedded NATS server:
// 1. Start an embedded server
// 2. Create a stream
// 3. Publish a message
// 4. Verify the subscriber receives the message
func TestEmbeddedNatsServer(t *testing.T) {
	// Start embedded NATS server
	opts := server.Options{
		Host:      "127.0.0.1",
		Port:      -1, // Use random port
		NoLog:     true,
		NoSigs:    true,
		JetStream: true, // Enable JetStream
	}

	// Create the server with the options
	natsServer, err := server.NewServer(&opts)
	require.NoError(t, err)

	// Start the server
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
