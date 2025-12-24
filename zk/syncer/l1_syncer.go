package syncer

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v4"
	ethereum "github.com/erigontech/erigon"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/iden3/go-iden3-crypto/keccak256"
	"golang.org/x/sync/singleflight"

	"encoding/binary"

	ethTypes "github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/rpc"
	"github.com/erigontech/erigon/zk/ethermanpool"
)

const (
	batchWorkers      = 4
	logsChannelSize   = 100
	filterLogsTimeout = 60 * time.Second
	// max workers used when halving and querying subranges
	subrangeWorkers    = 4
	headerFetchTimeout = 20 * time.Second
	maxBatch           = 100
)

// min number of blocks (span = To-From) we will query without further splitting
var MinFilterBlockSpan uint64 = 100
var GetLogsBackoffMultiplier = 1.5
var GetHeaderBackoffMultiplier = 1.0
var DefaultBackoffSleep = 1 * time.Second

var errorShortResponseLT32 = fmt.Errorf("response too short to contain hash data")
var errorShortResponseLT96 = fmt.Errorf("response too short to contain last batch number data")

const (
	rollupSequencedBatchesSignature = "0x25280169" // hardcoded abi signature
	globalExitRootManager           = "0xd02103ca"
	rollupManager                   = "0x49b7b802"
	admin                           = "0xf851a440"
	trustedSequencer                = "0xcfa8ed47"
	sequencedBatchesMapSignature    = "0xb4d63f58"
)

//go:generate mockgen -typed=true -destination=./mocks/etherman_mock.go -package=mocks github.com/erigontech/erigon/zk/ethermanpool IEtherman

type fetchJob struct {
	From uint64
	To   uint64
}

type jobResult struct {
	Size  uint64
	Error error
	Logs  []ethTypes.Log
}

type LogEvent struct {
	Logs     []ethTypes.Log
	Progress uint64 // set only on Done
	Done     bool
}

type LogsRetrieveMode int

const (
	LogsModeImmediate LogsRetrieveMode = iota
	LogsModeOnCompletion
)

type L1Syncer struct {
	ctx                 context.Context
	etherman            ethermanpool.IEtherman
	l1ContractAddresses []common.Address
	topics              [][]common.Hash
	blockRange          uint64
	queryDelay          uint64

	firstL1Block  uint64
	latestL1Block uint64

	// atomic
	isSyncStarted      atomic.Bool
	isDownloading      atomic.Bool
	lastCheckedL1Block atomic.Uint64
	wgRunLoopDone      sync.WaitGroup
	flagStop           atomic.Bool

	// Channels
	logsConsumChan   chan LogEvent
	logsProduceChan  chan LogEvent
	logsSwapMutex    sync.Mutex
	logsChanProgress chan string
	closeLogsChannel atomic.Bool

	highestBlockType string                                   // finalized, latest, safe
	headersCache     *expirable.LRU[uint64, *ethTypes.Header] // cache for headers
	sfGroup          singleflight.Group
	fetchHeaders     bool
}

func NewL1Syncer(ctx context.Context, etherman ethermanpool.IEtherman, l1ContractAddresses []common.Address, topics [][]common.Hash, blockRange, queryDelay uint64, highestBlockType string, firstL1Block uint64) *L1Syncer {
	headersCache := expirable.NewLRU[uint64, *ethTypes.Header](int(blockRange), nil, time.Minute*10)
	return &L1Syncer{
		ctx:                 ctx,
		etherman:            etherman,
		l1ContractAddresses: l1ContractAddresses,
		topics:              topics,
		blockRange:          blockRange,
		queryDelay:          queryDelay,
		logsProduceChan:     make(chan LogEvent, logsChannelSize),
		logsChanProgress:    make(chan string, logsChannelSize),
		highestBlockType:    highestBlockType,
		firstL1Block:        firstL1Block,
		headersCache:        headersCache,
	}
}

// clientCount returns the number of underlying clients if available.
// Falls back to 1 for single-client implementations.
func (s *L1Syncer) clientCount() int {
	if multi, ok := s.etherman.(ethermanpool.IMultiEtherman); ok {
		return multi.ClientCount()
	}
	return 1
}

func (s *L1Syncer) IsSyncStarted() bool {
	return s.isSyncStarted.Load()
}

func (s *L1Syncer) GetLastCheckedL1Block() uint64 {
	return s.lastCheckedL1Block.Load()
}

func (s *L1Syncer) StopQueryBlocks() {
	s.flagStop.Store(true)
}

func (s *L1Syncer) ConsumeQueryBlocks() {
	for {
		select {
		case <-s.logsProduceChan:
		case <-s.logsChanProgress:
		default:
			if !s.isSyncStarted.Load() {
				return
			}
			time.Sleep(time.Second)
		}
	}
}

func (s *L1Syncer) WaitQueryBlocksToFinish() {
	s.wgRunLoopDone.Wait()
}

// Channels
func (s *L1Syncer) GetLogsChan(mode LogsRetrieveMode) <-chan LogEvent {
	s.logsSwapMutex.Lock()
	defer s.logsSwapMutex.Unlock()

	s.logsConsumChan = s.logsProduceChan

	// If not downloading, grab existing data and close the channel
	if !s.isDownloading.Load() && mode == LogsModeImmediate {
		s.closeLogsChannel.Store(true)
	}
	return s.logsConsumChan
}

func (s *L1Syncer) GetProgressMessageChan() <-chan string {
	return s.logsChanProgress
}

func (s *L1Syncer) RunQueryBlocks(from uint64) {
	//if already started, don't start another thread
	if !s.isSyncStarted.CompareAndSwap(false, true) {
		return
	}

	// set it to true to catch the first cycle run case where the check can pass before the latest block is checked
	s.lastCheckedL1Block.Store(from)

	s.wgRunLoopDone.Add(1)
	s.flagStop.Store(false)

	//start a thread to check for new l1 block in interval
	go func() {
		defer s.isSyncStarted.Store(false)
		defer s.wgRunLoopDone.Done()

		log.Info("Starting L1 syncer thread")
		defer log.Info("Stopping L1 syncer thread")

		ticker := time.NewTicker(time.Duration(s.queryDelay) * time.Millisecond)
		defer ticker.Stop()

		sleepTicker := time.NewTicker(10 * time.Millisecond)
		defer sleepTicker.Stop()

		for {
			if s.flagStop.Load() {
				s.resetChannels()
				return
			}

			select {
			case <-s.ctx.Done():
				s.resetChannels()
				return
			case <-ticker.C:
				latestL1Block, err := s.getLatestL1Block()
				if err != nil {
					log.Error("Error getting latest L1 block", "err", err)
				} else {
					if latestL1Block > s.lastCheckedL1Block.Load() {
						// It should not be checked again in the new cycle, so +1 is added here.
						// Fixed receiving duplicate log events.
						// lastCheckedL1Block means that it has already been checked in the previous cycle.
						startBlock := s.lastCheckedL1Block.Load() + 1
						endBlock := latestL1Block
						if _, err = s.queryBlocks(startBlock, endBlock); err != nil {
							log.Error("Error querying blocks", "err", err)
						} else {
							s.lastCheckedL1Block.Store(latestL1Block)
						}
					}
				}
				s.resetChannels()
			case <-sleepTicker.C:
				if s.closeLogsChannel.Load() && !s.isDownloading.Load() {
					s.resetChannels()
				}
			}
		}
	}()
}

func (s *L1Syncer) GetHeader(number uint64) (*ethTypes.Header, error) {
	if header, ok := s.headersCache.Get(number); ok {
		return header, nil
	}

	// Deduplicate concurrent requests
	v, err, _ := s.sfGroup.Do(fmt.Sprintf("header-%d", number), func() (any, error) {
		// Add a timeout to avoid hanging indefinitely on slow/backed-up L1
		ctx, cancel := context.WithTimeout(s.ctx, headerFetchTimeout)
		defer cancel()
		header, err := s.etherman.HeaderByNumber(ctx, new(big.Int).SetUint64(number))
		if err != nil {
			return nil, err
		}
		s.headersCache.Add(number, header)
		return header, nil
	})

	if err != nil {
		return nil, err
	}
	return v.(*ethTypes.Header), nil
}

func (s *L1Syncer) GetBlock(number uint64) (*ethTypes.Block, error) {
	return s.etherman.BlockByNumber(s.ctx, new(big.Int).SetUint64(number))
}

func (s *L1Syncer) GetTransaction(hash common.Hash) (ethTypes.Transaction, bool, error) {
	return s.etherman.TransactionByHash(s.ctx, hash)
}

func (s *L1Syncer) GetPreElderberryAccInputHash(ctx context.Context, addr *common.Address, batchNum uint64) (common.Hash, error) {
	h, err := s.callSequencedBatchesMap(ctx, addr, batchNum)
	if err != nil {
		return common.Hash{}, err
	}

	return h, nil
}

// returns accInputHash only if the batch matches the last batch in sequence
// on Etrrof the rollup contract was changed so data is taken differently
func (s *L1Syncer) GetElderberryAccInputHash(ctx context.Context, addr *common.Address, rollupId, batchNum uint64) (common.Hash, error) {
	h, _, err := s.callGetRollupSequencedBatches(ctx, addr, rollupId, batchNum)
	if err != nil {
		return common.Hash{}, err
	}

	return h, nil
}

func (s *L1Syncer) GetL1BlockTimeStampByTxHash(ctx context.Context, txHash common.Hash) (uint64, error) {
	r, err := s.etherman.TransactionReceipt(ctx, txHash)
	if err != nil {
		return 0, err
	}

	header, err := s.GetHeader(r.BlockNumber.Uint64())
	if err != nil {
		return 0, err
	}

	return header.Time, nil
}

func (s *L1Syncer) l1QueryHeaders(logs []ethTypes.Log) error {
	// Build deduped slice of block numbers missing from cache
	seen := make(map[uint64]struct{})
	bns := make([]uint64, 0, len(logs))
	for _, l := range logs {
		bn := l.BlockNumber
		if _, ok := s.headersCache.Get(bn); ok {
			continue
		}
		if _, ok := seen[bn]; ok {
			continue
		}
		seen[bn] = struct{}{}
		bns = append(bns, bn)
	}

	if len(bns) == 0 {
		return nil
	}

	// Try batch path; returns remaining block numbers that still need per-header fetching
	remaining, err := s.l1QueryHeadersBatch(bns)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return nil
	}

	// Fallback: per-header concurrent fetch with retries
	logQueue := make(chan uint64, len(remaining))
	defer close(logQueue)
	for _, bn := range remaining {
		logQueue <- bn
	}

	var wg sync.WaitGroup
	wg.Add(len(remaining))

	backoffStrategy := backoff.NewExponentialBackOff()
	backoffStrategy.Multiplier = GetHeaderBackoffMultiplier // Just need randomization
	backoffStrategy.MaxInterval = 20 * time.Second

	process := func() {
		for {
			bn, ok := <-logQueue
			if !ok {
				break
			}

			if s.flagStop.Load() {
				wg.Done()
				continue
			}
			_, err := s.GetHeader(bn)
			if err != nil {
				time.Sleep(backoffStrategy.NextBackOff())
				logQueue <- bn
				continue
			}
			wg.Done()
		}
	}

	// launch the workers - multiply by client count for better parallelism
	workers := max(s.clientCount(), batchWorkers)
	for i := 0; i < workers; i++ {
		go process()
	}

	wg.Wait()

	return nil
}

func (s *L1Syncer) l1QueryHeadersBatch(bns []uint64) ([]uint64, error) {
	if len(bns) == 0 {
		return nil, nil
	}

	// Check if etherman implements HeaderBatcher for batch retrieval
	batcher, ok := s.etherman.(ethermanpool.HeaderBatcher)
	if !ok {
		// Fallback: no batch support, return all as missing
		return bns, nil
	}

	// Build chunked jobs of up to maxBatch
	type chunkJob struct{ nums []uint64 }
	jobs := make(chan chunkJob, (len(bns)/maxBatch)+1)

	for i := 0; i < len(bns); i += maxBatch {
		end := i + maxBatch
		if end > len(bns) {
			end = len(bns)
		}
		jobs <- chunkJob{nums: bns[i:end]}
	}
	close(jobs)

	// We will signal abort if any worker's backoff reaches Stop (e.g., provider ratelimits batches endlessly)
	abortCh := make(chan struct{})
	var abortOnce sync.Once

	// Worker function processing chunked jobs
	worker := func() {
		// Backoff strategy for 429/ratelimit on this worker
		bo := backoff.NewExponentialBackOff()
		bo.Multiplier = GetLogsBackoffMultiplier
		bo.MaxInterval = 10 * time.Second
		bo.MaxElapsedTime = 30 * time.Second

		for {
			select {
			case <-s.ctx.Done():
				return
			case <-abortCh:
				return
			case cj, ok := <-jobs:
				if !ok {
					return
				}
				// If we were asked to stop externally, just drop work
				if s.flagStop.Load() {
					continue
				}

				// Convert to big.Ints per call
				req := make([]*big.Int, len(cj.nums))
				for i, bn := range cj.nums {
					req[i] = new(big.Int).SetUint64(bn)
				}

				// Try to fetch; on 429-like errors, backoff and retry the same job
				for {
					// Quit promptly if another worker already aborted
					select {
					case <-abortCh:
						return
					default:
					}

					ctx, cancel := context.WithTimeout(s.ctx, headerFetchTimeout)
					heads, err := batcher.HeadersByNumbers(ctx, req)
					cancel()

					if err != nil {
						// Check for 429 rate limit errors
						if ethermanpool.Is429Error(err) {
							// backoff for 429; abort globally if we've reached Stop
							d := bo.NextBackOff()
							if d == backoff.Stop {
								log.Info("Batch header fetch aborting due to repeated rate limits")
								abortOnce.Do(func() { close(abortCh) })
								return
							}
							// Sleep, but bail out early on abort
							select {
							case <-time.After(d):
							case <-abortCh:
								return
							}
							// retry same job
							continue
						}
						// For non-429 errors, just give up this chunk; missing will be filled per-header
						bo.Reset()
						break
					}

					// Success path: cache headers we received
					if len(heads) != len(req) {
						log.Debug("Batch header fetch length mismatch", "want", len(req), "got", len(heads))
					}
					for i := range req {
						if i < len(heads) && heads[i] != nil {
							s.headersCache.Add(cj.nums[i], heads[i])
						}
					}
					// Success resets backoff and marks job done
					bo.Reset()
					break
				}
			}
		}
	}

	// Start workers - multiply by client count for better parallelism
	var wg sync.WaitGroup
	workers := max(s.clientCount(), batchWorkers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); worker() }()
	}

	// Wait for workers to drain jobs or abort
	wg.Wait()

	// After worker loop, compute which headers are still missing and return them
	missing := make([]uint64, 0)
	for _, bn := range bns {
		if _, ok := s.headersCache.Get(bn); !ok {
			missing = append(missing, bn)
		}
	}
	return missing, nil
}

func (s *L1Syncer) getLatestL1Block() (uint64, error) {
	var blockNumber *big.Int

	switch s.highestBlockType {
	case "finalized":
		blockNumber = big.NewInt(rpc.FinalizedBlockNumber.Int64())
	case "safe":
		blockNumber = big.NewInt(rpc.SafeBlockNumber.Int64())
	case "latest":
		blockNumber = nil
	}

	latestBlock, err := s.etherman.BlockByNumber(s.ctx, blockNumber)
	if err != nil {
		log.Error("L1 syncer: BlockByNumber failed", "err", err, "blockType", s.highestBlockType)
		return 0, err
	}

	latest := latestBlock.NumberU64()
	s.latestL1Block = latest

	return latest, nil
}

func (s *L1Syncer) queryBlocks(startBlock, endBlock uint64) (numLogs uint64, err error) {
	log.Debug("GetHighestSequence", "startBlock", startBlock, "latestBlock", endBlock)

	s.isDownloading.Store(true)
	defer s.isDownloading.Store(false)

	s.headersCache.Resize(int(endBlock) - int(startBlock) + 1)

	// define the blocks we're going to fetch up front
	fetches := make([]fetchJob, 0, (endBlock-startBlock)/s.blockRange+1)
	low := startBlock
	for {
		high := low + s.blockRange
		if high > endBlock {
			// at the end of our search
			high = endBlock
		}

		fetches = append(fetches, fetchJob{
			From: low,
			To:   high,
		})

		if high == endBlock {
			break
		}
		low += s.blockRange + 1
	}

	wg := sync.WaitGroup{}
	stop := make(chan bool)
	jobs := make(chan fetchJob, len(fetches))
	results := make(chan jobResult, len(fetches))
	defer close(results)

	// Multiply workers by client count for better parallelism
	workers := min(batchWorkers*s.clientCount(), len(fetches))
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go s.getSequencedLogs(jobs, results, stop, &wg)
	}

	for _, fetch := range fetches {
		jobs <- fetch
	}
	close(jobs)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	aimingFor := endBlock - startBlock
	complete := 0
	var progress uint64 = 0
	var prevProgress uint64 = 0
loop:
	for {
		select {
		case <-s.ctx.Done():
			break loop
		case res := <-results:
			if s.flagStop.Load() {
				break loop
			}

			complete++
			if res.Error != nil {
				err = res.Error
				break loop
			}
			progress += res.Size
			if len(res.Logs) > 0 {
				numLogs += uint64(len(res.Logs))

				wg.Add(1)
				logs := res.Logs
				go func(logs []ethTypes.Log) {
					defer wg.Done()

					if s.fetchHeaders {
						s.l1QueryHeaders(logs)
					}

					s.logsProduceChan <- LogEvent{Logs: logs}
				}(logs)
			}

			if complete == len(fetches) {
				// we've got all the results we need
				break loop
			}
		case <-ticker.C:
			if aimingFor == 0 {
				continue
			}
			s.logsChanProgress <- fmt.Sprintf("L1 Blocks processed progress (amounts): %d/%d (%d%%)", progress, aimingFor, (progress*100)/aimingFor)
			if progress > prevProgress {
				prevProgress = progress
				s.logsProduceChan <- LogEvent{Logs: nil} // send a nil log to indicate activity in case no logs for long in big range
			}
		}
	}

	close(stop)
	wg.Wait()

	return numLogs, err
}

func (s *L1Syncer) getSequencedLogs(jobs <-chan fetchJob, results chan jobResult, stop chan bool, wg *sync.WaitGroup) {
	defer wg.Done()

	backoffStrategy := backoff.NewExponentialBackOff()
	backoffStrategy.Multiplier = GetLogsBackoffMultiplier
	backoffStrategy.MaxInterval = 10 * time.Second
	backoffStrategy.MaxElapsedTime = 2 * time.Minute

	for j := range jobs {
		query := ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(j.From),
			ToBlock:   new(big.Int).SetUint64(j.To),
			Addresses: s.l1ContractAddresses,
			Topics:    s.topics,
		}

		var logs []ethTypes.Log
		var err error
	LOOP:
		for {
			select {
			case <-stop:
				break LOOP
			default:
				if s.flagStop.Load() {
					break LOOP
				}
				// First attempt: try the entire range with a timeout
				callCtx, cancel := context.WithTimeout(s.ctx, filterLogsTimeout)
				logs, err = s.etherman.FilterLogs(callCtx, query)
				cancel()
				if err != nil {
					// Check if err is 429 Too Many Requests
					lower := strings.ToLower(err.Error())
					is429 := strings.Contains(lower, "429") ||
						strings.Contains(lower, "too many") ||
						strings.Contains(lower, "rate limit")
					if is429 {
						// Rate limited: backoff and retry the same range (do NOT split)
						var d time.Duration
						if d = backoffStrategy.NextBackOff(); d == backoff.Stop {
							// backoff.Stop or invalid duration; reset and sleep a sane default
							backoffStrategy.Reset()
							d = DefaultBackoffSleep
						}
						time.Sleep(d)
						continue
					}
					// Non-rate-limit error: split the range using halving worker pool
					log.Debug("getSequencedLogs timed out or errored; splitting range", "err", err, "from", j.From, "to", j.To)
					// Fall back to halving the range with a bounded worker pool
					logs, err = s.fetchLogsWithHalving(s.ctx, query, j.From, j.To)
					break LOOP
				}
			}
			break
		}
		results <- jobResult{
			Size:  j.To - j.From,
			Error: err,
			Logs:  logs,
		}
	}
}

// fetchLogsWithHalving attempts to fetch logs for [from, to] inclusive using FilterLogs with a timeout.
// If the call times out or returns an error, the range is split into halves and queued for processing
// by a bounded worker pool, recursively halving until success or the span is below MinFilterBlockSpan.
// Returns aggregated logs and a non-nil error if any subrange could not be fetched under the minimum size.
func (s *L1Syncer) fetchLogsWithHalving(ctx context.Context, baseQuery ethereum.FilterQuery, from, to uint64) ([]ethTypes.Log, error) {
	type subJob struct{ From, To uint64 }

	// Aggregation and error tracking
	var (
		mu          sync.Mutex
		allLogs     []ethTypes.Log
		haveFailure atomic.Bool
	)

	jobs := make(chan subJob, subrangeWorkers*4)
	var tasksWG sync.WaitGroup

	// We will start the closer AFTER seeding the initial job to avoid a race
	// where Wait sees zero before any Add and closes the channel.

	// Helper to call FilterLogs for a specific subrange with timeout
	callOnce := func(f, t uint64) ([]ethTypes.Log, error) {
		q := baseQuery
		q.FromBlock = new(big.Int).SetUint64(f)
		q.ToBlock = new(big.Int).SetUint64(t)
		cctx, cancel := context.WithTimeout(ctx, filterLogsTimeout)
		defer cancel()
		return s.etherman.FilterLogs(cctx, q)
	}

	worker := func() {
		// Per-worker backoff for rate limiting, with jitter
		bo := backoff.NewExponentialBackOff()
		bo.Multiplier = GetLogsBackoffMultiplier
		bo.MaxInterval = 10 * time.Second

		for job := range jobs {
			// Use a local stack to avoid blocking on jobs channel when it's full.
			// We process left halves inline and enqueue right halves opportunistically.
			stack := []subJob{job}

			for len(stack) > 0 {
				cur := stack[len(stack)-1]
				stack = stack[:len(stack)-1]

				logs, err := callOnce(cur.From, cur.To)
				if err != nil {
					// Best-effort rate-limit detection
					lower := strings.ToLower(err.Error())
					is429 := strings.Contains(lower, "429") || strings.Contains(lower, "too many") || strings.Contains(lower, "rate limit")
					if is429 {
						// Rate limited: backoff and retry the same range (do NOT split)
						time.Sleep(bo.NextBackOff())
						stack = append(stack, cur) // retry this range
						continue
					}

					span := cur.To - cur.From
					if span <= MinFilterBlockSpan || cur.From > cur.To {
						// Below minimum span or invalid range: mark failure and continue
						haveFailure.Store(true)
						continue
					}

					mid := cur.From + span/2
					left := subJob{From: cur.From, To: mid}
					right := subJob{From: mid + 1, To: cur.To}

					// Prefer inline processing of left to keep making progress.
					// Try to enqueue right without blocking; otherwise, process it inline later.
					if right.From <= right.To {
						enqueued := false
						// Reserve a task slot before attempting to enqueue to avoid races with closer.
						tasksWG.Add(1)
						select {
						case jobs <- right:
							enqueued = true
						default:
							// queue is full, revert reservation and process inline instead
							tasksWG.Done()
						}
						if !enqueued {
							stack = append(stack, right)
						}
					}
					// Always process left inline next
					stack = append(stack, left)
					continue
				}

				// Success: aggregate logs
				if len(logs) > 0 {
					mu.Lock()
					allLogs = append(allLogs, logs...)
					mu.Unlock()
				}
				// Success resets per-worker backoff and increments ok counter
				bo.Reset()
			}
			// Finished processing this root job (and any inline splits)
			tasksWG.Done()
		}
	}

	// Kick off workers
	w := subrangeWorkers
	var wg sync.WaitGroup
	wg.Add(w)
	for i := 0; i < w; i++ {
		go func() { defer wg.Done(); worker() }()
	}

	// Seed initial job BEFORE starting the closer goroutine to ensure the
	// WaitGroup is non-zero when the closer starts waiting.
	tasksWG.Add(1)
	jobs <- subJob{From: from, To: to}

	// Close job channel when all scheduled tasks are done
	go func() {
		tasksWG.Wait()
		close(jobs)
	}()

	// Wait for all workers
	wg.Wait()

	if haveFailure.Load() {
		return allLogs, fmt.Errorf("failed to fetch some subranges [%d-%d] below min span", from, to)
	}
	return allLogs, nil
}

// calls the old rollup contract to get the accInputHash for a certain batch
// returns the accInputHash and lastBatchNumber
func (s *L1Syncer) callSequencedBatchesMap(ctx context.Context, addr *common.Address, batchNum uint64) (accInputHash common.Hash, err error) {
	mapKeyHex := fmt.Sprintf("%064x%064x", batchNum, 114 /* _legacySequencedBatches slot*/)
	mapKey := keccak256.Hash(common.FromHex(mapKeyHex))
	mkh := common.BytesToHash(mapKey)

	resp, err := s.etherman.StorageAt(ctx, *addr, mkh, nil)
	if err != nil {
		return
	}

	if len(resp) < 32 {
		return
	}
	accInputHash = common.BytesToHash(resp[:32])

	return
}

// calls the rollup contract to get the accInputHash for a certain batch
// returns the accInputHash and lastBatchNumber
func (s *L1Syncer) callGetRollupSequencedBatches(ctx context.Context, addr *common.Address, rollupId, batchNum uint64) (common.Hash, uint64, error) {
	rollupID := fmt.Sprintf("%064x", rollupId)
	batchNumber := fmt.Sprintf("%064x", batchNum)

	resp, err := s.etherman.CallContract(ctx, ethereum.CallMsg{
		To:   addr,
		Data: common.FromHex(rollupSequencedBatchesSignature + rollupID + batchNumber),
	}, nil)

	if err != nil {
		return common.Hash{}, 0, err
	}

	if len(resp) < 32 {
		return common.Hash{}, 0, errorShortResponseLT32
	}
	h := common.BytesToHash(resp[:32])

	if len(resp) < 96 {
		return common.Hash{}, 0, errorShortResponseLT96
	}
	lastBatchNumber := binary.BigEndian.Uint64(resp[88:96])

	return h, lastBatchNumber, nil
}

func (s *L1Syncer) CallAdmin(ctx context.Context, addr *common.Address) (common.Address, error) {
	return s.callGetAddress(ctx, addr, admin)
}

func (s *L1Syncer) CallRollupManager(ctx context.Context, addr *common.Address) (common.Address, error) {
	return s.callGetAddress(ctx, addr, rollupManager)
}

func (s *L1Syncer) CallGlobalExitRootManager(ctx context.Context, addr *common.Address) (common.Address, error) {
	return s.callGetAddress(ctx, addr, globalExitRootManager)
}

func (s *L1Syncer) CallTrustedSequencer(ctx context.Context, addr *common.Address) (common.Address, error) {
	return s.callGetAddress(ctx, addr, trustedSequencer)
}

func (s *L1Syncer) callGetAddress(ctx context.Context, addr *common.Address, data string) (common.Address, error) {
	resp, err := s.etherman.CallContract(ctx, ethereum.CallMsg{
		To:   addr,
		Data: common.FromHex(data),
	}, nil)

	if err != nil {
		return common.Address{}, err
	}

	if len(resp) < 20 {
		return common.Address{}, errorShortResponseLT32
	}

	return common.BytesToAddress(resp[len(resp)-20:]), nil
}

func (s *L1Syncer) CheckL1BlockFinalized(blockNo uint64) (finalized bool, finalizedBn uint64, err error) {
	block, err := s.etherman.BlockByNumber(s.ctx, big.NewInt(rpc.FinalizedBlockNumber.Int64()))
	if err != nil {
		return false, 0, err
	}

	return block.NumberU64() >= blockNo, block.NumberU64(), nil
}

func (s *L1Syncer) QueryForRootLog(to uint64) (*ethTypes.Log, error) {
	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(s.firstL1Block),
		ToBlock:   new(big.Int).SetUint64(to),
		Addresses: s.l1ContractAddresses,
		Topics:    s.topics,
	}
	// Pool handles retries internally via circuit breaker
	logs, err := s.etherman.FilterLogs(s.ctx, query)
	if err != nil {
		return nil, err
	}

	if len(logs) != 2 {
		// There should only be 2 logs, the root log and the log from the to block
		// this is called from index 1 on the info tree so we need the root to make index 0
		return nil, fmt.Errorf("did not find the expected number of logs")
	}

	return &logs[0], nil
}

func (s *L1Syncer) SetFetchHeaders(fetchHeaders bool) {
	s.fetchHeaders = fetchHeaders
}

func (s *L1Syncer) resetChannels() {
	s.logsSwapMutex.Lock()
	defer s.logsSwapMutex.Unlock()
	if s.logsConsumChan == s.logsProduceChan {
		s.logsConsumChan <- LogEvent{Logs: nil, Done: true, Progress: s.lastCheckedL1Block.Load()} // send a nil log to indicate activity in case no logs for long in big range
		close(s.logsConsumChan)
		s.logsProduceChan = make(chan LogEvent, logsChannelSize)
	}
	s.closeLogsChannel.Store(false)
}
