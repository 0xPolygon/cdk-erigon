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

// TestNatsServerClientArrangement tests a setup with:
// 1. One embedded NATS server
// 2. A client that connects to this server
// 3. Publishing a message from the server side
// 4. Receiving the message on the client side
func TestNatsServerClientArrangement(t *testing.T) {
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

	// Get the server URL
	serverURL := natsServer.ClientURL()

	// ------ SERVER SIDE ------
	// Connect to the server (server-side connection)
	serverConn, err := nats.Connect(serverURL)
	require.NoError(t, err)
	defer serverConn.Close()

	// Create JetStream context for the server
	serverJS, err := jetstream.New(serverConn)
	require.NoError(t, err)

	// Create a stream on the server
	streamName := "server_client_test"
	ctx := context.Background()

	// Create a stream with default settings
	_, err = serverJS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamName + ".>"},
	})
	require.NoError(t, err)

	// ------ CLIENT SIDE ------
	// Connect to the server (client-side connection)
	clientConn, err := nats.Connect(serverURL)
	require.NoError(t, err)
	defer clientConn.Close()

	// Create JetStream context for the client
	clientJS, err := jetstream.New(clientConn)
	require.NoError(t, err)

	// Message to be received by client
	messageReceived := make(chan bool)

	// Create a consumer on the client side
	consumer, err := clientJS.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable:       "client_consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	require.NoError(t, err)

	// Create subscription on the client side to receive messages
	sub, err := consumer.Consume(func(msg jetstream.Msg) {
		// Process the message
		receivedMsg := string(msg.Data())
		assert.Equal(t, "hello from server", receivedMsg)

		// Acknowledge the message
		err := msg.Ack()
		assert.NoError(t, err)

		// Signal that message has been received
		messageReceived <- true
	})
	require.NoError(t, err)
	defer sub.Stop()

	// Publish a message from the server side
	_, err = serverJS.Publish(ctx, streamName+".test", []byte("hello from server"))
	require.NoError(t, err)

	// Wait for the message to be received by the client
	select {
	case <-messageReceived:
		// Test passed
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for message")
	}
}
