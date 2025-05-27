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

// TestNatsClusterReplication tests a NATS cluster with JetStream replication:
// 1. Create a cluster of 3 NATS servers
// 2. Configure JetStream with a replicated stream across the cluster
// 3. Publish messages to one server
// 4. Verify messages can be consumed from another server in the cluster
func TestNatsClusterReplication(t *testing.T) {
	// Create temp directories for each server
	tempRoot, err := os.MkdirTemp("", "nats-cluster-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempRoot)

	// Configure cluster with 3 nodes
	clusterName := "erigon_test_cluster"
	numServers := 3
	serverDirs := make([]string, numServers)
	serverPorts := make([]int, numServers)
	clusterPorts := make([]int, numServers)

	// Use random ports starting from these bases
	basePort := 4222
	baseClusterPort := 6222

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
			StoreDir:           serverDirs[i],
			JetStream:          true,
			JetStreamMaxMemory: 1024 * 1024,      // 1MB
			JetStreamMaxStore:  1024 * 1024 * 10, // 10MB
			NoLog:              true,
			NoSigs:             true,
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

	// Create a connection to the first server
	serverConn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", serverPorts[0]))
	require.NoError(t, err)
	defer serverConn.Close()

	// Create JetStream context
	js, err := jetstream.New(serverConn)
	require.NoError(t, err)

	// Create a stream with replication factor of 3 (all servers)
	streamName := "replicated_stream"
	ctx := context.Background()

	// Create a replicated stream (R3 means replicated across 3 servers)
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamName + ".>"},
		Replicas: 3, // Replicate to all 3 servers
	})
	require.NoError(t, err)

	// Wait for stream to be fully replicated
	time.Sleep(1 * time.Second)

	// Publish messages to first server
	const messageCount = 10
	for i := 0; i < messageCount; i++ {
		_, err = js.Publish(ctx, streamName+".test", []byte(fmt.Sprintf("replicated message %d", i)))
		require.NoError(t, err)
	}

	// Now connect to the third server to verify messages were replicated
	clientConn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", serverPorts[2]))
	require.NoError(t, err)
	defer clientConn.Close()

	// Create JetStream context on the client (connected to server 3)
	clientJS, err := jetstream.New(clientConn)
	require.NoError(t, err)

	// Get stream info to verify it exists on server 3 with all messages
	streamInfo, err := clientJS.Stream(ctx, streamName)
	require.NoError(t, err)

	info, err := streamInfo.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount), info.State.Msgs,
		"Stream should have all messages replicated to server 3")

	// Consume messages from server 3
	received := make([]string, 0, messageCount)
	allReceived := make(chan bool)

	consumer, err := clientJS.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable:       "replicated_consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	require.NoError(t, err)

	sub, err := consumer.Consume(func(msg jetstream.Msg) {
		received = append(received, string(msg.Data()))

		err := msg.Ack()
		assert.NoError(t, err)

		if len(received) == messageCount {
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
		t.Fatal("Timed out waiting for replicated messages")
	}

	// Verify all messages were received (order may vary)
	assert.Equal(t, messageCount, len(received), "Should have received all replicated messages")

	// Shut down server 0 (where we published) to verify we can still access messages
	servers[0].Shutdown()

	// Wait for the cluster to stabilize
	time.Sleep(1 * time.Second)

	// Verify we can still get stream info from server 2 after server 0 is down
	streamInfo, err = clientJS.Stream(ctx, streamName)
	require.NoError(t, err)

	info, err = streamInfo.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount), info.State.Msgs,
		"Stream should maintain all messages after server 0 shutdown")
}
