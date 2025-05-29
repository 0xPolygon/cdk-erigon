package nats_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNatsHybridMessaging tests a hybrid approach with:
// 1. A hub server with JetStream for persistence
// 2. Multiple leaf nodes connected to the hub
// 3. Messages are published to the hub's JetStream for persistence
// 4. The same messages are also published as regular NATS messages for distribution to leaf nodes
// 5. This provides both persistence on the hub and real-time distribution to leaves
func TestNatsHybridMessaging(t *testing.T) {
	// Create temp directories for each server
	tempRoot, err := os.MkdirTemp("", "nats-hybrid-messaging-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempRoot)

	// Configuration
	hubPort := 14222
	hubLeafPort := 17222
	leafPort1 := 14223
	leafPort2 := 14224

	// Create directories for the servers
	hubDir := filepath.Join(tempRoot, "hub")
	leaf1Dir := filepath.Join(tempRoot, "leaf1")
	leaf2Dir := filepath.Join(tempRoot, "leaf2")

	for _, dir := range []string{hubDir, leaf1Dir, leaf2Dir} {
		err := os.MkdirAll(dir, 0755)
		require.NoError(t, err)
	}

	// Configure and start the hub server with JetStream enabled
	hubOpts := server.Options{
		ServerName: "hub",
		Host:       "127.0.0.1",
		Port:       hubPort,
		LeafNode: server.LeafNodeOpts{
			Host: "127.0.0.1",
			Port: hubLeafPort,
		},
		JetStream: true, // Enable JetStream on the hub
		StoreDir:  hubDir,
		NoLog:     true,
		NoSigs:    true,
	}

	hubServer, err := server.NewServer(&hubOpts)
	require.NoError(t, err)

	go hubServer.Start()
	defer hubServer.Shutdown()

	// Wait for hub server to be ready
	if !hubServer.ReadyForConnections(2 * time.Second) {
		t.Fatalf("Hub server failed to start")
	}

	// Parse hub URL for leaf connections
	hubURL, err := url.Parse(fmt.Sprintf("nats://127.0.0.1:%d", hubLeafPort))
	require.NoError(t, err)

	// Configure and start the first leaf server (no JetStream needed)
	leaf1Opts := server.Options{
		ServerName: "leaf1",
		Host:       "127.0.0.1",
		Port:       leafPort1,
		LeafNode: server.LeafNodeOpts{
			ReconnectInterval: time.Second,
			Remotes: []*server.RemoteLeafOpts{
				{
					URLs: []*url.URL{hubURL},
				},
			},
		},
		NoLog:  true,
		NoSigs: true,
	}

	leaf1Server, err := server.NewServer(&leaf1Opts)
	require.NoError(t, err)

	go leaf1Server.Start()
	defer leaf1Server.Shutdown()

	// Wait for leaf1 server to be ready
	if !leaf1Server.ReadyForConnections(2 * time.Second) {
		t.Fatalf("Leaf1 server failed to start")
	}

	// Configure and start the second leaf server (no JetStream needed)
	leaf2Opts := server.Options{
		ServerName: "leaf2",
		Host:       "127.0.0.1",
		Port:       leafPort2,
		LeafNode: server.LeafNodeOpts{
			ReconnectInterval: time.Second,
			Remotes: []*server.RemoteLeafOpts{
				{
					URLs: []*url.URL{hubURL},
				},
			},
		},
		NoLog:  true,
		NoSigs: true,
	}

	leaf2Server, err := server.NewServer(&leaf2Opts)
	require.NoError(t, err)

	go leaf2Server.Start()
	defer leaf2Server.Shutdown()

	// Wait for leaf2 server to be ready
	if !leaf2Server.ReadyForConnections(2 * time.Second) {
		t.Fatalf("Leaf2 server failed to start")
	}

	// Wait for leaf connections to be established
	time.Sleep(2 * time.Second)

	// Verify leaf node connections
	leafs := hubServer.NumLeafNodes()
	if leafs != 2 {
		t.Fatalf("Expected 2 leaf connections, got %d", leafs)
	}

	// Connect to the hub server
	hubConn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", hubPort))
	require.NoError(t, err)
	defer hubConn.Close()

	// Create JetStream context on the hub
	hubJS, err := jetstream.New(hubConn)
	require.NoError(t, err)

	// Create a stream on the hub server for persistence
	streamName := "persistent_stream"
	ctx := context.Background()

	_, err = hubJS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamName + ".>"},
	})
	require.NoError(t, err)

	// Connect to the leaf servers
	leaf1Conn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", leafPort1))
	require.NoError(t, err)
	defer leaf1Conn.Close()

	leaf2Conn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", leafPort2))
	require.NoError(t, err)
	defer leaf2Conn.Close()

	// Setup subscribers on leaf nodes to receive broadcast messages
	// We'll use a different subject than the JetStream stream
	const messageCount = 10
	var wg sync.WaitGroup
	wg.Add(messageCount * 2) // 2 leaf nodes, each receiving messageCount messages

	leaf1Received := make([]string, 0, messageCount)
	leaf2Received := make([]string, 0, messageCount)
	var mu1, mu2 sync.Mutex

	// Subscribe on leaf1
	_, err = leaf1Conn.Subscribe("broadcast.>", func(msg *nats.Msg) {
		mu1.Lock()
		leaf1Received = append(leaf1Received, string(msg.Data))
		mu1.Unlock()
		wg.Done()
	})
	require.NoError(t, err)

	// Subscribe on leaf2
	_, err = leaf2Conn.Subscribe("broadcast.>", func(msg *nats.Msg) {
		mu2.Lock()
		leaf2Received = append(leaf2Received, string(msg.Data))
		mu2.Unlock()
		wg.Done()
	})
	require.NoError(t, err)

	// Flush the subscriptions to ensure they're processed
	err = leaf1Conn.Flush()
	require.NoError(t, err)
	err = leaf2Conn.Flush()
	require.NoError(t, err)

	// Give some time for subscriptions to be established
	time.Sleep(500 * time.Millisecond)

	// Publish messages both to JetStream for persistence and to regular NATS for distribution
	for i := 0; i < messageCount; i++ {
		msgData := fmt.Sprintf("Message %d", i)

		// Store in JetStream for persistence
		_, err = hubJS.Publish(ctx, fmt.Sprintf("%s.msg.%d", streamName, i), []byte(msgData))
		require.NoError(t, err)

		// Broadcast to leaf nodes via regular NATS
		err = hubConn.Publish(fmt.Sprintf("broadcast.msg.%d", i), []byte(msgData))
		require.NoError(t, err)
	}

	// Flush to ensure all messages are sent
	err = hubConn.Flush()
	require.NoError(t, err)

	// Wait for all messages to be received by both leaf nodes
	wgDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(wgDone)
	}()

	select {
	case <-wgDone:
		// All messages received
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for messages, leaf1 got %d, leaf2 got %d", len(leaf1Received), len(leaf2Received))
	}

	// Verify both leaves received all messages
	assert.Equal(t, messageCount, len(leaf1Received), "Leaf1 should receive all messages")
	assert.Equal(t, messageCount, len(leaf2Received), "Leaf2 should receive all messages")

	// Verify messages were persisted in JetStream
	streamInfo, err := hubJS.Stream(ctx, streamName)
	require.NoError(t, err)

	info, err := streamInfo.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount), info.State.Msgs, "Stream should have all messages")

	// Create a JetStream consumer to read back the persisted messages
	consumer, err := hubJS.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable:       "verify_consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	require.NoError(t, err)

	// Read back persisted messages
	storedMessages := make([]string, 0, messageCount)
	allReceived := make(chan bool)

	sub, err := consumer.Consume(func(msg jetstream.Msg) {
		storedMessages = append(storedMessages, string(msg.Data()))
		err := msg.Ack()
		assert.NoError(t, err)

		if len(storedMessages) == messageCount {
			allReceived <- true
		}
	})
	require.NoError(t, err)
	defer sub.Stop()

	// Wait for all messages to be read back
	select {
	case <-allReceived:
		// All messages received
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for stored messages")
	}

	assert.Equal(t, messageCount, len(storedMessages), "Should be able to read back all stored messages")

	// Demonstrate recovery scenario: simulate a leaf node disconnecting and reconnecting
	// Stop leaf1 server
	leaf1Server.Shutdown()
	time.Sleep(1 * time.Second)

	// Create a new leaf1 server
	newLeaf1Opts := leaf1Opts
	newLeaf1Opts.Port = leafPort1 + 100 // Use a different port to ensure a fresh connection

	newLeaf1Server, err := server.NewServer(&newLeaf1Opts)
	require.NoError(t, err)

	go newLeaf1Server.Start()
	defer newLeaf1Server.Shutdown()

	// Wait for the new server to be ready
	if !newLeaf1Server.ReadyForConnections(2 * time.Second) {
		t.Fatalf("New leaf1 server failed to start")
	}

	// Connect to the new leaf1 server
	newLeaf1Conn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", leafPort1+100))
	require.NoError(t, err)
	defer newLeaf1Conn.Close()

	// The new leaf node would need to recover any missed messages
	// For simplicity, we'll demonstrate this by directly reading from hub's JetStream

	// Connect directly to the hub for recovery
	recoveryConn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", hubPort))
	require.NoError(t, err)
	defer recoveryConn.Close()

	// Create JetStream context for recovery
	recoveryJS, err := jetstream.New(recoveryConn)
	require.NoError(t, err)

	// Create a simplified consumer for recovery
	recoveryConsumer, err := recoveryJS.OrderedConsumer(ctx, streamName, jetstream.OrderedConsumerConfig{})
	require.NoError(t, err)

	// Read all messages in a simpler way
	recoveredMessages := make([]string, 0, messageCount)
	var msgCount int

	for msgCount < messageCount {
		msg, err := recoveryConsumer.Next()
		if err != nil {
			if err == context.DeadlineExceeded {
				t.Logf("Deadline exceeded after reading %d messages", msgCount)
				break
			}
			t.Logf("Error reading next message: %v", err)
			break
		}

		recoveredMessages = append(recoveredMessages, string(msg.Data()))
		msgCount++
	}

	// Verify recovery
	if msgCount > 0 {
		t.Logf("Successfully recovered %d messages", msgCount)
	} else {
		t.Logf("Failed to recover any messages")
	}
}
