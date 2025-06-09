package natsstream

import (
	"context"
	"encoding/binary"
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

func TestServerBookmarkStorage(t *testing.T) {
	// Setup server
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	// Create stream
	createTestStream(t, url, 1101)

	ctx := context.Background()

	// Get JetStream
	nc, err := nats.Connect(url)
	require.NoError(t, err)
	defer nc.Close()

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Create KV store for simulating server-side operations
	kvName := fmt.Sprintf("DATASTREAM_KV_%d", 1101)
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  kvName,
		History: 1,
		TTL:     0,
	})
	require.NoError(t, err)

	// Simulate server publishing blocks and bookmarks
	var entryNum uint64 = 0
	for i := uint64(1); i <= 5; i++ {
		// Create bookmark for this block that will point to the L2Block entry
		bookmark := types.NewBookmarkProto(i, datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK)
		bookmarkBytes, err := bookmark.Marshal()
		require.NoError(t, err)

		// Store bookmark mapping block number to the entry number where the L2Block will be published
		// Use hex encoding for the key since bookmark bytes contain non-ASCII characters
		bookmarkKey := fmt.Sprintf("%x", bookmarkBytes)
		value := make([]byte, 8)
		binary.BigEndian.PutUint64(value, entryNum) // This will be the L2Block entry number
		_, err = kv.Put(ctx, bookmarkKey, value)
		require.NoError(t, err)
		// Don't increment entryNum here - it will be incremented after publishing the L2Block

		// Publish L2 block
		l2Block := &datastream.L2Block{
			Number:    i,
			Timestamp: uint64(time.Now().Unix()),
			Hash:      common.Hash{byte(i)}.Bytes(),
		}

		msgData, err := proto.Marshal(l2Block)
		require.NoError(t, err)

		headers := nats.Header{}
		headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2Block))
		headers.Set("EntryNumber", fmt.Sprintf("%d", entryNum))

		msg := &nats.Msg{
			Subject: fmt.Sprintf("datastream.1101.L2_BLOCK"),
			Data:    msgData,
			Header:  headers,
		}

		_, err = js.PublishMsg(ctx, msg)
		require.NoError(t, err)
		entryNum++

		// Publish L2 block end
		blockEnd := &datastream.L2BlockEnd{
			Number: i,
		}

		blockEndData, err := proto.Marshal(blockEnd)
		require.NoError(t, err)

		headers = nats.Header{}
		headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2BlockEnd))
		headers.Set("EntryNumber", fmt.Sprintf("%d", entryNum))

		msg = &nats.Msg{
			Subject: fmt.Sprintf("datastream.1101.L2_BLOCK_END"),
			Data:    blockEndData,
			Header:  headers,
		}

		_, err = js.PublishMsg(ctx, msg)
		require.NoError(t, err)
		entryNum++
	}

	// Now create a client and test lookups
	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
	err = client.Start()
	require.NoError(t, err)
	defer client.Stop()

	// Test retrieving existing blocks - should be fast due to bookmarks
	for i := uint64(1); i <= 5; i++ {
		start := time.Now()
		block, err := client.GetL2BlockByNumber(i)
		elapsed := time.Since(start)

		assert.NoError(t, err)
		assert.NotNil(t, block)
		assert.Equal(t, i, block.L2BlockNumber)
		assert.Less(t, elapsed, 100*time.Millisecond, "Block lookup should be fast with bookmarks")

		t.Logf("Retrieved block %d in %v", i, elapsed)
	}

	// Test non-existent block - should fail quickly
	start := time.Now()
	_, err = client.GetL2BlockByNumber(10)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Less(t, elapsed, 50*time.Millisecond, "Non-existent block should fail quickly")

	t.Logf("Non-existent block lookup failed in %v", elapsed)
}
