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

// TestNatsDynamicCluster tests:
// 1. Starting with a single NATS server with JetStream enabled (but not in cluster mode)
// 2. Dynamically adding additional servers to the cluster
// 3. Automatic replication of JetStream data to newly added servers
// 4. Reading from new servers to verify data availability
func TestNatsDynamicCluster(t *testing.T) {
	t.Skip("dynamic clustering has proven not to work as expected, test remains for reference")
	// Create temp directories for each server
	tempRoot, err := os.MkdirTemp("", "nats-dynamic-cluster-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempRoot)

	// Define cluster configuration
	clusterName := "dynamic_cluster"
	maxServers := 3
	serverDirs := make([]string, maxServers)
	serverPorts := make([]int, maxServers)
	clusterPorts := make([]int, maxServers)
	servers := make([]*server.Server, maxServers)

	// Use base ports - use higher numbers to avoid conflicts
	baseServerPort := 24222
	baseClusterPort := 26222

	// Setup directories and ports
	for i := 0; i < maxServers; i++ {
		serverDirs[i] = filepath.Join(tempRoot, fmt.Sprintf("server-%d", i))
		err := os.MkdirAll(serverDirs[i], 0755)
		require.NoError(t, err)

		serverPorts[i] = baseServerPort + i
		clusterPorts[i] = baseClusterPort + i
	}

	// Common context for operations
	ctx := context.Background()

	// Step 1: Start with a non-clustered seed server
	t.Log("Step 1: Starting seed server without clustering...")

	// Configure the seed server with JetStream but no clustering
	seedOpts := &server.Options{
		ServerName: "server-0",
		Host:       "127.0.0.1",
		Port:       serverPorts[0],
		JetStream:  true,
		StoreDir:   serverDirs[0],
		Debug:      true,
		Trace:      true,
	}

	// Create and start the seed server
	servers[0], err = server.NewServer(seedOpts)
	require.NoError(t, err)
	servers[0].ConfigureLogger()
	go servers[0].Start()

	// Wait for server to be ready
	require.True(t, servers[0].ReadyForConnections(5*time.Second),
		"Seed server failed to start")
	t.Log("Seed server started successfully")

	// Connect to seed server
	seedURL := fmt.Sprintf("nats://127.0.0.1:%d", serverPorts[0])
	seedConn, err := nats.Connect(seedURL)
	require.NoError(t, err)
	defer seedConn.Close()

	// Create JetStream context
	seedJS, err := jetstream.New(seedConn)
	require.NoError(t, err)

	// Create a stream with a single replica (no clustering yet)
	streamName := "dynamic_stream"
	stream, err := seedJS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamName + ".>"},
		Replicas: 1, // Single replica since no clustering
	})
	require.NoError(t, err)

	// Publish some initial messages
	const initialMsgCount = 5
	for i := 1; i <= initialMsgCount; i++ {
		_, err = seedJS.Publish(ctx, fmt.Sprintf("%s.seed.%d", streamName, i),
			[]byte(fmt.Sprintf("Initial message %d", i)))
		require.NoError(t, err)
	}

	// Verify stream has the messages
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(initialMsgCount), info.State.Msgs,
		"Stream should have initial messages")

	// Step 2: Shut down the seed server to reconfigure with clustering
	t.Log("Step 2: Shutting down seed server to reconfigure with clustering...")
	servers[0].Shutdown()
	time.Sleep(2 * time.Second)

	// Step 3: Start the second server with cluster config
	t.Log("Step 3: Starting second server with cluster config...")
	server2Opts := &server.Options{
		ServerName: "server-1",
		Host:       "127.0.0.1",
		Port:       serverPorts[1],
		Cluster: server.ClusterOpts{
			Name: clusterName,
			Host: "127.0.0.1",
			Port: clusterPorts[1],
		},
		JetStream: true,
		StoreDir:  serverDirs[1],
		Debug:     true,
		Trace:     true,
	}

	servers[1], err = server.NewServer(server2Opts)
	require.NoError(t, err)
	servers[1].ConfigureLogger()
	go servers[1].Start()

	require.True(t, servers[1].ReadyForConnections(5*time.Second),
		"Second server failed to start")
	t.Log("Second server started successfully")

	// Step 4: Restart the seed server with cluster config and route to second server
	t.Log("Step 4: Restarting seed server with cluster config...")

	// Update seed server options with cluster config
	seedClusterOpts := &server.Options{
		ServerName: "server-0",
		Host:       "127.0.0.1",
		Port:       serverPorts[0],
		Cluster: server.ClusterOpts{
			Name: clusterName,
			Host: "127.0.0.1",
			Port: clusterPorts[0],
		},
		JetStream: true,
		StoreDir:  serverDirs[0], // Same store dir to keep data
		Debug:     true,
		Trace:     true,
	}

	// Add route to second server
	server2Route, err := url.Parse(fmt.Sprintf("nats://127.0.0.1:%d", clusterPorts[1]))
	require.NoError(t, err)
	seedClusterOpts.Routes = []*url.URL{server2Route}

	// Create and start the seed server with clustering
	servers[0], err = server.NewServer(seedClusterOpts)
	require.NoError(t, err)
	servers[0].ConfigureLogger()
	go servers[0].Start()

	require.True(t, servers[0].ReadyForConnections(5*time.Second),
		"Seed server failed to restart with clustering")
	t.Log("Seed server restarted with clustering")

	// Step 5: Update the second server to route to the seed server
	t.Log("Step 5: Updating second server with route to seed server...")
	servers[1].Shutdown()
	time.Sleep(2 * time.Second)

	// Update second server with route to seed
	server2OptsWithRoute := &server.Options{
		ServerName: "server-1",
		Host:       "127.0.0.1",
		Port:       serverPorts[1],
		Cluster: server.ClusterOpts{
			Name: clusterName,
			Host: "127.0.0.1",
			Port: clusterPorts[1],
		},
		JetStream: true,
		StoreDir:  serverDirs[1],
		Debug:     true,
		Trace:     true,
	}

	// Add route to seed server
	seedRoute, err := url.Parse(fmt.Sprintf("nats://127.0.0.1:%d", clusterPorts[0]))
	require.NoError(t, err)
	server2OptsWithRoute.Routes = []*url.URL{seedRoute}

	// Restart the second server
	servers[1], err = server.NewServer(server2OptsWithRoute)
	require.NoError(t, err)
	servers[1].ConfigureLogger()
	go servers[1].Start()

	require.True(t, servers[1].ReadyForConnections(5*time.Second),
		"Second server failed to restart with route")
	t.Log("Second server restarted with route to seed")

	// Step 6: Wait for cluster to form and verify routes
	t.Log("Step 6: Waiting for cluster to form...")

	// Function to check if a server has the expected number of routes
	waitForRoutes := func(s *server.Server, expected int, timeout time.Duration) error {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if routes := s.NumRoutes(); routes >= expected {
				t.Logf("Server %s has %d routes established", s.Name(), routes)
				return nil
			}
			t.Logf("Server %s has %d routes, waiting for %d...",
				s.Name(), s.NumRoutes(), expected)
			time.Sleep(500 * time.Millisecond)
		}
		return fmt.Errorf("timeout waiting for server %s to establish %d routes, has %d",
			s.Name(), expected, s.NumRoutes())
	}

	// Check routes on both servers
	err = waitForRoutes(servers[0], 1, 20*time.Second)
	require.NoError(t, err, "Seed server failed to establish route")

	err = waitForRoutes(servers[1], 1, 20*time.Second)
	require.NoError(t, err, "Second server failed to establish route")

	// Give more time for cluster to stabilize
	t.Log("Cluster formed, waiting for stabilization...")
	time.Sleep(10 * time.Second)

	// Step 7: Reconnect to seed server and recreate JetStream context
	t.Log("Step 7: Reconnecting to seed server...")
	seedConn, err = nats.Connect(seedURL)
	require.NoError(t, err)
	defer seedConn.Close()

	// Connect to second server
	server2URL := fmt.Sprintf("nats://127.0.0.1:%d", serverPorts[1])
	server2Conn, err := nats.Connect(server2URL)
	require.NoError(t, err)
	defer server2Conn.Close()

	// Create JetStream contexts for both servers
	seedJS, err = jetstream.New(seedConn)
	require.NoError(t, err)

	server2JS, err := jetstream.New(server2Conn)
	require.NoError(t, err)

	// Function to poll for stream availability with retries
	pollForStream := func(js jetstream.JetStream, streamName string, expectedMsgs uint64,
		expectedReplicas int, timeout time.Duration) (*jetstream.StreamInfo, error) {
		deadline := time.Now().Add(timeout)

		for time.Now().Before(deadline) {
			// Try to get the stream
			streamObj, err := js.Stream(ctx, streamName)
			if err != nil {
				t.Logf("Stream not available yet: %v", err)
				time.Sleep(time.Second)
				continue
			}

			// Try to get stream info
			info, err := streamObj.Info(ctx)
			if err != nil {
				t.Logf("Stream info not available yet: %v", err)
				time.Sleep(time.Second)
				continue
			}

			// Check if the stream has the expected properties
			if info.State.Msgs == expectedMsgs && info.Config.Replicas == expectedReplicas {
				t.Logf("Stream has expected %d messages with %d replicas",
					info.State.Msgs, info.Config.Replicas)
				return info, nil
			}

			t.Logf("Stream not fully replicated yet. Got msgs=%d (expected %d), replicas=%d (expected %d)",
				info.State.Msgs, expectedMsgs, info.Config.Replicas, expectedReplicas)
			time.Sleep(2 * time.Second)
		}

		return nil, fmt.Errorf("timeout waiting for stream to be available with expected properties")
	}

	// Step 8: Update the stream to use 2 replicas now that we have a cluster
	t.Log("Step 8: Updating stream to use 2 replicas...")

	// Retry stream update with increasing timeouts
	var updateErr error
	for attempt := 1; attempt <= 5; attempt++ {
		t.Logf("Attempt %d to update stream to 2 replicas...", attempt)
		_, updateErr = seedJS.UpdateStream(ctx, jetstream.StreamConfig{
			Name:     streamName,
			Subjects: []string{streamName + ".>"},
			Replicas: 2, // Update to 2 replicas for our 2-node cluster
		})

		if updateErr == nil {
			break
		}
		t.Logf("Failed to update stream: %v. Retrying after delay...", updateErr)
		time.Sleep(time.Duration(attempt*3) * time.Second)
	}
	require.NoError(t, updateErr, "Failed to update stream to 2 replicas")
	t.Log("Successfully updated stream to 2 replicas")

	// Step 9: Wait for replication to complete
	t.Log("Step 9: Waiting for replication to complete...")
	time.Sleep(10 * time.Second)

	// Verify replication on both servers
	seedInfo, err := pollForStream(seedJS, streamName, initialMsgCount, 2, 30*time.Second)
	require.NoError(t, err, "Failed to verify stream on seed server")
	assert.Equal(t, 2, seedInfo.Config.Replicas, "Stream should have 2 replicas")

	server2Info, err := pollForStream(server2JS, streamName, initialMsgCount, 2, 30*time.Second)
	require.NoError(t, err, "Failed to verify stream on second server")
	assert.Equal(t, 2, server2Info.Config.Replicas, "Stream should have 2 replicas")
	assert.Equal(t, uint64(initialMsgCount), server2Info.State.Msgs,
		"Second server should have all messages replicated")

	// Step 10: Add more messages from the second server
	t.Log("Step 10: Publishing messages from second server...")
	const secondServerMsgCount = 5
	for i := 1; i <= secondServerMsgCount; i++ {
		_, err = server2JS.Publish(ctx, fmt.Sprintf("%s.server2.%d", streamName, i),
			[]byte(fmt.Sprintf("Server2 message %d", i)))
		require.NoError(t, err)
	}

	// Wait for replication back to seed server
	time.Sleep(5 * time.Second)

	// Verify both servers have all messages
	totalMsgs := initialMsgCount + secondServerMsgCount

	seedInfoUpdated, err := pollForStream(seedJS, streamName, uint64(totalMsgs), 2, 30*time.Second)
	require.NoError(t, err, "Failed to verify updated stream on seed server")
	assert.Equal(t, uint64(totalMsgs), seedInfoUpdated.State.Msgs,
		"Seed server should have all messages")

	// Step 11: Add a third server to the cluster
	t.Log("Step 11: Adding third server to the cluster...")

	// Configure third server with routes to both existing servers
	server3Opts := &server.Options{
		ServerName: "server-2",
		Host:       "127.0.0.1",
		Port:       serverPorts[2],
		Cluster: server.ClusterOpts{
			Name: clusterName,
			Host: "127.0.0.1",
			Port: clusterPorts[2],
		},
		JetStream: true,
		StoreDir:  serverDirs[2],
		Debug:     true,
		Trace:     true,
	}

	// Add routes to both existing servers
	route0, err := url.Parse(fmt.Sprintf("nats://127.0.0.1:%d", clusterPorts[0]))
	require.NoError(t, err)
	route1, err := url.Parse(fmt.Sprintf("nats://127.0.0.1:%d", clusterPorts[1]))
	require.NoError(t, err)
	server3Opts.Routes = []*url.URL{route0, route1}

	// Start the third server
	servers[2], err = server.NewServer(server3Opts)
	require.NoError(t, err)
	servers[2].ConfigureLogger()
	go servers[2].Start()

	require.True(t, servers[2].ReadyForConnections(5*time.Second),
		"Third server failed to start")
	t.Log("Third server started successfully")

	// Step 12: Wait for all servers to see each other
	t.Log("Step 12: Waiting for complete cluster formation...")

	// Each server should have maxServers-1 routes
	for i, s := range servers {
		err = waitForRoutes(s, maxServers-1, 30*time.Second)
		require.NoError(t, err, "Server %d failed to establish expected routes", i)
	}
	t.Log("Complete 3-node cluster has formed")

	// Wait for cluster stabilization
	time.Sleep(10 * time.Second)

	// Step 13: Connect to third server and create JS context
	t.Log("Step 13: Connecting to third server...")
	server3URL := fmt.Sprintf("nats://127.0.0.1:%d", serverPorts[2])
	server3Conn, err := nats.Connect(server3URL)
	require.NoError(t, err)
	defer server3Conn.Close()

	server3JS, err := jetstream.New(server3Conn)
	require.NoError(t, err)

	// Step 14: Update stream to use 3 replicas
	t.Log("Step 14: Updating stream to use 3 replicas...")
	for attempt := 1; attempt <= 5; attempt++ {
		t.Logf("Attempt %d to update stream to 3 replicas...", attempt)
		_, updateErr = seedJS.UpdateStream(ctx, jetstream.StreamConfig{
			Name:     streamName,
			Subjects: []string{streamName + ".>"},
			Replicas: 3, // Update to use all 3 servers
		})

		if updateErr == nil {
			break
		}
		t.Logf("Failed to update stream: %v. Retrying after delay...", updateErr)
		time.Sleep(time.Duration(attempt*3) * time.Second)
	}
	require.NoError(t, updateErr, "Failed to update stream to 3 replicas")
	t.Log("Successfully updated stream to 3 replicas")

	// Step 15: Wait for replication to third server and verify
	t.Log("Step 15: Waiting for replication to third server...")
	time.Sleep(15 * time.Second)

	server3Info, err := pollForStream(server3JS, streamName, uint64(totalMsgs), 3, 60*time.Second)
	require.NoError(t, err, "Failed to verify stream on third server")
	assert.Equal(t, 3, server3Info.Config.Replicas, "Stream should have 3 replicas")
	assert.Equal(t, uint64(totalMsgs), server3Info.State.Msgs,
		"Third server should have all messages replicated")

	// Step 16: Test fault tolerance by shutting down the seed server
	t.Log("Step 16: Testing fault tolerance by shutting down seed server...")
	servers[0].Shutdown()
	time.Sleep(10 * time.Second) // Give time for the cluster to detect and handle the shutdown

	// Step 17: Publish from the third server after seed is down
	t.Log("Step 17: Publishing messages from third server with seed down...")
	const server3MsgCount = 3
	for i := 1; i <= server3MsgCount; i++ {
		_, err = server3JS.Publish(ctx, fmt.Sprintf("%s.server3.%d", streamName, i),
			[]byte(fmt.Sprintf("Server3 message %d", i)))
		require.NoError(t, err)
	}

	// Wait for replication between remaining servers
	time.Sleep(5 * time.Second)

	// Verify messages were stored despite seed server being down
	finalTotal := totalMsgs + server3MsgCount
	server2Final, err := pollForStream(server2JS, streamName, uint64(finalTotal), 3, 30*time.Second)
	require.NoError(t, err, "Failed to verify final state on second server")
	assert.Equal(t, uint64(finalTotal), server2Final.State.Msgs,
		"Second server should have all messages including those published after seed shutdown")

	t.Log("Dynamic cluster test completed successfully")
}
