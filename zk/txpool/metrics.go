package txpool

import (
	"sort"
	"time"
)

const MetricsRunTime = 1 * time.Minute

type Metrics struct {
	txCounter    uint64
	lastPoolSize uint64
	// TxIn is the number of transactions that entered the pool in the last minute
	TxIn uint64
	// TxOut is the number of transactions that left the pool in the last minute
	TxOut uint64
	// MedianWaitTime is the median wait time in seconds for transactions in the pool over 1 minute
	MedianWaitTimeSeconds uint64
}

func (m *Metrics) IncrementCounter() {
	m.txCounter++
}

func (m *Metrics) ResetCounter() {
	m.txCounter = 0
}

func (m *Metrics) Update(pool *TxPool) {
	currentPoolSize := uint64(pool.pending.Len() + pool.baseFee.Len() + pool.queued.Len())

	out := m.lastPoolSize + m.txCounter - currentPoolSize
	if out < 0 {
		out = 0
	}

	var waitTimes []time.Duration
	pool.all.ascendAll(func(mt *metaTx) bool {
		created := time.Unix(int64(mt.created), 0)
		waitTimes = append(waitTimes, time.Since(created))
		return true
	})

	medianWaitTime := 0
	if len(waitTimes) > 0 {
		sort.Slice(waitTimes, func(i, j int) bool {
			return waitTimes[i] < waitTimes[j]
		})
		mid := len(waitTimes) / 2
		var median time.Duration
		if len(waitTimes)%2 == 0 {
			median = (waitTimes[mid-1] + waitTimes[mid]) / 2
		} else {
			median = waitTimes[mid]
		}
		medianWaitTime = int(median.Seconds())
		if medianWaitTime < 0 {
			medianWaitTime = 0
		}
	}

	m.lastPoolSize = currentPoolSize
	m.TxIn = m.txCounter
	m.TxOut = out
	m.MedianWaitTimeSeconds = uint64(medianWaitTime)

	m.ResetCounter()
}
