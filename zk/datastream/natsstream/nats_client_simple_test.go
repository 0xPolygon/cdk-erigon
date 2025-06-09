package natsstream

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestSimpleNATSFlow(t *testing.T) {
	// Setup NATS server
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	// Connect directly to verify setup
	nc, err := nats.Connect(url)
	require.NoError(t, err)
	defer nc.Close()

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Create stream
	streamName := "DATASTREAM_1101"
	_, err = js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{"datastream.1101.>"},
	})
	require.NoError(t, err)

	// Create and start client
	client := NewNATSClient(context.Background(), url, false, 1101, 7, log.New())
	err = client.Start()
	require.NoError(t, err)
	defer client.Stop()

	// Start reading
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	// Wait for consumer to be ready
	time.Sleep(500 * time.Millisecond)

	// Publish a simple message directly
	l2Block := &datastream.L2Block{
		Number:    1,
		Timestamp: uint64(time.Now().Unix()),
		Hash:      common.Hash{0x01}.Bytes(),
	}

	msgData, err := proto.Marshal(l2Block)
	require.NoError(t, err)

	// Try different subject patterns
	subjects := []string{
		"datastream.1101.L2_BLOCK",
		"datastream.1101.L2_BLOCK.1",
		"datastream.1101.1",
	}

	for _, subject := range subjects {
		t.Logf("Trying subject: %s", subject)

		headers := nats.Header{}
		headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2Block))
		headers.Set("EntryNumber", "1")

		msg := &nats.Msg{
			Subject: subject,
			Data:    msgData,
			Header:  headers,
		}

		_, err = js.PublishMsg(context.Background(), msg)
		require.NoError(t, err)
	}

	// Check if we get any message
	entryChan := client.GetEntryChan()

	select {
	case entry := <-*entryChan:
		t.Logf("Received entry: %+v", entry)
		fullBlock, ok := entry.(*types.FullL2Block)
		assert.True(t, ok)
		assert.Equal(t, uint64(1), fullBlock.L2BlockNumber)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for entry")
	}
}

func TestDirectJetStreamRead(t *testing.T) {
	// Setup NATS server
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	// Connect directly
	nc, err := nats.Connect(url)
	require.NoError(t, err)
	defer nc.Close()

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Create stream
	streamName := "TEST_STREAM"
	stream, err := js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{"test.>"},
	})
	require.NoError(t, err)

	// Publish a test message
	ack, err := js.Publish(context.Background(), "test.foo", []byte("hello"))
	require.NoError(t, err)
	t.Logf("Published message with sequence: %d", ack.Sequence)

	// Create consumer
	consumer, err := js.CreateOrUpdateConsumer(context.Background(), streamName, jetstream.ConsumerConfig{
		Durable:       "test-consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	require.NoError(t, err)

	// Read message
	iter, err := consumer.Messages()
	require.NoError(t, err)

	msgChan := make(chan jetstream.Msg, 1)
	go func() {
		msg, err := iter.Next()
		if err != nil {
			t.Logf("Error getting message: %v", err)
			return
		}
		msgChan <- msg
	}()

	select {
	case msg := <-msgChan:
		t.Logf("Received message: subject=%s, data=%s", msg.Subject(), string(msg.Data()))
		msg.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}

	// Check stream info
	info, err := stream.Info(context.Background())
	require.NoError(t, err)
	t.Logf("Stream has %d messages", info.State.Msgs)
}
