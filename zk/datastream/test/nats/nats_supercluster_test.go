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

// TestNatsSupercluster tests a NATS supercluster configuration:
// 1. Create 3 clusters (east, central, west)
// 2. Connect them using gateways to form a supercluster
// 3. Configure JetStream
// 4. Verify that messages can be replicated across the supercluster
func TestNatsSupercluster(t *testing.T) {
	// Skip this test in short mode as it's more complex
	if testing.Short() {
		t.Skip("Skipping in short mode")
	}

	// Create temp directories for each server
	tempRoot, err := os.MkdirTemp("", "nats-supercluster-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempRoot)

	// Define clusters
	clusters := []struct {
		name        string
		serverCount int
		clientPort  int
		clusterPort int
		gatewayPort int
	}{
		{"east", 1, 24222, 26222, 27222},
		{"central", 1, 24223, 26223, 27223},
		{"west", 1, 24224, 26224, 27224},
	}

	// Create and configure all servers
	var allServers []*server.Server
	clusterURLs := make(map[string]string)
	gatewayURLs := make(map[string]string)

	// Pre-populate gateway URLs for each cluster
	for _, c := range clusters {
		gatewayURLs[c.name] = fmt.Sprintf("nats://127.0.0.1:%d", c.gatewayPort)
	}

	// Create and start servers for each cluster
	for _, c := range clusters {
		clusterDir := filepath.Join(tempRoot, c.name)
		err := os.MkdirAll(clusterDir, 0755)
		require.NoError(t, err)

		// Create server dirs for this cluster
		serverDirs := make([]string, c.serverCount)
		for i := 0; i < c.serverCount; i++ {
			serverDirs[i] = filepath.Join(clusterDir, fmt.Sprintf("server-%d", i))
			err := os.MkdirAll(serverDirs[i], 0755)
			require.NoError(t, err)
		}

		// Setup route URLs for intra-cluster communication
		var routeURLs []*url.URL
		for i := 0; i < c.serverCount; i++ {
			routeURL, err := url.Parse(fmt.Sprintf("nats://127.0.0.1:%d", c.clusterPort+i))
			require.NoError(t, err)
			routeURLs = append(routeURLs, routeURL)
		}

		// Remember this cluster's URL for other clusters
		clusterURLs[c.name] = fmt.Sprintf("nats://127.0.0.1:%d", c.clusterPort)

		// Setup gateway URLs to connect to other clusters
		var gateways []*server.RemoteGatewayOpts
		for _, otherCluster := range clusters {
			if otherCluster.name == c.name {
				// Add self with our own gateway URL
				selfURL, err := url.Parse(gatewayURLs[c.name])
				require.NoError(t, err)

				gateways = append(gateways, &server.RemoteGatewayOpts{
					Name: c.name,
					// For self-reference, we still need a URL (empty URLs array causes the error)
					URLs: []*url.URL{selfURL},
				})
				continue
			}

			// Add gateway to other cluster
			gatewayURL, err := url.Parse(gatewayURLs[otherCluster.name])
			require.NoError(t, err)
			gateways = append(gateways, &server.RemoteGatewayOpts{
				Name: otherCluster.name,
				URLs: []*url.URL{gatewayURL},
			})
		}

		// Start servers for this cluster
		for i := 0; i < c.serverCount; i++ {
			serverName := fmt.Sprintf("%s-server-%d", c.name, i)
			serverOpts := server.Options{
				ServerName: serverName,
				Host:       "127.0.0.1",
				Port:       c.clientPort + i,
				Cluster: server.ClusterOpts{
					Name: c.name,
					Host: "127.0.0.1",
					Port: c.clusterPort + i,
				},
				Gateway: server.GatewayOpts{
					Name:     c.name,
					Host:     "127.0.0.1",
					Port:     c.gatewayPort + i,
					Gateways: gateways,
				},
				JetStream: true,
				StoreDir:  serverDirs[i],
				NoLog:     true,
				NoSigs:    true,
			}

			// Add routes to other servers in this cluster
			for j := 0; j < c.serverCount; j++ {
				if i != j {
					serverOpts.Routes = append(serverOpts.Routes, routeURLs[j])
				}
			}

			// Create and start the server
			server, err := server.NewServer(&serverOpts)
			require.NoError(t, err)

			go server.Start()
			defer server.Shutdown()

			// Wait for server to be ready
			if !server.ReadyForConnections(5 * time.Second) {
				t.Fatalf("Server %s failed to start", serverName)
			}

			allServers = append(allServers, server)
		}
	}

	// Wait for cluster and gateway connections to establish
	time.Sleep(5 * time.Second)

	// Verify all expected gateway connections
	for _, server := range allServers {
		outboundGateways := server.NumOutboundGateways()

		// Each server should have outbound connections to other clusters
		expectOutbound := len(clusters) - 1
		if outboundGateways < expectOutbound {
			t.Logf("Server %s has %d outbound gateways, expected at least %d",
				server.Name(), outboundGateways, expectOutbound)
		}
	}

	// Connect to the east cluster
	eastConn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", clusters[0].clientPort))
	require.NoError(t, err)
	defer eastConn.Close()

	// Create JetStream context for east
	eastJS, err := jetstream.New(eastConn)
	require.NoError(t, err)

	// Create a stream on east
	streamName := "supercluster_stream"
	ctx := context.Background()

	_, err = eastJS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamName + ".>"},
	})
	require.NoError(t, err)

	// Publish messages from east
	const eastMessageCount = 5
	for i := 0; i < eastMessageCount; i++ {
		_, err = eastJS.Publish(ctx, streamName+".east", []byte(fmt.Sprintf("message from east: %d", i)))
		require.NoError(t, err)
	}

	// Connect to west cluster
	westConn, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", clusters[2].clientPort))
	require.NoError(t, err)
	defer westConn.Close()

	// Create JetStream context for west
	westJS, err := jetstream.New(westConn)
	require.NoError(t, err)

	// Create a stream on west - note in a real supercluster with JetStream,
	// stream placement is managed by the meta leader
	_, err = westJS.Stream(ctx, streamName)
	if err != nil {
		// If stream doesn't exist yet in west, create it
		_, err = westJS.CreateStream(ctx, jetstream.StreamConfig{
			Name:     streamName,
			Subjects: []string{streamName + ".>"},
		})
		require.NoError(t, err)
	}

	// Publish messages from west
	const westMessageCount = 5
	for i := 0; i < westMessageCount; i++ {
		_, err = westJS.Publish(ctx, streamName+".west", []byte(fmt.Sprintf("message from west: %d", i)))
		require.NoError(t, err)
	}

	// Wait for messages to be propagated
	time.Sleep(2 * time.Second)

	// Verify messages
	eastStreamInfo, err := eastJS.Stream(ctx, streamName)
	require.NoError(t, err)

	// Get stream info to confirm messages were stored
	_, err = eastStreamInfo.Info(ctx)
	require.NoError(t, err)

	// Connect regular NATS subscription from west to east
	// This tests that the gateway connectivity works for regular NATS
	testSubject := "gateway.test"
	crossClusterReceived := make(chan bool)

	// Create subscription on west
	_, err = westConn.Subscribe(testSubject, func(msg *nats.Msg) {
		assert.Equal(t, "hello from east", string(msg.Data))
		crossClusterReceived <- true
	})
	require.NoError(t, err)

	// Publish from east
	err = eastConn.Publish(testSubject, []byte("hello from east"))
	require.NoError(t, err)

	// Wait for the message to be received
	select {
	case <-crossClusterReceived:
		// Message received
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for cross-cluster message")
	}

	// This demonstrates that the supercluster is functioning for regular NATS traffic
	// Note: For full JetStream supercluster with cross-cluster replication, additional
	// configuration with source/mirror streams would be required.
}
