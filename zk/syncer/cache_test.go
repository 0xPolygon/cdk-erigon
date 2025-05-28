package syncer

import (
	"bytes"
	"encoding/binary"
	"testing"
)

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
