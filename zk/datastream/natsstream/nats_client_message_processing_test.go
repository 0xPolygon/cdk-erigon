package natsstream

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// mockJetStreamMsg is a simple mock implementation of jetstream.Msg
type mockJetStreamMsg struct {
	msg *nats.Msg
}

func (m *mockJetStreamMsg) Data() []byte         { return m.msg.Data }
func (m *mockJetStreamMsg) Headers() nats.Header { return m.msg.Header }
func (m *mockJetStreamMsg) Subject() string      { return m.msg.Subject }
func (m *mockJetStreamMsg) Reply() string        { return m.msg.Reply }

func (m *mockJetStreamMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *mockJetStreamMsg) Ack() error                                { return nil }
func (m *mockJetStreamMsg) Nak() error                                { return nil }
func (m *mockJetStreamMsg) InProgress() error                         { return nil }
func (m *mockJetStreamMsg) Term() error                               { return nil }
func (m *mockJetStreamMsg) TermWithReason(reason string) error        { return nil }
func (m *mockJetStreamMsg) DoubleAck(ctx context.Context) error       { return nil }
func (m *mockJetStreamMsg) NakWithDelay(delay time.Duration) error    { return nil }

// TestProcessBookmark tests the processBookmark function
func TestProcessBookmark(t *testing.T) {
	logger := log.New()
	ctx := context.Background()
	// Use minimal manager for function tests
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, logger)
	client := NewNATSClient(ctx, "nats://localhost", false, manager, logger)

	// Create a valid bookmark message
	bookmark := &datastream.BookMark{
		Type:  datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK,
		Value: 12345,
	}
	bookmarkData, err := proto.Marshal(bookmark)
	require.NoError(t, err)

	// Create a mock NATS message
	msg := &nats.Msg{
		Subject: "test.subject",
		Data:    bookmarkData,
		Header: nats.Header{
			"EntryType": []string{"176"}, // EtBookmark
		},
	}

	// Convert to jetstream.Msg interface using the mock implementation
	jsMock := &mockJetStreamMsg{msg: msg}

	// Test successful bookmark processing
	result, err := client.processBookmark(jsMock)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK, result.BookmarkType())
	assert.Equal(t, uint64(12345), result.BookMark.GetValue())
}

// TestProcessBookmark_InvalidData tests bookmark processing with invalid data
func TestProcessBookmark_InvalidData(t *testing.T) {
	logger := log.New()
	ctx := context.Background()
	// Use minimal manager for function tests
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, logger)
	client := NewNATSClient(ctx, "nats://localhost", false, manager, logger)

	// Create a mock NATS message with invalid data
	msg := &nats.Msg{
		Subject: "test.subject",
		Data:    []byte("invalid bookmark data"),
		Header: nats.Header{
			"EntryType": []string{"176"}, // EtBookmark
		},
	}

	jsMock := &mockJetStreamMsg{msg: msg}

	// Test error handling for invalid bookmark data
	result, err := client.processBookmark(jsMock)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "error unmarshaling bookmark")
}

// TestHandleStreamGerUpdate tests GER update processing by testing the underlying decode function
func TestHandleStreamGerUpdate(t *testing.T) {
	// Create a valid GER update message
	gerUpdate := &datastream.UpdateGER{
		BatchNumber:    1,
		Timestamp:      uint64(time.Now().Unix()),
		GlobalExitRoot: []byte("test_global_exit_root"),
	}
	gerData, err := proto.Marshal(gerUpdate)
	require.NoError(t, err)

	// Test the core decode functionality that handleStreamGerUpdate uses
	decoded, err := types.DecodeGerUpdateProto(gerData)
	require.NoError(t, err)
	assert.NotNil(t, decoded)
	assert.Equal(t, uint64(1), decoded.BatchNumber)
	expectedHash := "0x0000000000000000000000746573745f676c6f62616c5f657869745f726f6f74"
	assert.Equal(t, expectedHash, decoded.GlobalExitRoot.Hex())
}

// TestHandleStreamGerUpdate_InvalidData tests GER update processing with invalid data
func TestHandleStreamGerUpdate_InvalidData(t *testing.T) {
	// Test the core decode functionality with invalid data
	_, err := types.DecodeGerUpdateProto([]byte("invalid ger update data"))
	assert.Error(t, err)
	// The error should be related to protobuf unmarshaling
}

// TestHandleBatchEndMessage tests batch end message processing
func TestHandleBatchEndMessage(t *testing.T) {
	logger := log.New()
	ctx := context.Background()
	// Use minimal manager for function tests
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, logger)
	client := NewNATSClient(ctx, "nats://localhost", false, manager, logger)

	// Create a mock NATS message
	msg := &nats.Msg{
		Subject: "test.subject",
		Data:    []byte{}, // Empty data for batch end
		Header: nats.Header{
			"EntryType": []string{"5"}, // EtBatchEnd
		},
	}

	jsMock := &mockJetStreamMsg{msg: msg}

	// Create a mock block search state
	state := &blockSearchState{
		targetBlockNum: 100,
		phase:          0, // Using 0 for simplicity
	}

	// Test batch end message processing
	newState, fullBlock, tx, foundEnd, err := client.handleBatchEndMessage(jsMock, state)
	require.NoError(t, err)
	assert.NotNil(t, newState)
	assert.Nil(t, fullBlock)  // Should be nil for batch end messages
	assert.Nil(t, tx)         // Should be nil for batch end messages
	assert.False(t, foundEnd) // Should be false for batch end messages
}

// TestHandleBookmarkMessage tests the handleBookmarkMessage function
func TestHandleBookmarkMessage(t *testing.T) {
	logger := log.New()
	ctx := context.Background()
	// Use minimal manager for function tests
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, logger)
	client := NewNATSClient(ctx, "nats://localhost", false, manager, logger)

	// Create a valid bookmark message
	bookmark := &datastream.BookMark{
		Type:  datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK,
		Value: 54321,
	}
	bookmarkData, err := proto.Marshal(bookmark)
	require.NoError(t, err)

	// Create a mock NATS message
	msg := &nats.Msg{
		Subject: "test.subject",
		Data:    bookmarkData,
		Header: nats.Header{
			"EntryType": []string{"176"}, // EtBookmark
		},
	}

	jsMock := &mockJetStreamMsg{msg: msg}

	// Create a mock block search state
	state := &blockSearchState{
		targetBlockNum: 100,
		phase:          0, // Using 0 for simplicity
	}

	// Test successful bookmark message processing
	newState, fullBlock, tx, foundEnd, err := client.handleBookmarkMessage(jsMock, state)
	require.NoError(t, err)
	assert.NotNil(t, newState)
	assert.Nil(t, fullBlock)  // Should be nil for bookmark messages
	assert.Nil(t, tx)         // Should be nil for bookmark messages
	assert.False(t, foundEnd) // Should be false for bookmark messages
}

// TestHandleBookmarkMessage_InvalidData tests bookmark message processing with invalid data
func TestHandleBookmarkMessage_InvalidData(t *testing.T) {
	logger := log.New()
	ctx := context.Background()
	// Use minimal manager for function tests
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, logger)
	client := NewNATSClient(ctx, "nats://localhost", false, manager, logger)

	// Create a mock NATS message with invalid data
	msg := &nats.Msg{
		Subject: "test.subject",
		Data:    []byte("invalid bookmark data"),
		Header: nats.Header{
			"EntryType": []string{"176"}, // EtBookmark
		},
	}

	jsMock := &mockJetStreamMsg{msg: msg}

	// Create a mock block search state
	state := &blockSearchState{
		targetBlockNum: 100,
		phase:          0, // Using 0 for simplicity
	}

	// Test error handling for invalid bookmark data
	newState, fullBlock, tx, foundEnd, err := client.handleBookmarkMessage(jsMock, state)
	assert.Error(t, err)
	// Note: function returns the original state on error, not nil
	assert.NotNil(t, newState)
	assert.Nil(t, fullBlock)
	assert.Nil(t, tx)
	assert.False(t, foundEnd)
	assert.Contains(t, err.Error(), "error unmarshaling bookmark")
}

// TestCreateConsumerConfig tests the createConsumerConfig function
func TestCreateConsumerConfig(t *testing.T) {
	logger := log.New()
	ctx := context.Background()
	// Use minimal manager for function tests
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, logger)
	client := NewNATSClient(ctx, "nats://localhost", false, manager, logger)

	client.Start()
	defer client.Stop()

	// Test successful config creation
	consumerConfig, err := client.createConsumerConfig()
	require.NoError(t, err)
	assert.NotEmpty(t, consumerConfig.Durable)
	assert.Contains(t, consumerConfig.Durable, "DATASTREAM_CONSUMER_")
	assert.Equal(t, jetstream.AckExplicitPolicy, consumerConfig.AckPolicy)
	assert.Equal(t, jetstream.DeliverAllPolicy, consumerConfig.DeliverPolicy) // For progress=0
}

// TestHandleStreamBatchEnd tests the handleStreamBatchEnd function
func TestHandleStreamBatchEnd(t *testing.T) {
	// Create a valid batch end message
	batchEnd := &datastream.BatchEnd{
		Number: 42,
	}
	batchData, err := proto.Marshal(batchEnd)
	require.NoError(t, err)

	// Test the core decode functionality that handleStreamBatchEnd uses
	decoded, err := types.UnmarshalBatchEnd(batchData)
	require.NoError(t, err)
	assert.NotNil(t, decoded)
	assert.Equal(t, uint64(42), decoded.Number)
}

// TestHandleStreamBatchEnd_InvalidData tests batch end processing with invalid data
func TestHandleStreamBatchEnd_InvalidData(t *testing.T) {
	// Test the core decode functionality with invalid data
	_, err := types.UnmarshalBatchEnd([]byte("invalid batch end data"))
	assert.Error(t, err)
	// The error should be related to protobuf unmarshaling
}

// TestIsFatalError tests the isFatalError function with various error types
func TestIsFatalError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "transaction outside block error",
			err:      assert.AnError, // Placeholder - will be overridden
			expected: true,
		},
		{
			name:     "block without end error",
			err:      assert.AnError, // Placeholder - will be overridden
			expected: true,
		},
		{
			name:     "block end mismatch error",
			err:      assert.AnError, // Placeholder - will be overridden
			expected: true,
		},
		{
			name:     "missing header error",
			err:      assert.AnError, // Placeholder - will be overridden
			expected: true,
		},
		{
			name:     "non-fatal error",
			err:      assert.AnError, // Regular error
			expected: false,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up specific error messages for fatal cases
			switch i {
			case 1:
				tt.err = assert.AnError
				// Override with specific error message
				err := fmt.Errorf("unexpected L2 tx entry, found outside of block")
				result := isFatalError(err)
				assert.True(t, result)
				return
			case 2:
				err := fmt.Errorf("received new L2 block 3 without proper block end for previous block 2")
				result := isFatalError(err)
				assert.True(t, result)
				return
			case 3:
				err := fmt.Errorf("block end number doesn't match block number")
				result := isFatalError(err)
				assert.True(t, result)
				return
			case 4:
				err := fmt.Errorf("message missing EntryType header")
				result := isFatalError(err)
				assert.True(t, result)
				return
			}

			result := isFatalError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
