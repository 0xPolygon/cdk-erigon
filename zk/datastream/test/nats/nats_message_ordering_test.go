package nats_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNatsMessageOrdering tests that messages are delivered in the same order they were published
func TestNatsMessageOrdering(t *testing.T) {
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
	streamName := "order_test_stream"
	ctx := context.Background()

	// Create a stream with default settings
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamName + ".>"},
	})
	require.NoError(t, err)

	// Number of messages to send
	const messageCount = 100

	// Received messages
	var receivedMessages []int
	var mu sync.Mutex
	allReceived := make(chan bool)

	// Create a consumer
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "order_test_consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	require.NoError(t, err)

	// Create subscription to receive messages
	sub, err := consumer.Consume(func(msg jetstream.Msg) {
		// Extract message number
		var msgNum int
		_, err := fmt.Sscanf(string(msg.Data()), "message %d", &msgNum)
		assert.NoError(t, err)

		// Add to received messages
		mu.Lock()
		receivedMessages = append(receivedMessages, msgNum)
		// Check if we've received all messages
		if len(receivedMessages) == messageCount {
			allReceived <- true
		}
		mu.Unlock()

		// Acknowledge the message
		err = msg.Ack()
		assert.NoError(t, err)
	})
	require.NoError(t, err)
	defer sub.Stop()

	// Publish messages
	for i := 0; i < messageCount; i++ {
		message := fmt.Sprintf("message %d", i)
		_, err = js.Publish(ctx, streamName+".test", []byte(message))
		require.NoError(t, err)
	}

	// Wait for all messages to be received with timeout
	select {
	case <-allReceived:
		// Continue with assertions
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for all messages")
	}

	// Check that messages were received in order
	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, messageCount, len(receivedMessages), "Should have received all messages")

	// Verify message order
	for i := 0; i < messageCount; i++ {
		assert.Equal(t, i, receivedMessages[i], "Messages should be received in order")
	}
}
