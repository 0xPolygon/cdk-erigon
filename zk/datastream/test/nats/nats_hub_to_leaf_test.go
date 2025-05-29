package nats_test

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNatsHubToLeafDistribution tests a hub and spoke architecture where:
// 1. Create a "hub" NATS server
// 2. Create multiple "leaf" NATS servers that connect to the hub
// 3. Publish messages on the hub
// 4. Verify that messages published on the hub can be received by leaf nodes
func TestNatsHubToLeafDistribution(t *testing.T) {
	// Create temp directories for each server
	tempRoot, err := os.MkdirTemp("", "nats-hub-to-leaf-test")
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

	// Configure and start the hub server
	hubOpts := server.Options{
		ServerName: "hub",
		Host:       "127.0.0.1",
		Port:       hubPort,
		LeafNode: server.LeafNodeOpts{
			Host: "127.0.0.1",
			Port: hubLeafPort,
		},
		NoLog:  true,
		NoSigs: true,
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

	// Configure and start the first leaf server
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

	// Configure and start the second leaf server
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

	// Connect to the first leaf server
	leaf1Conn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", leafPort1))
	require.NoError(t, err)
	defer leaf1Conn.Close()

	// Connect to the second leaf server
	leaf2Conn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", leafPort2))
	require.NoError(t, err)
	defer leaf2Conn.Close()

	// Create a channel to signal when leaf1 receives a message
	leaf1Received := make(chan string)
	leaf2Received := make(chan string)

	// Setup subscribers on both leaf nodes
	_, err = leaf1Conn.Subscribe("hub.broadcast", func(msg *nats.Msg) {
		leaf1Received <- string(msg.Data)
	})
	require.NoError(t, err)

	_, err = leaf2Conn.Subscribe("hub.broadcast", func(msg *nats.Msg) {
		leaf2Received <- string(msg.Data)
	})
	require.NoError(t, err)

	// Flush the subscriptions to ensure they're processed
	err = leaf1Conn.Flush()
	require.NoError(t, err)
	err = leaf2Conn.Flush()
	require.NoError(t, err)

	// Give some time for subscriptions to be established
	time.Sleep(500 * time.Millisecond)

	// Publish a message from the hub
	testMessage := "Hello from hub!"
	err = hubConn.Publish("hub.broadcast", []byte(testMessage))
	require.NoError(t, err)
	err = hubConn.Flush()
	require.NoError(t, err)

	// Wait for both leaf nodes to receive the message
	var leaf1Message, leaf2Message string
	select {
	case leaf1Message = <-leaf1Received:
		// Message received by leaf1
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for leaf1 to receive message")
	}

	select {
	case leaf2Message = <-leaf2Received:
		// Message received by leaf2
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for leaf2 to receive message")
	}

	// Verify both leaves received the correct message
	assert.Equal(t, testMessage, leaf1Message, "Leaf1 should receive the hub's message")
	assert.Equal(t, testMessage, leaf2Message, "Leaf2 should receive the hub's message")

	// Now test request-reply pattern from hub to leaf
	// Subscribe to a request on leaf1
	_, err = leaf1Conn.Subscribe("hub.request", func(msg *nats.Msg) {
		// Reply to the request
		err := msg.Respond([]byte("Reply from leaf1"))
		assert.NoError(t, err)
	})
	require.NoError(t, err)
	err = leaf1Conn.Flush()
	require.NoError(t, err)

	// Give some time for the subscription to be registered and propagated
	time.Sleep(500 * time.Millisecond)

	// Send a request from the hub to leaf1
	reply, err := hubConn.Request("hub.request", []byte("Request from hub"), 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "Reply from leaf1", string(reply.Data), "Hub should receive reply from leaf1")

	// Test sending multiple messages in a row
	const messageCount = 10
	receivedCount1 := 0
	receivedCount2 := 0

	countChan1 := make(chan bool)
	countChan2 := make(chan bool)

	// Set up counting subscribers
	_, err = leaf1Conn.Subscribe("hub.multiple", func(msg *nats.Msg) {
		receivedCount1++
		if receivedCount1 == messageCount {
			countChan1 <- true
		}
	})
	require.NoError(t, err)

	_, err = leaf2Conn.Subscribe("hub.multiple", func(msg *nats.Msg) {
		receivedCount2++
		if receivedCount2 == messageCount {
			countChan2 <- true
		}
	})
	require.NoError(t, err)

	err = leaf1Conn.Flush()
	require.NoError(t, err)
	err = leaf2Conn.Flush()
	require.NoError(t, err)

	// Give some time for subscriptions to be established
	time.Sleep(500 * time.Millisecond)

	// Publish multiple messages from the hub
	for i := 0; i < messageCount; i++ {
		err = hubConn.Publish("hub.multiple", []byte(fmt.Sprintf("Message %d", i)))
		require.NoError(t, err)
	}
	err = hubConn.Flush()
	require.NoError(t, err)

	// Wait for all messages to be received
	select {
	case <-countChan1:
		// All messages received by leaf1
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for leaf1 to receive all messages, got %d of %d",
			receivedCount1, messageCount)
	}

	select {
	case <-countChan2:
		// All messages received by leaf2
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for leaf2 to receive all messages, got %d of %d",
			receivedCount2, messageCount)
	}

	assert.Equal(t, messageCount, receivedCount1, "Leaf1 should receive all messages")
	assert.Equal(t, messageCount, receivedCount2, "Leaf2 should receive all messages")
}
