package nats_test

import (
	"context"
	"fmt"
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

// TestNatsStreamSourcing tests:
// 1. JetStream's built-in capabilities for replicating data between streams
// 2. Using source streams and mirror streams for automatic replication
// 3. All running within a single NATS server for simplicity
func TestNatsStreamSourcing(t *testing.T) {
	// Create temp directory for the server
	tempRoot, err := os.MkdirTemp("", "nats-stream-sourcing-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempRoot)

	// Configure and start a single NATS server with JetStream
	serverPort := 14222
	serverDir := filepath.Join(tempRoot, "server")
	err = os.MkdirAll(serverDir, 0755)
	require.NoError(t, err)

	opts := server.Options{
		ServerName: "sourcing-server",
		Host:       "127.0.0.1",
		Port:       serverPort,
		JetStream:  true,
		StoreDir:   serverDir,
		NoLog:      true,
		NoSigs:     true,
	}

	natsServer, err := server.NewServer(&opts)
	require.NoError(t, err)

	go natsServer.Start()
	defer natsServer.Shutdown()

	// Wait for server to be ready
	if !natsServer.ReadyForConnections(5 * time.Second) {
		t.Fatalf("Server failed to start")
	}

	// Connect to the server
	nc, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", serverPort))
	require.NoError(t, err)
	defer nc.Close()

	// Create JetStream context
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	ctx := context.Background()

	// Create source streams that will be the origin of data
	sourceStream1 := "source_stream_1"
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     sourceStream1,
		Subjects: []string{sourceStream1 + ".>"},
	})
	require.NoError(t, err)

	sourceStream2 := "source_stream_2"
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     sourceStream2,
		Subjects: []string{sourceStream2 + ".>"},
	})
	require.NoError(t, err)

	// Publish messages to both source streams
	const messageCount = 5
	for i := 1; i <= messageCount; i++ {
		// Publish to source 1
		msgData1 := fmt.Sprintf("Message %d from source 1", i)
		_, err = js.Publish(ctx, fmt.Sprintf("%s.msg.%d", sourceStream1, i), []byte(msgData1))
		require.NoError(t, err)

		// Publish to source 2
		msgData2 := fmt.Sprintf("Message %d from source 2", i)
		_, err = js.Publish(ctx, fmt.Sprintf("%s.msg.%d", sourceStream2, i), []byte(msgData2))
		require.NoError(t, err)
	}

	// Verify source streams have their messages
	stream1, err := js.Stream(ctx, sourceStream1)
	require.NoError(t, err)
	info1, err := stream1.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount), info1.State.Msgs, "Source stream 1 should have all messages")

	stream2, err := js.Stream(ctx, sourceStream2)
	require.NoError(t, err)
	info2, err := stream2.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(messageCount), info2.State.Msgs, "Source stream 2 should have all messages")

	// 1. APPROACH 1: Create a mirror stream that replicates from a source
	// A mirror is an exact copy of another stream
	mirrorName := "mirror_of_source_1"
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: mirrorName,
		Mirror: &jetstream.StreamSource{
			Name: sourceStream1,
		},
	})
	require.NoError(t, err)

	// Wait for mirror to sync
	time.Sleep(1 * time.Second)

	// Verify mirror has the same messages as source
	mirrorStream, err := js.Stream(ctx, mirrorName)
	require.NoError(t, err)
	mirrorInfo, err := mirrorStream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, info1.State.Msgs, mirrorInfo.State.Msgs, "Mirror should have same message count as source")

	// 2. APPROACH 2: Create a multi-sourced stream that combines multiple sources
	// This aggregates messages from multiple streams
	multiSourceName := "combined_sources"
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: multiSourceName,
		Sources: []*jetstream.StreamSource{
			{Name: sourceStream1},
			{Name: sourceStream2},
		},
	})
	require.NoError(t, err)

	// Wait for sources to sync
	time.Sleep(1 * time.Second)

	// Verify multi-source stream has combined messages from both sources
	multiSourceStream, err := js.Stream(ctx, multiSourceName)
	require.NoError(t, err)
	multiSourceInfo, err := multiSourceStream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, info1.State.Msgs+info2.State.Msgs, multiSourceInfo.State.Msgs,
		"Multi-source stream should have combined messages from both sources")

	// Read messages from the mirror to verify content
	mirrorConsumer, err := js.CreateOrUpdateConsumer(ctx, mirrorName, jetstream.ConsumerConfig{
		Durable:       "mirror_consumer",
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	mirrorMessages := make([]string, 0, messageCount)
	mirrorDone := make(chan bool)

	sub1, err := mirrorConsumer.Consume(func(msg jetstream.Msg) {
		mirrorMessages = append(mirrorMessages, string(msg.Data()))
		err := msg.Ack()
		assert.NoError(t, err)

		if len(mirrorMessages) == messageCount {
			mirrorDone <- true
		}
	})
	require.NoError(t, err)
	defer sub1.Stop()

	// Wait for all mirror messages
	select {
	case <-mirrorDone:
		// All messages received
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for mirror messages, got %d of %d",
			len(mirrorMessages), messageCount)
	}

	// Read messages from the multi-source stream to verify content
	multiConsumer, err := js.CreateOrUpdateConsumer(ctx, multiSourceName, jetstream.ConsumerConfig{
		Durable:       "multi_consumer",
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	multiMessages := make([]string, 0, messageCount*2)
	multiDone := make(chan bool)

	sub2, err := multiConsumer.Consume(func(msg jetstream.Msg) {
		multiMessages = append(multiMessages, string(msg.Data()))
		err := msg.Ack()
		assert.NoError(t, err)

		if len(multiMessages) == messageCount*2 {
			multiDone <- true
		}
	})
	require.NoError(t, err)
	defer sub2.Stop()

	// Wait for all multi-source messages
	select {
	case <-multiDone:
		// All messages received
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for multi-source messages, got %d of %d",
			len(multiMessages), messageCount*2)
	}

	// Verify we received the correct number of messages
	assert.Equal(t, messageCount, len(mirrorMessages),
		"Should have received all messages from mirror")
	assert.Equal(t, messageCount*2, len(multiMessages),
		"Should have received all messages from both sources in multi-source stream")

	// Now let's publish more messages to source 1 and verify they automatically appear in mirror and multi-source
	for i := messageCount + 1; i <= messageCount+3; i++ {
		msgData := fmt.Sprintf("Additional message %d from source 1", i)
		_, err = js.Publish(ctx, fmt.Sprintf("%s.msg.%d", sourceStream1, i), []byte(msgData))
		require.NoError(t, err)
	}

	// Wait for sync
	time.Sleep(2 * time.Second)

	// Check updated counts
	updatedSource1, err := js.Stream(ctx, sourceStream1)
	require.NoError(t, err)
	updatedInfo1, err := updatedSource1.Info(ctx)
	require.NoError(t, err)
	expectedSource1Count := uint64(messageCount + 3)
	assert.Equal(t, expectedSource1Count, updatedInfo1.State.Msgs,
		"Source 1 should have original plus new messages")

	updatedMirror, err := js.Stream(ctx, mirrorName)
	require.NoError(t, err)
	updatedMirrorInfo, err := updatedMirror.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, expectedSource1Count, updatedMirrorInfo.State.Msgs,
		"Mirror should automatically have new messages from source 1")

	updatedMulti, err := js.Stream(ctx, multiSourceName)
	require.NoError(t, err)
	updatedMultiInfo, err := updatedMulti.Info(ctx)
	require.NoError(t, err)
	expectedMultiCount := uint64(messageCount*2 + 3) // Original from both sources + 3 new from source 1
	assert.Equal(t, expectedMultiCount, updatedMultiInfo.State.Msgs,
		"Multi-source should automatically have new messages from source 1")

	// The key points demonstrated by this test:
	// 1. JetStream provides built-in capabilities for stream replication via Mirror and Sources
	// 2. Mirror creates an exact copy of another stream
	// 3. Sources allow combining multiple streams into one
	// 4. Replication happens automatically without manual coding
	// 5. New messages are automatically propagated to mirrors and sourced streams
}
