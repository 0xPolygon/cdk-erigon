package rpchelper

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon-lib/kv"

	"github.com/ledgerwatch/erigon/eth/stagedsync/stages"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/erigon/zk/sequencer"
	"github.com/ledgerwatch/erigon/zkevm/jsonrpc/client"
)

var (
	// Global sequencer RPC URL for RPC nodes
	// use a global variable to avoid passing the sequencer RPC URL to
	//  every function (which is used in multiple places)
	sequencerRpcUrl string
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
	return GetFinalizedBlockNumberWithSequencerUrl(tx, GetSequencerRpcUrl())
}

func GetFinalizedBatchNumber(tx kv.Tx) (uint64, error) {
	return getFinalizedBatchNumberWithSequencerUrl(tx, GetSequencerRpcUrl())
}

// GetFinalizedBlockNumberWithSequencerUrl returns the finalized block number
// If running as sequencer, it read from local database
// If running as RPC node, it queries the sequencer for the finalized block header
func GetFinalizedBlockNumberWithSequencerUrl(tx kv.Tx, sequencerRpcUrl string) (uint64, error) {
	if sequencer.IsSequencer() {
		return getFinalizedBlockNumberAsSequencer(tx)
	} else {
		return getFinalizedBlockNumberAsRPC(tx, sequencerRpcUrl)
	}
}

func getFinalizedBatchNumberWithSequencerUrl(tx kv.Tx, sequencerRpcUrl string) (uint64, error) {
	if sequencer.IsSequencer() {
		return getFinalizedBatchNumberAsSequencer(tx)
	} else {
		return getFinalizedBatchNumberAsRPC(tx, sequencerRpcUrl)
	}
}

func getFinalizedBatchNumberAsSequencer(tx kv.Tx) (uint64, error) {
	return stages.GetStageProgress(tx, stages.AnalysisGroupVerifiedBatchNo)
}

func getFinalizedBatchNumberAsRPC(tx kv.Tx, sequencerRpcUrl string) (uint64, error) {
	if sequencerRpcUrl == "" {
		return 0, fmt.Errorf("sequencerRpcUrl is not set")
	}

	response, err := client.JSONRPCCall(sequencerRpcUrl, "zkevm_finalizedBatchNumber")
	if err != nil {
		return 0, fmt.Errorf("failed to call sequencer RPC: %w", err)
	}
	return transHexToUint64(response.Result)
}

// getFinalizedBlockNumberAsSequencer implements the original logic for sequencer nodes
func getFinalizedBlockNumberAsSequencer(tx kv.Tx) (uint64, error) {
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

// getFinalizedBlockNumberAsRPC implements the logic for RPC nodes by querying the sequencer
func getFinalizedBlockNumberAsRPC(tx kv.Tx, sequencerRpcUrl string) (uint64, error) {
	if sequencerRpcUrl == "" {
		panic("sequencerRpcUrl is not set")
	}

	// Query the sequencer for the finalized block header
	blockNumber, err := querySequencerForFinalizedBlock(sequencerRpcUrl)
	if err != nil {
		return 0, fmt.Errorf("failed to query sequencer for finalized block: %w", err)
	}

	return blockNumber, nil
}

// querySequencerForFinalizedBlock sends a request to the sequencer to get the finalized block number
func querySequencerForFinalizedBlock(sequencerRpcUrl string) (uint64, error) {
	// Send eth_getBlockByNumber request with "finalized" parameter
	response, err := client.JSONRPCCall(sequencerRpcUrl, "eth_getBlockByNumber", rpc.FinalizedBlockNumber.String(), false)
	if err != nil {
		return 0, fmt.Errorf("failed to call sequencer RPC: %w", err)
	}

	if response.Error != nil {
		return 0, fmt.Errorf("sequencer RPC error: %s", response.Error.Message)
	}

	// Parse the response to extract block number
	var blockHeader map[string]interface{}
	if err := json.Unmarshal(response.Result, &blockHeader); err != nil {
		return 0, fmt.Errorf("failed to unmarshal block header: %w", err)
	}

	// Extract block number from the response
	blockNumberHex, ok := blockHeader["number"].(string)
	if !ok {
		return 0, fmt.Errorf("invalid block number in response")
	}

	// Convert hex string to uint64
	blockNumber, err := hexutil.DecodeUint64(blockNumberHex)
	if err != nil {
		return 0, fmt.Errorf("failed to decode block number: %w", err)
	}

	return blockNumber, nil
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
