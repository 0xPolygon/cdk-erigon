package metrics

import (
	"fmt"
	"sync"
	"time"

	"github.com/ledgerwatch/log/v3"
)

var realtimeInstance *realtimeStatisticsInstance
var realtimeOnce sync.Once

func GetRealtimeStatistics() Statistics {
	realtimeOnce.Do(func() {
		realtimeInstance = &realtimeStatisticsInstance{}
		realtimeInstance.resetStatistics()
	})
	return realtimeInstance
}

type realtimeStatisticsInstance struct {
	mu                 sync.RWMutex
	lastUpdateTime     time.Time
	blockHeight        uint64
	pendingBlockHeight uint64
	statistics         map[LogTag]int64
	tags               map[LogTag]string
}

// GetLastUpdateTime returns the last update time
func (r *realtimeStatisticsInstance) GetLastUpdateTime() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastUpdateTime
}

// Statistics interface methods (required by Statistics interface)
func (r *realtimeStatisticsInstance) CumulativeCounting(tag LogTag) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statistics[tag]++
}

func (r *realtimeStatisticsInstance) CumulativeValue(tag LogTag, value int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statistics[tag] += value
}

func (r *realtimeStatisticsInstance) CumulativeTiming(tag LogTag, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statistics[tag] += duration.Milliseconds()
}

func (r *realtimeStatisticsInstance) CumulativeMicroTiming(tag LogTag, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statistics[tag] += duration.Microseconds()
}

func (r *realtimeStatisticsInstance) SetTag(tag LogTag, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tags[tag] = value
}

func (r *realtimeStatisticsInstance) GetTag(tag LogTag) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tags[tag]
}

func (r *realtimeStatisticsInstance) GetStatistics(tag LogTag) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.statistics[tag]
}

func (r *realtimeStatisticsInstance) resetStatistics() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastUpdateTime = time.Now()
	r.blockHeight = 0
	r.pendingBlockHeight = 0
	r.statistics = make(map[LogTag]int64)
	r.tags = make(map[LogTag]string)
}

// SummaryCheckpoint returns a checkpoint summary and resets counters
func (r *realtimeStatisticsInstance) SummaryCheckpoint() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := fmt.Sprintf("RealtimeCheckpoint{ "+
		"BlockHeight<%d>, PendingHeight<%d>}",
		r.blockHeight, r.pendingBlockHeight)

	log.Info(result)

	return result
}

func (r *realtimeStatisticsInstance) Summary() string {
	r.mu.RLock()

	timeSinceUpdate := time.Since(r.lastUpdateTime)

	result := fmt.Sprintf("RealtimeStats{ "+
		"BlockHeight<%d>, PendingHeight<%d>, "+
		"LastUpdate<%v>, TimeSinceUpdate<%v> }",
		r.blockHeight, r.pendingBlockHeight,
		r.lastUpdateTime.Format(time.RFC3339), timeSinceUpdate)

	log.Info(result)

	SetRealtimeBlockHeight(float64(r.blockHeight))
	SetRealtimePendingBlockHeight(float64(r.pendingBlockHeight))

	r.mu.RUnlock()

	r.resetStatistics()
	return result
}
