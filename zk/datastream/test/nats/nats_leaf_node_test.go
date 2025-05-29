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

// TestNatsLeafNodeSetup tests a setup with a hub and spoke architecture using leaf nodes:
// 1. Create a "hub" NATS server with JetStream enabled
// 2. Create multiple "leaf" NATS servers that connect to the hub
// 3. Verify that JetStream is independently configured on each server
func TestNatsLeafNodeSetup(t *testing.T) {
	// Create temp directories for each server
	tempRoot, err := os.MkdirTemp("", "nats-leaf-node-test")
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
		JetStream: true,
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
		JetStream: true,
		StoreDir:  leaf1Dir,
		NoLog:     true,
		NoSigs:    true,
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
		JetStream: true,
		StoreDir:  leaf2Dir,
		NoLog:     true,
		NoSigs:    true,
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

	// Create JetStream context on the hub
	hubJS, err := jetstream.New(hubConn)
	require.NoError(t, err)

	// Create a stream on the hub server
	hubStreamName := "hub_stream"
	ctx := context.Background()

	_, err = hubJS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     hubStreamName,
		Subjects: []string{hubStreamName + ".>"},
	})
	require.NoError(t, err)

	// Publish messages directly to the hub JetStream
	const messageCount = 5
	for i := 0; i < messageCount; i++ {
		_, err = hubJS.Publish(ctx, hubStreamName+".hub", []byte(fmt.Sprintf("message from hub: %d", i)))
		require.NoError(t, err)
	}

	// Create a JetStream context on leaf1
	leaf1JS, err := jetstream.New(leaf1Conn)
	require.NoError(t, err)

	// Create a local stream on leaf1
	leaf1StreamName := "leaf1_stream"
	_, err = leaf1JS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     leaf1StreamName,
		Subjects: []string{leaf1StreamName + ".>"},
	})
	require.NoError(t, err)

	// Publish messages to the leaf1 local stream
	for i := 0; i < messageCount; i++ {
		_, err = leaf1JS.Publish(ctx, leaf1StreamName+".leaf1", []byte(fmt.Sprintf("message on leaf1: %d", i)))
		require.NoError(t, err)
	}

	// Create a JetStream context on leaf2
	leaf2JS, err := jetstream.New(leaf2Conn)
	require.NoError(t, err)

	// Create a local stream on leaf2
	leaf2StreamName := "leaf2_stream"
	_, err = leaf2JS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     leaf2StreamName,
		Subjects: []string{leaf2StreamName + ".>"},
	})
	require.NoError(t, err)

	// Publish messages to the leaf2 local stream
	for i := 0; i < messageCount; i++ {
		_, err = leaf2JS.Publish(ctx, leaf2StreamName+".leaf2", []byte(fmt.Sprintf("message on leaf2: %d", i)))
		require.NoError(t, err)
	}

	// Verify JetStream domains are separate by confirming we can't access
	// a stream created on the hub from leaf1
	_, err = leaf1JS.Stream(ctx, hubStreamName)
	assert.Error(t, err, "Should not be able to access hub JetStream from leaf1")

	// Verify leaf1 cannot access leaf2's JetStream
	_, err = leaf1JS.Stream(ctx, leaf2StreamName)
	assert.Error(t, err, "Should not be able to access leaf2 JetStream from leaf1")

	// Verify each node can access its own streams
	leaf1Stream, err := leaf1JS.Stream(ctx, leaf1StreamName)
	require.NoError(t, err, "Leaf1 should be able to access its own stream")

	leaf1Info, err := leaf1Stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount), leaf1Info.State.Msgs, "Leaf1 stream should have all messages")

	leaf2Stream, err := leaf2JS.Stream(ctx, leaf2StreamName)
	require.NoError(t, err, "Leaf2 should be able to access its own stream")

	leaf2Info, err := leaf2Stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount), leaf2Info.State.Msgs, "Leaf2 stream should have all messages")

	hubStream, err := hubJS.Stream(ctx, hubStreamName)
	require.NoError(t, err, "Hub should be able to access its own stream")

	hubInfo, err := hubStream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount), hubInfo.State.Msgs, "Hub stream should have all messages")
}
