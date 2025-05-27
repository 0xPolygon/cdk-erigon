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

// TestNatsBidirectionalCommunication tests bidirectional communication:
// 1. A NATS server and a client both connected to it
// 2. Each side creates a stream for the other to publish to
// 3. Both sides publish and receive messages
// 4. Demonstrates full bidirectional communication
func TestNatsBidirectionalCommunication(t *testing.T) {
	// Start embedded NATS server
	opts := server.Options{
		Host:      "127.0.0.1",
		Port:      -1, // Use random port
		NoLog:     true,
		NoSigs:    true,
		JetStream: true, // Enable JetStream
	}

	// Create and start the server
	natsServer, err := server.NewServer(&opts)
	require.NoError(t, err)

	go natsServer.Start()
	defer natsServer.Shutdown()

	// Wait for server to be ready
	if !natsServer.ReadyForConnections(2 * time.Second) {
		t.Fatalf("NATS server failed to start")
	}

	// Get the server URL
	serverURL := natsServer.ClientURL()
	ctx := context.Background()

	// ------ SERVER SIDE SETUP ------
	serverConn, err := nats.Connect(serverURL)
	require.NoError(t, err)
	defer serverConn.Close()

	serverJS, err := jetstream.New(serverConn)
	require.NoError(t, err)

	// Server creates a stream for client->server communication
	serverStreamName := "client_to_server"
	_, err = serverJS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     serverStreamName,
		Subjects: []string{serverStreamName + ".>"},
	})
	require.NoError(t, err)

	// ------ CLIENT SIDE SETUP ------
	clientConn, err := nats.Connect(serverURL)
	require.NoError(t, err)
	defer clientConn.Close()

	clientJS, err := jetstream.New(clientConn)
	require.NoError(t, err)

	// Client creates a stream for server->client communication
	clientStreamName := "server_to_client"
	_, err = clientJS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     clientStreamName,
		Subjects: []string{clientStreamName + ".>"},
	})
	require.NoError(t, err)

	// ------ SETUP COMMUNICATION CHANNELS ------
	// Channels to signal message receipt
	serverReceivedMsg := make(chan bool)
	clientReceivedMsg := make(chan bool)

	// ------ SERVER SUBSCRIBES TO CLIENT MESSAGES ------
	serverConsumer, err := serverJS.CreateOrUpdateConsumer(ctx, serverStreamName, jetstream.ConsumerConfig{
		Durable:       "server_consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	require.NoError(t, err)

	serverSub, err := serverConsumer.Consume(func(msg jetstream.Msg) {
		// Process message from client
		receivedMsg := string(msg.Data())
		assert.Equal(t, "hello from client", receivedMsg)

		err := msg.Ack()
		assert.NoError(t, err)

		// Signal receipt
		serverReceivedMsg <- true

		// After receiving client message, server responds back
		_, err = serverJS.Publish(ctx, clientStreamName+".response", []byte("hello from server"))
		assert.NoError(t, err)
	})
	require.NoError(t, err)
	defer serverSub.Stop()

	// ------ CLIENT SUBSCRIBES TO SERVER MESSAGES ------
	clientConsumer, err := clientJS.CreateOrUpdateConsumer(ctx, clientStreamName, jetstream.ConsumerConfig{
		Durable:       "client_consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	require.NoError(t, err)

	clientSub, err := clientConsumer.Consume(func(msg jetstream.Msg) {
		// Process message from server
		receivedMsg := string(msg.Data())
		assert.Equal(t, "hello from server", receivedMsg)

		err := msg.Ack()
		assert.NoError(t, err)

		// Signal receipt
		clientReceivedMsg <- true
	})
	require.NoError(t, err)
	defer clientSub.Stop()

	// ------ START COMMUNICATION ------
	// Client initiates the conversation
	_, err = clientJS.Publish(ctx, serverStreamName+".request", []byte("hello from client"))
	require.NoError(t, err)

	// ------ VERIFY BIDIRECTIONAL COMMUNICATION ------
	// Wait for server to receive message from client
	select {
	case <-serverReceivedMsg:
		// Server received message from client
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for server to receive client message")
	}

	// Wait for client to receive server's response
	select {
	case <-clientReceivedMsg:
		// Client received message from server
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for client to receive server message")
	}
}
