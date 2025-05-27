package syncer

import (
	"context"
	"errors"
	"fmt"
	"github.com/erigontech/erigon/zk/sequencer"
	"math/big"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	ethereum "github.com/erigontech/erigon"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/iden3/go-iden3-crypto/keccak256"

	"encoding/binary"

	ethTypes "github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/rpc"
)

const (
	progressMessageDuration = 10 * time.Second
	batchWorkers            = 2
	statusCodeNoError       = uint8(0)
	statusCodeHasError      = uint8(0)
)

var (
	errShortResponseLT32 = fmt.Errorf("response too short to contain hash data")
	errShortResponseLT96 = fmt.Errorf("response too short to contain last batch number data")
)

const (
	rollupSequencedBatchesSignature = "0x25280169" // hardcoded abi signature
	globalExitRootManager           = "0xd02103ca"
	rollupManager                   = "0x49b7b802"
	admin                           = "0xf851a440"
	trustedSequencer                = "0xcfa8ed47"
	sequencedBatchesMapSignature    = "0xb4d63f58"
)

//go:generate mockgen -typed=true -destination=./mocks/etherman_mock.go -package=mocks . IEtherman

type IEtherman interface {
	HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error)
	BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethTypes.Block, error)
	FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]ethTypes.Log, error)
	CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	TransactionByHash(ctx context.Context, hash common.Hash) (ethTypes.Transaction, bool, error)
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*ethTypes.Receipt, error)
	StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error)
}

type fetchJob struct {
	From uint64
	To   uint64
}

type jobResult struct {
	Size uint64
	Logs []ethTypes.Log
}

type L1Syncer struct {
	ctx                 context.Context
	etherMans           []IEtherman
	ethermanIndex       uint8
	ethermanMtx         *sync.Mutex
	l1ContractAddresses []common.Address
	topics              [][]common.Hash
	blockRange          uint64
	queryDelay          uint64 // milliseconds

	latestL1Block uint64

	// atomic
	isSyncStarted      atomic.Bool
	isDownloading      atomic.Bool
	lastCheckedL1Block atomic.Uint64
	wgRunLoopDone      sync.WaitGroup
	flagStop           atomic.Bool

	// tx provides access to l1 headers cache
	l1Cache *L1Cache

	highestBlockType string // finalized, latest, safe
	isSequencer      bool
}

func NewL1Syncer(ctx context.Context,
	l1Cache *L1Cache, etherMans []IEtherman,
	l1ContractAddresses []common.Address,
	topics [][]common.Hash,
	blockRange, queryDelay uint64,
	highestBlockType string,
) *L1Syncer {
	l1Syncer := &L1Syncer{
		ctx:                 ctx,
		etherMans:           etherMans,
		ethermanIndex:       0,
		ethermanMtx:         &sync.Mutex{},
		l1ContractAddresses: l1ContractAddresses,
		topics:              topics,
		blockRange:          blockRange,
		queryDelay:          queryDelay,
		highestBlockType:    highestBlockType,
		l1Cache:             l1Cache,
		isSequencer:         sequencer.IsSequencer(),
	}

	return l1Syncer
}

func (s *L1Syncer) getNextEtherman() IEtherman {
	s.ethermanMtx.Lock()
	defer s.ethermanMtx.Unlock()

	// Use modulo to ensure the index wraps around automatically
	etherman := s.etherMans[s.ethermanIndex%uint8(len(s.etherMans))]
	s.ethermanIndex++

	return etherman
}

// MatchAddress matching address from received log
func (s *L1Syncer) VerifyAddress(logEntry *ethTypes.Log) bool {
	return slices.Contains(s.l1ContractAddresses, logEntry.Address)
}

func (s *L1Syncer) IsSequencer() bool {
	return s.isSequencer
}

func (s *L1Syncer) IsSyncStarted() bool {
	return s.isSyncStarted.Load()
}

func (s *L1Syncer) StartSync() {
	s.isSyncStarted.Store(true)
}

func (s *L1Syncer) StopSync() {
	s.isSyncStarted.Store(false)
}

func (s *L1Syncer) IsDownloading() bool {
	return s.isDownloading.Load()
}

func (s *L1Syncer) StartDownloading() {
	s.isDownloading.Store(true)
}

func (s *L1Syncer) StopDownloading() {
	s.isDownloading.Store(false)
}

func (s *L1Syncer) GetLastCheckedL1Block() uint64 {
	return s.lastCheckedL1Block.Load()
}

func (s *L1Syncer) StopSyncer() {
	s.flagStop.Store(true)
}

// RunQueryBlocksOnce performs a one-time query of blockchain logs between the last checked block and the latest block.
func (s *L1Syncer) RunQueryBlocksOnce(logPrefix string, lastCheckedBlock uint64, logsCh chan<- []ethTypes.Log, errCh chan<- error) {
	s.StartSync()

	s.lastCheckedL1Block.Store(lastCheckedBlock)

	defer func() {
		log.Info(fmt.Sprintf("[%s] Stopping L1 RunQueryBlocksOnce syncer", logPrefix))

		close(logsCh)
		close(errCh)
		s.StopSync()
		s.StopDownloading()
	}()

	log.Info(fmt.Sprintf("[%s] Starting L1 syncer thread RunQueryBlocksOnce", logPrefix))

	latestL1Block, err := s.getLatestL1Block()
	if err != nil {
		log.Error(fmt.Sprintf("[%s] Error getting latest L1 block: %s", logPrefix, err))
		return
	}

	log.Info(fmt.Sprintf("[%s] RunQueryBlocksOnce from %d -> %d", logPrefix, s.lastCheckedL1Block.Load(), latestL1Block))

	if latestL1Block <= s.lastCheckedL1Block.Load() {
		return
	}

	s.StartDownloading()

	status := make(chan uint8)

	go s.queryBlocks(logPrefix, lastCheckedBlock, latestL1Block, logsCh, errCh, status)

	statusCode := <-status

	if statusCode == statusCodeNoError {
		s.lastCheckedL1Block.Store(latestL1Block)
	}

	log.Info(fmt.Sprintf("[%s] Returning L1 syncer thread RunQueryBlocksOnce", logPrefix))

	close(status)
	s.StopDownloading()

	return
}

// RunQueryBlocks
// It prevents multiple simultaneous executions and manages synchronization states across the process lifecycle.
func (s *L1Syncer) RunQueryBlocks(logPrefix string, lastCheckedBlock uint64, logsCh chan<- []ethTypes.Log, errCh chan<- error) {
	//if already started, don't start another thread
	if s.IsSyncStarted() {
		return
	}

	s.StartSync()

	s.lastCheckedL1Block.Store(lastCheckedBlock)

	//start a thread to check for new l1 block with interval
	go func() {
		// Never called
		defer func() {
			log.Info(fmt.Sprintf("[%s] Stopping L1 syncer RunQueryBlocks thread", logPrefix))
			close(logsCh)
			close(errCh)
			s.StopSync()
			s.StopDownloading()
		}()

		log.Info(fmt.Sprintf("[%s] Starting L1 syncer RunQueryBlocks thread", logPrefix))

		for {

			log.Info(fmt.Sprintf("[%s] Starting L1 syncer RunQueryBlocks loop iteration", logPrefix))

			latestL1Block, err := s.getLatestL1Block()
			if err != nil {
				log.Error(fmt.Sprintf("[%s] RunQueryBlocks Error getting latest L1 block: %s", logPrefix, err))
				continue
			}

			if latestL1Block > s.lastCheckedL1Block.Load() {
				log.Info(fmt.Sprintf("[%s] RunQueryBlocks %d -> %d", logPrefix, s.lastCheckedL1Block.Load(), latestL1Block))
				s.StartDownloading()

				status := make(chan uint8)

				go s.queryBlocks(logPrefix, lastCheckedBlock, latestL1Block, logsCh, errCh, status)

				statusCode := <-status

				if statusCode == statusCodeNoError {
					s.lastCheckedL1Block.Store(latestL1Block)
					lastCheckedBlock = latestL1Block
				}

				close(status)
				s.StopDownloading()
			}

			log.Info(fmt.Sprintf("[%s] L1 syncer RunQueryBlocks timeout instance", logPrefix))
			time.Sleep(time.Duration(s.queryDelay) * time.Millisecond)
		}
	}()
	return
}

func (s *L1Syncer) GetHeader(number uint64) (*ethTypes.Header, error) {
	var (
		header *ethTypes.Header
		err    error
	)

	header, err = s.l1Cache.getL1BlockHeaderCache(number)
	if err == nil {
		if header != nil {
			// log.Info(fmt.Sprintf("[L1Syncer] GetHeader: header from cache: %d", header.Number.Uint64()))
			return header, nil
		}
	} else {
		log.Warn(fmt.Sprintf("[L1Syncer] GetHeader getL1BlockHeaderCache error: %s", err))
	}

	em := s.getNextEtherman()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	header, err = em.HeaderByNumber(ctx, new(big.Int).SetUint64(number))

	if err != nil {
		cancel()
		return nil, err
	}

	cancel()

	err = s.l1Cache.writeL1BlockHeaderCache(header)
	if err != nil {
		log.Warn(fmt.Sprintf("[L1Syncer] GetHeader: write to cache: %s", err))
	}

	return header, nil
}

func (s *L1Syncer) GetBlock(number uint64) (*ethTypes.Block, error) {
	em := s.getNextEtherman()
	return em.BlockByNumber(s.ctx, new(big.Int).SetUint64(number))
}

func (s *L1Syncer) GetTransaction(hash common.Hash) (ethTypes.Transaction, bool, error) {
	em := s.getNextEtherman()
	return em.TransactionByHash(s.ctx, hash)
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
	em := s.getNextEtherman()
	r, err := em.TransactionReceipt(ctx, txHash)
	if err != nil {
		return 0, err
	}

	header, err := em.HeaderByNumber(context.Background(), r.BlockNumber)
	if err != nil {
		return 0, err
	}

	return header.Time, nil
}

func (s *L1Syncer) L1QueryHeaders(logs []ethTypes.Log) (map[uint64]*ethTypes.Header, error) {
	var (
		header *ethTypes.Header
		err    error
	)

	logsSize := len(logs)

	// queue up all the logs
	logQueue := make(chan *ethTypes.Log, logsSize)
	defer close(logQueue)
	for i := 0; i < logsSize; i++ {
		logQueue <- &logs[i]
	}

	var wg sync.WaitGroup
	wg.Add(logsSize)

	headersQueue := make(chan *ethTypes.Header, logsSize)

	process := func(em IEtherman) {

		for {
			if s.ctx.Err() != nil {
				wg.Done()
				break
			}

			l, ok := <-logQueue
			if !ok {
				break
			}

			header, err = s.l1Cache.getL1BlockHeaderCache(l.BlockNumber)
			if err == nil {
				if header != nil {
					// log.Info(fmt.Sprintf("[L1Syncer] L1QueryHeaders: header from cache: %d", header.Number.Uint64()))
					headersQueue <- header
					wg.Done()
					continue
				}
			} else {
				log.Warn(fmt.Sprintf("[L1Syncer] L1QueryHeaders getL1BlockHeaderCache error: %s", err))
			}

			// TODO: Use calls with timeout
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

			header, err = em.HeaderByNumber(ctx, new(big.Int).SetUint64(l.BlockNumber))
			// log.Info(fmt.Sprintf("[L1Syncer] L1QueryHeaders: not found from cache: %d", header.Number.Uint64()))

			if err != nil {
				cancel()
				log.Error("Error getting block", "err", err)
				// assume a transient error and try again
				time.Sleep(1 * time.Second)
				logQueue <- l
				continue
			}

			cancel()

			err = s.l1Cache.writeL1BlockHeaderCache(header)
			if err != nil {
				log.Warn(fmt.Sprintf("[L1Syncer] L1QueryHeaders: write to cache: %s", err))
			}

			headersQueue <- header
			wg.Done()
		}
	}

	// launch the workers - some endpoints might be faster than others so will consume more of the queue
	// but, we really don't care about that.  We want the data as fast as possible
	mans := s.etherMans
	for i := 0; i < len(mans); i++ {
		go process(mans[i])
	}

	wg.Wait()
	close(headersQueue)

	headersMap := map[uint64]*ethTypes.Header{}
	for header = range headersQueue {
		headersMap[header.Number.Uint64()] = header
	}
	return headersMap, nil
}

// Called a lot
// TODO: Check multiple calls with prefix "Waiting for txs from the pool... "
func (s *L1Syncer) getLatestL1Block() (uint64, error) {
	em := s.getNextEtherman()

	var blockNumber *big.Int

	switch s.highestBlockType {
	case "finalized":
		blockNumber = big.NewInt(rpc.FinalizedBlockNumber.Int64())
	case "safe":
		blockNumber = big.NewInt(rpc.SafeBlockNumber.Int64())
	case "latest":
		blockNumber = nil
	default:
		return 0, errors.New("highestBlockType is not valid")
	}

	latestBlock, err := em.BlockByNumber(context.Background(), blockNumber)
	if err != nil {
		return 0, err
	}

	latest := latestBlock.NumberU64()
	s.latestL1Block = latest

	log.Info(fmt.Sprintf("Received latest L1 block with option \"%s\": %d", s.highestBlockType, latest))

	return latest, nil
}

// queryBlocks returns error, if logs from L1 blocks sequence not loaded
// Partial loading not supported yet
func (s *L1Syncer) queryBlocks(logPrefix string, startBlock, lastBlock uint64, logsCh chan<- []ethTypes.Log, errCh chan<- error, status chan<- uint8) {
	// Fixed receiving duplicate log events.
	// lastCheckedL1Block means that it has already been checked in the previous cycle.
	// It should not be checked again in the new cycle, so +1 is added here.
	startBlock += 1

	// define the blocks we're going to fetch up front
	fetches := makeFetchJobs(startBlock, lastBlock, s.blockRange)

	log.Info(fmt.Sprintf("[%s] queryBlocks from %d to %d with range %d", logPrefix, startBlock, lastBlock, s.blockRange))

	// wg used for a graceful closing channel for multiple writers with defer()
	// wg := sync.WaitGroup{}
	jobs := make(chan fetchJob, len(fetches))
	results := make(chan jobResult, len(fetches))

	// wg.Add(batchWorkers)
	for i := 0; i < batchWorkers; i++ {
		go s.getFilteredLogs(logPrefix, jobs, results)
	}

	defer func() {
		close(results)
	}()

	for _, fetch := range fetches {
		jobs <- fetch
	}
	// No more writing, may close
	close(jobs)

	progress := atomic.Uint64{}

	aimingFor := lastBlock - startBlock - uint64(len(fetches)) + 1
	complete := 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go queryBlocksProcessingStatus(ctx, logPrefix, &progress, aimingFor)

	for {
		select {
		case <-s.ctx.Done():
			errCh <- errors.New("queryBlocks stopped by context")
			status <- statusCodeHasError
			return
		case res := <-results:
			complete++

			progress.Add(res.Size)

			if len(res.Logs) > 0 {
				// for _, logEntry := range res.Logs {
				// 	log.Info(fmt.Sprintf("[%s] Log received: %s ====> %s", logPrefix, logEntry.Address, logEntry.Topics))
				// }

				logsCh <- res.Logs
			}

			if complete == len(fetches) {
				// TODO: close here
				// we've got all the results we need
				status <- statusCodeNoError
				return
			}
		}
	}
}

// makeFetchJobs generates ranges pairs slice [from,to] without overlap, supported get_Logs filter,
// where blockFrom >= from, and blockTo <= to
func makeFetchJobs(src, dst, blockRange uint64) []fetchJob {
	fetches := make([]fetchJob, 0)

	for start := src; start <= dst; {
		end := start + blockRange - 1
		if end > dst {
			end = dst
		}
		fetches = append(fetches, fetchJob{From: start, To: end})
		start = end + 1
	}

	return fetches
}

// queryBlocksProcessingStatus shown status
func queryBlocksProcessingStatus(ctx context.Context, logPrefix string, processed *atomic.Uint64, totalCount uint64) {
	if totalCount == 0 {
		log.Info(fmt.Sprintf("[%s] L1 Blocks processed progress (amounts): no blocks", logPrefix))
		return
	}

	ticker := time.NewTicker(progressMessageDuration)
	defer ticker.Stop()

	printStatus := func() {
		log.Info(fmt.Sprintf(
			"[%s] L1 Blocks processed progress (amounts): %d/%d (%d%%)",
			logPrefix,
			processed.Load(),
			totalCount,
			(processed.Load()*100)/totalCount,
		),
		)
	}

	for {
		select {
		case <-ticker.C:
			printStatus()
		case <-ctx.Done():
			printStatus()
			return
		}
	}
}

func (s *L1Syncer) getFilteredLogs(logPrefix string, jobs <-chan fetchJob, results chan jobResult) {
	for {
		select {
		case <-s.ctx.Done():
			return
		case j, ok := <-jobs:
			if !ok {
				return
			}

			query := ethereum.FilterQuery{
				FromBlock: new(big.Int).SetUint64(j.From),
				ToBlock:   new(big.Int).SetUint64(j.To),
				Addresses: s.l1ContractAddresses,
				Topics:    s.topics,
			}

			var logs []ethTypes.Log
			var err error

			for attempt := 0; attempt < 20; attempt++ {
				em := s.getNextEtherman()
				logs, err = em.FilterLogs(context.Background(), query)
				if err == nil {
					results <- jobResult{
						Size: j.To - j.From,
						Logs: logs,
					}
					break
				} else {
					if attempt > 5 {
						log.Error(fmt.Sprintf("[%s] L1 syncer (getFilteredLogs) exceeded %d retries. Error: %s", logPrefix, attempt, err))

						if attempt == 20 {
							panic("L1 syncer (getFilteredLogs) exceeded 20 retries")
						}

						time.Sleep(time.Duration(attempt*2) * time.Second)
					}
				}

			}
		}
	}
}

// calls the old rollup contract to get the accInputHash for a certain batch
// returns the accInputHash and lastBatchNumber
func (s *L1Syncer) callSequencedBatchesMap(ctx context.Context, addr *common.Address, batchNum uint64) (accInputHash common.Hash, err error) {
	mapKeyHex := fmt.Sprintf("%064x%064x", batchNum, 114 /* _legacySequencedBatches slot*/)
	mapKey := keccak256.Hash(common.FromHex(mapKeyHex))
	mkh := common.BytesToHash(mapKey)

	em := s.getNextEtherman()

	resp, err := em.StorageAt(ctx, *addr, mkh, nil)
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

	em := s.getNextEtherman()
	resp, err := em.CallContract(ctx, ethereum.CallMsg{
		To:   addr,
		Data: common.FromHex(rollupSequencedBatchesSignature + rollupID + batchNumber),
	}, nil)

	if err != nil {
		return common.Hash{}, 0, err
	}

	if len(resp) < 32 {
		return common.Hash{}, 0, errShortResponseLT32
	}
	h := common.BytesToHash(resp[:32])

	if len(resp) < 96 {
		return common.Hash{}, 0, errShortResponseLT96
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
	em := s.getNextEtherman()
	resp, err := em.CallContract(ctx, ethereum.CallMsg{
		To:   addr,
		Data: common.FromHex(data),
	}, nil)

	if err != nil {
		return common.Address{}, err
	}

	if len(resp) < 20 {
		return common.Address{}, errShortResponseLT32
	}

	return common.BytesToAddress(resp[len(resp)-20:]), nil
}

func (s *L1Syncer) CheckL1BlockFinalized(blockNo uint64) (finalized bool, finalizedBn uint64, err error) {
	em := s.getNextEtherman()
	block, err := em.BlockByNumber(s.ctx, big.NewInt(rpc.FinalizedBlockNumber.Int64()))
	if err != nil {
		return false, 0, err
	}

	return block.NumberU64() >= blockNo, block.NumberU64(), nil
}

func (s *L1Syncer) QueryForRootLog(to uint64) (*ethTypes.Log, error) {
	var logs []ethTypes.Log
	var err error
	retry := 0
	for {
		em := s.getNextEtherman()
		query := ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(0),
			ToBlock:   new(big.Int).SetUint64(to),
			Addresses: s.l1ContractAddresses,
			Topics:    s.topics,
		}
		logs, err = em.FilterLogs(context.Background(), query)
		if err != nil {
			log.Debug("QueryForRootLog retry error", "err", err)
			retry++
			if retry > 5 {
				return nil, err
			}
			time.Sleep(time.Duration(retry*2) * time.Second)
			continue
		}
		break
	}

	if len(logs) != 2 {
		// There should only be 2 logs, the root log and the log from the to block
		// this is called from index 1 on the info tree so we need the root to make index 0
		return nil, fmt.Errorf("did not find the expected number of logs")
	}

	return &logs[0], nil
}

func (s *L1Syncer) WriteL1TreeLogs(logEntry ethTypes.Log) error {
	return s.l1Cache.writeL1TreeLogs(&logEntry)
}

func (s *L1Syncer) GetLastL1TreeLogBlockNumber() (uint64, error) {
	return s.l1Cache.getLastL1TreeLogBlockNumber()
}

func (s *L1Syncer) GetL1TreeLogs(startBlockNumber uint64, logsCh chan<- ethTypes.Log) {
	s.l1Cache.getL1TreeLogs(startBlockNumber, logsCh)
}

func (s *L1Syncer) ClearTreeLogs() error {
	return s.l1Cache.clearTreeLogs()
}

func (s *L1Syncer) TruncateTreeLogs(toBlockNumber uint64) error {
	return s.l1Cache.truncateTreeLogs(toBlockNumber)
}
