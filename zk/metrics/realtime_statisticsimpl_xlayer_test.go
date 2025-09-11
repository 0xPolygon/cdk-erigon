package metrics

import (
	"sync"
	"testing"
	"time"
)

func TestRealtimeStatisticsInstanceSummary(t *testing.T) {
	type fields struct {
		timestamp          time.Time
		blockHeight        uint64
		pendingBlockHeight uint64
		statistics         map[LogTag]int64
		tags               map[LogTag]string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{"1", fields{
			timestamp:          time.Now().Add(-time.Second),
			blockHeight:        12345,
			pendingBlockHeight: 12350,
			statistics:         map[LogTag]int64{},
			tags:               map[LogTag]string{},
		}, "test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get a fresh instance for testing
			realtimeInstance = nil
			realtimeOnce = sync.Once{}
			stats := GetRealtimeStatistics()
			realtimeStats := stats.(*realtimeStatisticsInstance)

			// Set up the test data
			realtimeStats.mu.Lock()
			realtimeStats.lastUpdateTime = tt.fields.timestamp
			realtimeStats.blockHeight = tt.fields.blockHeight
			realtimeStats.pendingBlockHeight = tt.fields.pendingBlockHeight
			realtimeStats.statistics = tt.fields.statistics
			realtimeStats.tags = tt.fields.tags
			realtimeStats.mu.Unlock()

			t.Log(realtimeStats.SummaryCheckpoint())
			t.Log(realtimeStats.Summary())
		})
	}
}
