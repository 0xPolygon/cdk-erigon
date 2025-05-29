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

// TestNatsAutoReplication tests automatic JetStream replication using:
// 1. A proper NATS cluster with multiple servers
// 2. JetStream enabled on all servers in the cluster
// 3. Stream configuration with replication factor > 1
// 4. Automatic replication of data across cluster nodes
func TestNatsAutoReplication(t *testing.T) {
	// Create temp directories for each server
	tempRoot, err := os.MkdirTemp("", "nats-auto-replication-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempRoot)

	// Configure cluster with 3 nodes
	clusterName := "auto_replication_cluster"
	numServers := 3
	serverDirs := make([]string, numServers)
	serverPorts := make([]int, numServers)
	clusterPorts := make([]int, numServers)

	// Use random ports starting from these bases
	basePort := 14222
	baseClusterPort := 16222

	// Setup routes for clustering - each server needs to know about the others
	var routeURLs []string
	for i := 0; i < numServers; i++ {
		// Create directory for this server
		serverDirs[i] = filepath.Join(tempRoot, fmt.Sprintf("server-%d", i))
		err := os.MkdirAll(serverDirs[i], 0755)
		require.NoError(t, err)

		// Assign ports (each server gets its own ports)
		serverPorts[i] = basePort + i
		clusterPorts[i] = baseClusterPort + i

		// Format the cluster URL that other servers will use to connect to this one
		routeURLs = append(routeURLs, fmt.Sprintf("nats://127.0.0.1:%d", clusterPorts[i]))
	}

	// Start the cluster
	servers := make([]*server.Server, numServers)
	for i := 0; i < numServers; i++ {
		// Configure each server
		opts := server.Options{
			ServerName: fmt.Sprintf("server-%d", i), // Required for JetStream clustering
			Host:       "127.0.0.1",
			Port:       serverPorts[i],
			Cluster: server.ClusterOpts{
				Name: clusterName,
				Host: "127.0.0.1",
				Port: clusterPorts[i],
			},
			JetStream: true,
			StoreDir:  serverDirs[i],
			NoLog:     true,
			NoSigs:    true,
		}

		// Add routes to other servers (except self)
		for j := 0; j < numServers; j++ {
			if i != j {
				// Parse route URL string to URL object
				routeURL, err := url.Parse(routeURLs[j])
				require.NoError(t, err)
				opts.Routes = append(opts.Routes, routeURL)
			}
		}

		// Create and start the server
		servers[i], err = server.NewServer(&opts)
		require.NoError(t, err)

		go servers[i].Start()
		defer servers[i].Shutdown()

		// Wait for server to be ready
		if !servers[i].ReadyForConnections(5 * time.Second) {
			t.Fatalf("Server %d failed to start", i)
		}
	}

	// Wait for cluster to form
	time.Sleep(2 * time.Second)

	// Verify cluster formation
	for i, s := range servers {
		numRoutes := s.NumRoutes()
		expect := numServers - 1 // Each server connects to all others but not itself

		// Wait up to 5 seconds for routes to be established
		timeout := time.Now().Add(5 * time.Second)
		for time.Now().Before(timeout) && numRoutes < expect {
			time.Sleep(250 * time.Millisecond)
			numRoutes = s.NumRoutes()
		}

		if numRoutes < expect {
			t.Fatalf("Server %d has %d routes, expected %d", i, numRoutes, expect)
		}
	}

	// Wait for JetStream cluster to be formed
	time.Sleep(2 * time.Second)

	// Connect to the first server
	firstServerConn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", serverPorts[0]))
	require.NoError(t, err)
	defer firstServerConn.Close()

	// Create JetStream context
	js, err := jetstream.New(firstServerConn)
	require.NoError(t, err)

	// Create a stream with replication factor of 3 (all servers)
	streamName := "auto_replicated_stream"
	ctx := context.Background()

	// Create a replicated stream (R3 means replicated across 3 servers)
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamName + ".>"},
		Replicas: 3, // Replicate to all 3 servers - THIS IS THE KEY FOR AUTO-REPLICATION
	})
	require.NoError(t, err)

	// Wait for stream to be fully replicated
	time.Sleep(1 * time.Second)

	// Get stream info to verify replication is configured
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	t.Logf("Stream has %d replicas", info.Config.Replicas)
	assert.Equal(t, 3, info.Config.Replicas, "Stream should be configured with 3 replicas")

	// Publish messages to the stream via first server
	const messageCount = 10
	for i := 1; i <= messageCount; i++ {
		msgData := fmt.Sprintf("Auto-replicated message %d", i)
		_, err = js.Publish(ctx, fmt.Sprintf("%s.msg.%d", streamName, i), []byte(msgData))
		require.NoError(t, err)
	}

	// Verify messages are in the stream
	info, err = stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount), info.State.Msgs, "Stream should have all messages")

	// Now connect to the third server to verify messages were replicated
	thirdServerConn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", serverPorts[2]))
	require.NoError(t, err)
	defer thirdServerConn.Close()

	// Create JetStream context on the third server
	thirdJS, err := jetstream.New(thirdServerConn)
	require.NoError(t, err)

	// Get stream info from third server to verify messages were replicated
	thirdStream, err := thirdJS.Stream(ctx, streamName)
	require.NoError(t, err)

	thirdInfo, err := thirdStream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount), thirdInfo.State.Msgs, "Third server should have all replicated messages")

	// Create a consumer on the third server to read messages
	consumer, err := thirdJS.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable:       "third_server_consumer",
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	// Read messages from the third server
	receivedMessages := make([]string, 0, messageCount)
	allReceived := make(chan bool)

	sub, err := consumer.Consume(func(msg jetstream.Msg) {
		receivedMessages = append(receivedMessages, string(msg.Data()))
		err := msg.Ack()
		assert.NoError(t, err)

		if len(receivedMessages) == messageCount {
			allReceived <- true
		}
	})
	require.NoError(t, err)
	defer sub.Stop()

	// Wait for all messages to be received
	select {
	case <-allReceived:
		// All messages received
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for messages, got %d of %d", len(receivedMessages), messageCount)
	}

	assert.Equal(t, messageCount, len(receivedMessages), "Should have received all auto-replicated messages")

	// Now shut down the first server (where we published the messages)
	servers[0].Shutdown()
	time.Sleep(1 * time.Second)

	// Verify we can still access the stream and its messages from the third server
	thirdStreamAfterShutdown, err := thirdJS.Stream(ctx, streamName)
	require.NoError(t, err)

	thirdInfoAfterShutdown, err := thirdStreamAfterShutdown.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount), thirdInfoAfterShutdown.State.Msgs,
		"Stream should maintain all messages after first server shutdown")

	// Publish new messages to the third server after first server shutdown
	const additionalMessages = 5
	for i := messageCount + 1; i <= messageCount+additionalMessages; i++ {
		msgData := fmt.Sprintf("Post-shutdown message %d", i)
		_, err = thirdJS.Publish(ctx, fmt.Sprintf("%s.msg.%d", streamName, i), []byte(msgData))
		require.NoError(t, err)
	}

	// Verify new messages were added
	finalInfo, err := thirdStreamAfterShutdown.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount+additionalMessages), finalInfo.State.Msgs,
		"Stream should have original plus new messages")

	// The key point demonstrated by this test is:
	// 1. When using a proper NATS cluster with JetStream enabled
	// 2. Setting the Replicas field in StreamConfig automatically handles replication
	// 3. No manual replication code is needed - NATS handles it internally
	// 4. Fault tolerance is built-in, allowing continued operation if some servers go down
}
