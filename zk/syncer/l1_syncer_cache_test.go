package syncer

import (
	"bytes"
	"context"
	"encoding/binary"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/kv/memdb"
	"github.com/erigontech/erigon/core/types"
	"github.com/stretchr/testify/assert"
	"math/big"
	"testing"
	"time"
)

const mockBlockNumberInt = 21

func TestL1Cache(t *testing.T) {
	ctx := context.Background()
	l1CacheDb := memdb.NewTestDB(t)
	l1CacheSyncer, err := NewL1SyncerCache(ctx, l1CacheDb)
	assert.NoError(t, err)
	defer l1CacheSyncer.Close()

	mockBlockParentHash := common.HexToHash("0x123456789")
	mockBlockTime := uint64(time.Now().Unix())
	mockBlockNumber := big.NewInt(mockBlockNumberInt)
	mockBlockHeader := &types.Header{ParentHash: mockBlockParentHash, Number: mockBlockNumber, Time: mockBlockTime}

	mockL1ContractAddresses := []common.Address{
		common.HexToAddress("0x1"),
		common.HexToAddress("0x2"),
		common.HexToAddress("0x3"),
	}
	mockL1ContractTopics := [][]common.Hash{
		[]common.Hash{common.HexToHash("0x1")},
		[]common.Hash{common.HexToHash("0x2")},
		[]common.Hash{common.HexToHash("0x3")},
	}

	mockMainnetExitRoot := common.HexToHash("0x111")
	mockRollupExitRoot := common.HexToHash("0x222")

	// writeL1BlockHeaderCache
	err = l1CacheSyncer.writeL1BlockHeaderCache(mockBlockHeader)
	assert.NoError(t, err)

	// getL1BlockHeaderCache
	resultHeader, err := l1CacheSyncer.getL1BlockHeaderCache(mockBlockNumberInt)

	assert.NoError(t, err)

	assert.Equal(t, mockBlockHeader.ParentHash, resultHeader.ParentHash)
	assert.Equal(t, mockBlockHeader.Number, resultHeader.Number)
	assert.Equal(t, mockBlockHeader.Time, resultHeader.Time)

	l1InfoTreeLogs := []types.Log{
		{
			BlockNumber: mockBlockNumber.Uint64(),
			Index:       0,
			Address:     mockL1ContractAddresses[0],
			Topics:      []common.Hash{mockL1ContractTopics[0][0], mockMainnetExitRoot, mockRollupExitRoot},
		},
		{
			BlockNumber: mockBlockNumber.Uint64(),
			Index:       1,
			Address:     mockL1ContractAddresses[1],
			Topics:      []common.Hash{mockL1ContractTopics[1][0], mockMainnetExitRoot, mockRollupExitRoot},
		},
		{
			BlockNumber: mockBlockNumber.Uint64(),
			Index:       2,
			Address:     mockL1ContractAddresses[2],
			Topics:      []common.Hash{mockL1ContractTopics[2][0], mockMainnetExitRoot, mockRollupExitRoot},
		},
	}

	// writeL1TreeLogs
	for index := range l1InfoTreeLogs {
		err = l1CacheSyncer.writeL1TreeLogs(&l1InfoTreeLogs[index])
		assert.NoError(t, err)
	}

	// getLastL1TreeLogBlockNumber
	lastTreeLogBlockNumber, err := l1CacheSyncer.getLastL1TreeLogBlockNumber()
	assert.NoError(t, err)
	assert.Equal(t, mockBlockNumber.Uint64(), lastTreeLogBlockNumber)

	logsCh := make(chan types.Log)
	go l1CacheSyncer.getL1TreeLogs(0, logsCh)
	index := 0
	for logEntry := range logsCh {
		assert.Equal(t, l1InfoTreeLogs[index].BlockNumber, logEntry.BlockNumber)
		assert.Equal(t, l1InfoTreeLogs[index].Index, logEntry.Index)
		assert.Equal(t, l1InfoTreeLogs[index].Address, logEntry.Address)
		assert.Equal(t, l1InfoTreeLogs[index].Topics, logEntry.Topics)
		index++
	}

	assert.Equal(t, len(l1InfoTreeLogs), index)

	// getL1TreeLogs
	err = l1CacheSyncer.clearTreeLogs()
	assert.NoError(t, err)
}

func TestDecodeL1LogKey(t *testing.T) {
	tests := []struct {
		name                string
		data                []byte
		expectedBlockNumber uint64
		expectedLogIndex    uint
		expectError         bool
	}{
		{
			name: "valid data",
			data: func() []byte {
				buf := make([]byte, 12)
				binary.BigEndian.PutUint64(buf[:8], 123456789)
				binary.BigEndian.PutUint32(buf[8:12], 99)
				return buf
			}(),
			expectedBlockNumber: 123456789,
			expectedLogIndex:    99,
			expectError:         false,
		},
		{
			name:                "zero data",
			data:                make([]byte, 12),
			expectedBlockNumber: 0,
			expectedLogIndex:    0,
			expectError:         false,
		},
		{
			name:                "incorrect data length (too short)",
			data:                make([]byte, 10),
			expectedBlockNumber: 0,
			expectedLogIndex:    0,
			expectError:         true,
		},
		{
			name:                "incorrect data length (too long)",
			data:                make([]byte, 20),
			expectedBlockNumber: 0,
			expectedLogIndex:    0,
			expectError:         true,
		},
		{
			name: "max values",
			data: func() []byte {
				buf := make([]byte, 12)
				binary.BigEndian.PutUint64(buf[:8], 18446744073709551615) // Max uint64
				binary.BigEndian.PutUint32(buf[8:12], 4294967295)         // Max uint32
				return buf
			}(),
			expectedBlockNumber: 18446744073709551615,
			expectedLogIndex:    4294967295,
			expectError:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockNumber, logIndex, err := decodeL1LogKey(tt.data)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if blockNumber != tt.expectedBlockNumber {
					t.Errorf("unexpected block number: got %v, want %v", blockNumber, tt.expectedBlockNumber)
				}
				if logIndex != tt.expectedLogIndex {
					t.Errorf("unexpected log index: got %v, want %v", logIndex, tt.expectedLogIndex)
				}
			}
		})
	}
}

func TestEncodeL1LogKey(t *testing.T) {
	tests := []struct {
		name        string
		blockNumber uint64
		logIndex    uint
		expected    []byte
	}{
		{
			name:        "zero values",
			blockNumber: 0,
			logIndex:    0,
			expected:    make([]byte, 12),
		},
		{
			name:        "valid small values",
			blockNumber: 1,
			logIndex:    1,
			expected: func() []byte {
				buf := make([]byte, 12)
				binary.BigEndian.PutUint64(buf[:8], 1)
				binary.BigEndian.PutUint32(buf[8:], 1)
				return buf
			}(),
		},
		{
			name:        "large block number and small log index",
			blockNumber: 123456789,
			logIndex:    1,
			expected: func() []byte {
				buf := make([]byte, 12)
				binary.BigEndian.PutUint64(buf[:8], 123456789)
				binary.BigEndian.PutUint32(buf[8:], 1)
				return buf
			}(),
		},
		{
			name:        "small block number and large log index",
			blockNumber: 1,
			logIndex:    4294967295, // Max uint32 value
			expected: func() []byte {
				buf := make([]byte, 12)
				binary.BigEndian.PutUint64(buf[:8], 1)
				binary.BigEndian.PutUint32(buf[8:], 4294967295)
				return buf
			}(),
		},
		{
			name:        "max values",
			blockNumber: 18446744073709551615, // Max uint64 value
			logIndex:    4294967295,           // Max uint32 value
			expected: func() []byte {
				buf := make([]byte, 12)
				binary.BigEndian.PutUint64(buf[:8], 18446744073709551615)
				binary.BigEndian.PutUint32(buf[8:], 4294967295)
				return buf
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodeL1LogKey(tt.blockNumber, tt.logIndex)
			if !bytes.Equal(result, tt.expected) {
				t.Errorf("unexpected result: got %v, want %v", result, tt.expected)
			}
		})
	}
}
