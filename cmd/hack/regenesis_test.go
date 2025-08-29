package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestGetStorageDiff(t *testing.T) {
	tests := []struct {
		name        string
		preStorage  map[string]string
		postStorage map[string]string
		expected    map[string]string
	}{
		{
			name:        "empty storages",
			preStorage:  map[string]string{},
			postStorage: map[string]string{},
			expected:    map[string]string{},
		},
		{
			name: "no changes",
			preStorage: map[string]string{
				"0x00": "0x01",
				"0x01": "0x02",
			},
			postStorage: map[string]string{
				"0x00": "0x01",
				"0x01": "0x02",
			},
			expected: map[string]string{},
		},
		{
			name: "new storage keys added",
			preStorage: map[string]string{
				"0x00": "0x01",
			},
			postStorage: map[string]string{
				"0x00": "0x01",
				"0x01": "0x02",
				"0x02": "0x03",
			},
			expected: map[string]string{
				"0x01": "0x02",
				"0x02": "0x03",
			},
		},
		{
			name: "storage values modified",
			preStorage: map[string]string{
				"0x00": "0x01",
				"0x01": "0x02",
			},
			postStorage: map[string]string{
				"0x00": "0x01",
				"0x01": "0x03", // changed from 0x02 to 0x03
			},
			expected: map[string]string{
				"0x01": "0x03",
			},
		},
		{
			name: "storage keys deleted",
			preStorage: map[string]string{
				"0x00": "0x01",
				"0x01": "0x02",
				"0x02": "0x03",
			},
			postStorage: map[string]string{
				"0x00": "0x01",
				// 0x01 and 0x02 are deleted
			},
			expected: map[string]string{
				"0x01": "0x",
				"0x02": "0x",
			},
		},
		{
			name: "mixed changes - additions, modifications, and deletions",
			preStorage: map[string]string{
				"0x00": "0x01",
				"0x01": "0x02",
				"0x02": "0x03",
			},
			postStorage: map[string]string{
				"0x00": "0x01", // unchanged
				"0x01": "0x04", // modified from 0x02 to 0x04
				"0x03": "0x05", // new key
			},
			expected: map[string]string{
				"0x01": "0x04", // modified value
				"0x02": "0x",   // deleted key
				"0x03": "0x05", // new key
			},
		},
		{
			name:       "nil preStorage",
			preStorage: nil,
			postStorage: map[string]string{
				"0x00": "0x01",
				"0x01": "0x02",
			},
			expected: map[string]string{
				"0x00": "0x01",
				"0x01": "0x02",
			},
		},
		{
			name: "nil postStorage",
			preStorage: map[string]string{
				"0x00": "0x01",
				"0x01": "0x02",
			},
			postStorage: nil,
			expected: map[string]string{
				"0x00": "0x",
				"0x01": "0x",
			},
		},
		{
			name:        "both nil storages",
			preStorage:  nil,
			postStorage: nil,
			expected:    map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetStorageDiff(tt.preStorage, tt.postStorage)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("GetStorageDiff() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestGetStorageDiffPerformance tests performance with large maps
func TestGetStorageDiffPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	// Create large storage maps
	size := 10000
	preStorage := make(map[string]string, size)
	postStorage := make(map[string]string, size)

	for i := 0; i < size; i++ {
		key := fmt.Sprintf("0x%04x", i)
		value := fmt.Sprintf("0x%04x", i)
		preStorage[key] = value

		// Modify some values
		if i%2 == 0 {
			postStorage[key] = value
		} else {
			postStorage[key] = fmt.Sprintf("0x%04x", i+1000)
		}
	}

	// Add some new keys to post storage
	for i := size; i < size+1000; i++ {
		key := fmt.Sprintf("0x%04x", i)
		value := fmt.Sprintf("0x%04x", i)
		postStorage[key] = value
	}

	// Benchmark the function
	b := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			GetStorageDiff(preStorage, postStorage)
		}
	})

	t.Logf("Performance test completed: %s", b.String())
}
