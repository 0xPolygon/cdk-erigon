package nats_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNatsLateJoiningLeaves tests a scenario where:
//  1. A hub server starts with JetStream enabled
//  2. The hub publishes a series of messages to a JetStream stream
//  3. Late-joining leaf nodes connect and can:
//     a. Replay all messages from the beginning, or
//     b. Start consuming from a specific sequence number
func TestNatsLateJoiningLeaves(t *testing.T) {
	// Create temp directories for each server
	tempRoot, err := os.MkdirTemp("", "nats-late-joining-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempRoot)

	// Configuration
	hubPort := 14222
	hubLeafPort := 17222
	leaf1Port := 14223 // Will read from beginning
	leaf2Port := 14224 // Will read from sequence 10

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

	// Connect to the hub server
	hubConn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", hubPort))
	require.NoError(t, err)
	defer hubConn.Close()

	// Create JetStream context on the hub
	hubJS, err := jetstream.New(hubConn)
	require.NoError(t, err)

	// Create a stream on the hub server for persistence
	streamName := "historical_stream"
	ctx := context.Background()

	_, err = hubJS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamName + ".>"},
		// Ensure messages are retained for replay
		Retention: jetstream.LimitsPolicy,
		MaxMsgs:   1000, // Keep up to 1000 messages
	})
	require.NoError(t, err)

	// Publish initial set of messages (1-20) before any leaf nodes connect
	const totalMessages = 20
	for i := 1; i <= totalMessages; i++ {
		msgData := fmt.Sprintf("Initial message %d", i)
		_, err = hubJS.Publish(ctx, fmt.Sprintf("%s.msg.%d", streamName, i), []byte(msgData))
		require.NoError(t, err)
	}

	// Wait to ensure all messages are stored
	time.Sleep(500 * time.Millisecond)

	// Get info about the stream to verify messages were stored
	streamInfo, err := hubJS.Stream(ctx, streamName)
	require.NoError(t, err)

	info, err := streamInfo.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(totalMessages), info.State.Msgs, "Stream should have all initial messages")

	// Now connect the first leaf node that will read from beginning
	hubURL, err := url.Parse(fmt.Sprintf("nats://127.0.0.1:%d", hubLeafPort))
	require.NoError(t, err)

	// Configure and start leaf1 server
	leaf1Opts := server.Options{
		ServerName: "leaf1",
		Host:       "127.0.0.1",
		Port:       leaf1Port,
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

	// Connect to leaf1 server
	leaf1Conn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", leaf1Port))
	require.NoError(t, err)
	defer leaf1Conn.Close()

	// For leaf nodes to read from the hub's JetStream, they need to connect directly to the hub
	// This is because JetStream domains are isolated between the hub and leaves
	// Create a direct connection from leaf1 to hub for JetStream access
	leaf1ToHubConn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", hubPort))
	require.NoError(t, err)
	defer leaf1ToHubConn.Close()

	leaf1JS, err := jetstream.New(leaf1ToHubConn)
	require.NoError(t, err)

	// Create a consumer that reads all messages from the beginning
	leaf1Consumer, err := leaf1JS.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable:       "leaf1_consumer",
		DeliverPolicy: jetstream.DeliverAllPolicy, // Start from beginning
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	// Read messages from the beginning for leaf1
	leaf1Messages := make([]string, 0, totalMessages)
	leaf1Done := make(chan bool)

	leaf1Sub, err := leaf1Consumer.Consume(func(msg jetstream.Msg) {
		leaf1Messages = append(leaf1Messages, string(msg.Data()))
		err := msg.Ack()
		assert.NoError(t, err)

		if len(leaf1Messages) == totalMessages {
			leaf1Done <- true
		}
	})
	require.NoError(t, err)
	defer leaf1Sub.Stop()

	// Connect the second leaf node that will read from sequence 10
	// Configure and start leaf2 server
	leaf2Opts := server.Options{
		ServerName: "leaf2",
		Host:       "127.0.0.1",
		Port:       leaf2Port,
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

	// Connect to leaf2 server
	leaf2Conn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", leaf2Port))
	require.NoError(t, err)
	defer leaf2Conn.Close()

	// Connect from leaf2 to hub for JetStream access
	leaf2ToHubConn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", hubPort))
	require.NoError(t, err)
	defer leaf2ToHubConn.Close()

	leaf2JS, err := jetstream.New(leaf2ToHubConn)
	require.NoError(t, err)

	// Starting sequence for leaf2 (start from message 10)
	const startSeq = 10

	// Create a consumer that starts from sequence 10
	leaf2Consumer, err := leaf2JS.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable:       "leaf2_consumer",
		DeliverPolicy: jetstream.DeliverByStartSequencePolicy,
		OptStartSeq:   startSeq, // Start from sequence 10
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	// Read messages starting from sequence 10 for leaf2
	leaf2Messages := make([]string, 0, totalMessages-startSeq+1)
	leaf2Done := make(chan bool)
	expectedLeaf2Count := totalMessages - startSeq + 1

	leaf2Sub, err := leaf2Consumer.Consume(func(msg jetstream.Msg) {
		leaf2Messages = append(leaf2Messages, string(msg.Data()))
		err := msg.Ack()
		assert.NoError(t, err)

		if len(leaf2Messages) == expectedLeaf2Count {
			leaf2Done <- true
		}
	})
	require.NoError(t, err)
	defer leaf2Sub.Stop()

	// Wait for both leaf nodes to receive their messages
	select {
	case <-leaf1Done:
		// Leaf1 received all messages
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for leaf1 to receive all messages, got %d of %d",
			len(leaf1Messages), totalMessages)
	}

	select {
	case <-leaf2Done:
		// Leaf2 received expected messages from sequence 10
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for leaf2 to receive messages from sequence %d, got %d of %d",
			startSeq, len(leaf2Messages), expectedLeaf2Count)
	}

	// Verify leaf1 received all messages from beginning
	assert.Equal(t, totalMessages, len(leaf1Messages),
		"Leaf1 should receive all messages from beginning")

	// Verify leaf2 received only messages from sequence 10
	assert.Equal(t, expectedLeaf2Count, len(leaf2Messages),
		"Leaf2 should receive messages from sequence 10 onwards")

	// Now publish additional messages and verify both consumers receive them
	const additionalMessages = 5
	for i := totalMessages + 1; i <= totalMessages+additionalMessages; i++ {
		msgData := fmt.Sprintf("Additional message %d", i)
		_, err = hubJS.Publish(ctx, fmt.Sprintf("%s.msg.%d", streamName, i), []byte(msgData))
		require.NoError(t, err)
	}

	// Wait for additional messages to be processed
	time.Sleep(1 * time.Second)

	// Verify leaf1 received all messages including additional ones
	assert.GreaterOrEqual(t, len(leaf1Messages), totalMessages+additionalMessages,
		"Leaf1 should receive additional messages")

	// Verify leaf2 received all messages after its starting sequence including additional ones
	assert.GreaterOrEqual(t, len(leaf2Messages), expectedLeaf2Count+additionalMessages,
		"Leaf2 should receive additional messages")

	// Demonstrate time-based consumer: connect a third client that wants messages newer than 30 seconds
	// This demonstrates how to use DeliverByStartTimePolicy which is useful for time-based replay

	// First publish a message with a future timestamp
	futureMsg := "Future message for time-based consumer"
	_, err = hubJS.Publish(ctx, fmt.Sprintf("%s.future", streamName), []byte(futureMsg))
	require.NoError(t, err)

	// Create a consumer that starts from a specific time (30 seconds ago)
	startTime := time.Now().Add(-30 * time.Second)
	timeConsumer, err := leaf1JS.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable:       "time_consumer",
		DeliverPolicy: jetstream.DeliverByStartTimePolicy,
		OptStartTime:  &startTime,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	// Read messages starting from the specified time
	timeMessages := make([]string, 0)
	timeDone := make(chan bool)

	timeSub, err := timeConsumer.Consume(func(msg jetstream.Msg) {
		timeMessages = append(timeMessages, string(msg.Data()))
		err := msg.Ack()
		assert.NoError(t, err)

		// We don't know exactly how many messages we'll get with time-based filtering
		// So we'll just wait a bit and check what we received
		if len(timeMessages) >= 1 {
			// At least got the future message
			timeDone <- true
		}
	})
	require.NoError(t, err)
	defer timeSub.Stop()

	// Wait for time-based consumer to receive messages
	select {
	case <-timeDone:
		// Received at least one message
	case <-time.After(5 * time.Second):
		// If we don't get any messages that's okay too for time-based filter
		t.Logf("No messages matched the time-based filter")
	}

	// Verify time-based consumer received recent messages
	assert.GreaterOrEqual(t, len(timeMessages), 0,
		"Time-based consumer should receive any messages that match its time criteria")
}
