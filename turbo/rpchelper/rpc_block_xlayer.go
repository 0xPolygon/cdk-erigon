package rpchelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/ledgerwatch/erigon-lib/kv"

	"github.com/ledgerwatch/erigon/eth/stagedsync/stages"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/erigon/zk/sequencer"
	"github.com/ledgerwatch/erigon/zkevm/jsonrpc/client"
	"github.com/ledgerwatch/erigon/zkevm/log"
)

var (
	// Global sequencer RPC URL for RPC nodes
	// use a global variable to avoid passing the sequencer RPC URL to
	//  every function (which is used in multiple places)
	sequencerRpcUrl string

	// Global variable storing a pointer to the current finalized batch number.
	// A nil pointer indicates the value has not been fetched yet.
	currentFinalizedBatchNumber atomic.Pointer[uint64]

	// Global variable storing a pointer to the current block gas limit.
	// A nil pointer indicates the value has not been fetched yet.
	currentBlockGasLimit atomic.Pointer[uint64]

	// ErrFinalizedBatchUnavailable is returned when the poller has not yet fetched the finalized batch number.
	ErrFinalizedBatchUnavailable = errors.New("finalized batch number is not yet available from the poller")

	// ErrBlockGasLimitUnavailable is returned when the poller has not yet fetched the block gas limit.
	ErrBlockGasLimitUnavailable = errors.New("block gas limit is not yet available from the poller")
)

// SetSequencerRpcUrl sets the global sequencer RPC URL
func SetSequencerRpcUrl(url string) {
	sequencerRpcUrl = url
}

// GetSequencerRpcUrl returns the global sequencer RPC URL
func GetSequencerRpcUrl() string {
	return sequencerRpcUrl
}

// GetFinalizedBlockNumber returns the finalized block number
// This is a backward-compatible function that uses the global sequencer RPC URL
func GetFinalizedBlockNumber(tx kv.Tx) (uint64, error) {
	if sequencer.IsSequencer() {
		return getFinalizedBlockNumberFromLocalDB(tx)
	} else {
		return getBlockNumberFromCachedFinalizedBatchNumber(tx)
	}
}

func GetFinalizedBatchNumber(tx kv.Tx) (uint64, error) {
	if sequencer.IsSequencer() {
		return getFinalizedBatchNumberFromLocalDB(tx)
	} else {
		return getCachedFinalizedBatchNumber()
	}
}

func getFinalizedBatchNumberFromLocalDB(tx kv.Tx) (uint64, error) {
	return stages.GetStageProgress(tx, stages.AnalysisGroupVerifiedBatchNo)
}

// getFinalizedBlockNumberFromLocalDB implements the original logic for sequencer nodes
func getFinalizedBlockNumberFromLocalDB(tx kv.Tx) (uint64, error) {
	// get highest verified batch
	highestVerifiedBatchNo, err := stages.GetStageProgress(tx, stages.AnalysisGroupVerifiedBatchNo)
	if err != nil {
		return 0, err
	}
	hermezDb := hermez_db.NewHermezDbReader(tx)
	// we've got the highest batch to execute to, now get it's highest block
	highestVerifiedBlockHeight, _, err := hermezDb.GetHighestBlockInBatch(highestVerifiedBatchNo)
	if err != nil {
		return 0, err
	}

	var highestBlockNumber uint64
	highestBlockNumber, err = stages.GetStageProgress(tx, stages.Execution)
	if err != nil {
		return 0, fmt.Errorf("getting latest finished block number: %w", err)
	}

	blockNumber := highestVerifiedBlockHeight
	if highestBlockNumber < blockNumber {
		blockNumber = highestBlockNumber
	}

	return blockNumber, nil
}

// getBlockNumberFromCachedFinalizedBatchNumber reads the latest finalized batch number from the cache
// and uses the local database to find the corresponding highest block number in that batch.
func getBlockNumberFromCachedFinalizedBatchNumber(tx kv.Tx) (uint64, error) {
	// Read current finalized batch number from the poller cache.
	batchNumber, err := getCachedFinalizedBatchNumber()
	if err != nil {
		return 0, err
	}

	// Use hermez database to get the highest block in the finalized batch
	hermezDb := hermez_db.NewHermezDbReader(tx)
	blockNumber, _, err := hermezDb.GetHighestBlockInBatch(batchNumber)
	if err != nil {
		return 0, fmt.Errorf("failed to get highest block in batch %d: %w", batchNumber, err)
	}

	return blockNumber, nil
}

// StartFinalizedBatchPoller starts a background goroutine that queries the sequencer
// for finalized batch number on given interval; stops when ctx is done.
func StartFinalizedBatchPoller(ctx context.Context, interval time.Duration) {
	if sequencer.IsSequencer() {
		return
	}

	go startBackgroundQuery(ctx, interval)
}

// startBackgroundQuery runs the poller loop that periodically fetches the
// finalized batch number from the sequencer and updates the cache.
func startBackgroundQuery(ctx context.Context, interval time.Duration) {
	if sequencerRpcUrl == "" {
		log.Warn("finalized batch poller not started: empty sequencer RPC URL")
		return
	}
	// Initial fetch to warm up the cache and handle early requests.
	if bn, err := getFinalizedBatchNumberFromSequencer(sequencerRpcUrl); err != nil {
		log.Error("failed to get finalized batch number from sequencer (initial fetch)", "err", err)
	} else {
		newVal := bn
		currentFinalizedBatchNumber.Store(&newVal)
	}

	// Initial fetch for block gas limit
	if gasLimit, err := getBlockGasLimitFromSequencer(sequencerRpcUrl); err != nil {
		log.Warn("failed to get block gas limit from sequencer (initial fetch)", "err", err)
		newVal := uint64(1000_0000)
		currentBlockGasLimit.Store(&newVal)
	} else {
		newVal := gasLimit
		currentBlockGasLimit.Store(&newVal)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("finalized batch poller stopped")
			return
		case <-ticker.C:
			bn, err := getFinalizedBatchNumberFromSequencer(sequencerRpcUrl)
			if err != nil {
				log.Error("failed to get finalized batch number from sequencer", "err", err)
			} else {
				newVal := bn
				currentFinalizedBatchNumber.Store(&newVal)
			}

			// Also fetch block gas limit
			gasLimit, err := getBlockGasLimitFromSequencer(sequencerRpcUrl)
			if err != nil {
				log.Warn("failed to get block gas limit from sequencer", "err", err)
				// Don't continue here, we want to update batch number even if gas limit fails
			} else {
				newValGas := gasLimit
				currentBlockGasLimit.Store(&newValGas)
			}
		}
	}
}

func getFinalizedBatchNumberFromSequencer(sequencerRpcUrl string) (uint64, error) {
	if sequencerRpcUrl == "" {
		return 0, fmt.Errorf("sequencerRpcUrl is not set")
	}

	response, err := client.JSONRPCCall(sequencerRpcUrl, "zkevm_finalizedBatchNumber")
	if err != nil {
		return 0, fmt.Errorf("failed to call zkevm_finalizedBatchNumber to sequencer.err:%v. sequencerRpcUrl:%s", err, sequencerRpcUrl)
	}
	return transHexToUint64(response.Result)
}

func transHexToUint64(hex json.RawMessage) (uint64, error) {
	var result string
	err := json.Unmarshal(hex, &result)
	if err != nil {
		return 0, err
	}

	if len(result) > 1 && (result[:2] == "0x" || result[:2] == "0X") {
		result = result[2:]
	}

	result1, err := strconv.ParseUint(result, 16, 64)
	if err != nil {
		return 0, err
	}

	return result1, nil
}

// getCachedFinalizedBatchNumber is the single source of truth for reading the latest batch number
// fetched by the poller. It returns an error if the poller hasn't successfully fetched a value yet.
func getCachedFinalizedBatchNumber() (uint64, error) {
	valPtr := currentFinalizedBatchNumber.Load()
	if valPtr == nil {
		return 0, ErrFinalizedBatchUnavailable
	}
	return *valPtr, nil
}

func getBlockGasLimitFromSequencer(sequencerRpcUrl string) (uint64, error) {
	if sequencerRpcUrl == "" {
		return 0, fmt.Errorf("sequencerRpcUrl is not set")
	}

	response, err := client.JSONRPCCall(sequencerRpcUrl, "eth_getBlockGasLimit")
	if err != nil {
		return 0, fmt.Errorf("failed to call eth_getBlockGasLimit to sequencer.err:%v. sequencerRpcUrl:%s", err, sequencerRpcUrl)
	}
	return transHexToUint64(response.Result)
}

// GetCachedBlockGasLimit is the single source of truth for reading the latest block gas limit
// fetched by the poller. It returns an error if the poller hasn't successfully fetched a value yet.
func GetCachedBlockGasLimit() (uint64, error) {
	valPtr := currentBlockGasLimit.Load()
	if valPtr == nil {
		return 0, ErrBlockGasLimitUnavailable
	}
	return *valPtr, nil
}

// SetCachedBlockGasLimit sets the cached block gas limit (for testing purposes)
func SetCachedBlockGasLimit(gasLimit uint64) {
	currentBlockGasLimit.Store(&gasLimit)
}
