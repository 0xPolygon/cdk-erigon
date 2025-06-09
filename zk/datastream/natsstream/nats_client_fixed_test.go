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

func TestNATSClient_BasicFlow(t *testing.T) {
	// Setup server
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	// Create stream
	createTestStream(t, url, 1101)

	// Create client with a regular context (not timeout)
	client := NewNATSClient(context.Background(), url, false, 1101, 7, log.New())
	err := client.Start()
	require.NoError(t, err)

	// Start reading
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	// Give consumer time to start
	time.Sleep(200 * time.Millisecond)

	// Publish messages
	nc := client.nc
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Publish L2 block
	l2Block := &datastream.L2Block{
		Number:    1,
		Timestamp: uint64(time.Now().Unix()),
		Hash:      common.Hash{0x01}.Bytes(),
	}

	msgData, err := proto.Marshal(l2Block)
	require.NoError(t, err)

	headers := nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2Block))
	headers.Set("EntryNumber", "1")

	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    msgData,
		Header:  headers,
	}

	_, err = js.PublishMsg(context.Background(), msg)
	require.NoError(t, err)

	// Publish L2 block end
	blockEnd := &datastream.L2BlockEnd{
		Number: 1,
	}

	blockEndData, err := proto.Marshal(blockEnd)
	require.NoError(t, err)

	headers = nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2BlockEnd))
	headers.Set("EntryNumber", "2")

	msg = &nats.Msg{
		Subject: "datastream.entry",
		Data:    blockEndData,
		Header:  headers,
	}

	_, err = js.PublishMsg(context.Background(), msg)
	require.NoError(t, err)

	// Read the entry
	entryChan := client.GetEntryChan()

	select {
	case entry := <-*entryChan:
		fullBlock, ok := entry.(*types.FullL2Block)
		assert.True(t, ok)
		assert.Equal(t, uint64(1), fullBlock.L2BlockNumber)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for entry")
	}

	// Stop reading before stopping client
	client.StopReadingToChannel()
	time.Sleep(100 * time.Millisecond)

	// Now stop the client
	err = client.Stop()
	assert.NoError(t, err)
}
