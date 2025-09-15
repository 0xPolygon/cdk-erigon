package realtimeapi

import (
	"fmt"
	"math/big"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/turbo/adapter/ethapi"
	"github.com/ledgerwatch/erigon/turbo/jsonrpc"
	realtimeCache "github.com/ledgerwatch/erigon/zk/realtime/cache"
	"github.com/ledgerwatch/erigon/zk/realtime/subscription"
)

var (
	MockBlockHash         = libcommon.BytesToHash([]byte{1})
	EmptyBlockHash        = libcommon.Hash{}
	ErrRealtimeNotEnabled = fmt.Errorf("realtime is not enabled")
)

// RealtimeAPIImpl is implementation of the RealtimeAPI interface
type RealtimeAPIImpl struct {
	*jsonrpc.APIImpl
	cacheDB    *realtimeCache.RealtimeCache
	subService *subscription.RealtimeSubscription
}

// NewRealtimeAPIImpl returns RealtimeAPIImpl instance
func NewRealtimeAPIImpl(
	base *jsonrpc.APIImpl,
	cacheDB *realtimeCache.RealtimeCache,
	subService *subscription.RealtimeSubscription,
) *RealtimeAPIImpl {

	return &RealtimeAPIImpl{
		APIImpl:    base,
		cacheDB:    cacheDB,
		subService: subService,
	}
}

func NewRealtimeAPI(
	base *jsonrpc.APIImpl,
	cacheDB *realtimeCache.RealtimeCache,
	subService *subscription.RealtimeSubscription,
) interface{} {
	return NewRealtimeAPIImpl(base, cacheDB, subService)
}

func (api *RealtimeAPIImpl) getBlockNumberOrHash(blockNrOrHash rpc.BlockNumberOrHash) (uint64, error) {
	hash, ok := blockNrOrHash.Hash()
	if !ok {
		blockNum, _, _, err := api.getBlockNumber(*blockNrOrHash.BlockNumber)
		if err != nil {
			return 0, err
		}
		return blockNum, nil
	} else {
		blockNum, found := api.cacheDB.Stateless.GetBlockNumberByHash(hash)
		if !found {
			return 0, fmt.Errorf("block %x not found", hash)
		}
		return blockNum, nil
	}
}

func (api *RealtimeAPIImpl) getBlockNumber(blockNr rpc.BlockNumber) (uint64, bool, bool, error) {
	if api.cacheDB == nil || !api.cacheDB.ReadyFlag.Load() {
		return 0, false, false, ErrRealtimeNotEnabled
	}

	confirmHeight, err := api.getConfirmHeightFromCache()
	if err != nil {
		return 0, false, false, err
	}
	pendingHeight, err := api.getPendingHeightFromCache()
	if err != nil {
		return 0, false, false, err
	}

	switch blockNr {
	case rpc.LatestBlockNumber:
		return confirmHeight, true, false, nil
	case rpc.PendingBlockNumber:
		return pendingHeight, false, true, nil
	// Unsupported tags
	case rpc.EarliestBlockNumber:
		return 0, false, false, fmt.Errorf("earliest block number is not realtime supported")
	case rpc.FinalizedBlockNumber:
		return 0, false, false, fmt.Errorf("finalized block number is not realtime supported")
	case rpc.SafeBlockNumber:
		return 0, false, false, fmt.Errorf("safe block number is not realtime supported")
	default:
		blockNumber := uint64(blockNr.Int64())
		if blockNumber > confirmHeight {
			return 0, false, false, fmt.Errorf("block with number %d not found", blockNumber)
		}
		return blockNumber, blockNumber == confirmHeight, blockNumber == pendingHeight, nil
	}
}

func (api *RealtimeAPIImpl) getPendingHeightFromCache() (uint64, error) {
	pendingHeight := api.cacheDB.GetPendingHeight()
	if pendingHeight == 0 {
		return 0, fmt.Errorf("no pending block number found in realtime cache")
	}
	return pendingHeight, nil
}

func (api *RealtimeAPIImpl) getConfirmHeightFromCache() (uint64, error) {
	confirmHeight := api.cacheDB.GetHighestConfirmHeight()
	if confirmHeight == 0 {
		return 0, fmt.Errorf("no confirmed block number found in realtime cache")
	}
	return confirmHeight, nil
}

func (api *RealtimeAPIImpl) createStateReader(blockNrOrHash rpc.BlockNumberOrHash) (state.StateReader, uint64, error) {
	if blockNrOrHash.BlockNumber == nil {
		blockHeight, err := api.getBlockNumberOrHash(blockNrOrHash)
		if err != nil {
			return nil, 0, err
		}
		reader := api.cacheDB.GetStateReaderByHeight(blockHeight)
		return reader, blockHeight, nil
	}

	pendingHeight := api.cacheDB.GetPendingHeight()
	if *blockNrOrHash.BlockNumber == rpc.PendingBlockNumber || *blockNrOrHash.BlockNumber == rpc.BlockNumber(pendingHeight) {
		pendingReader, err := api.cacheDB.GetPendingStateCache()
		if pendingReader != nil && err == nil {
			return pendingReader, pendingHeight, nil
		}
		// Case where no pending block is open yet, or pending block was confirmed. Use latest confirmed state reader
		return api.cacheDB.GetLatestStateCache(), api.cacheDB.GetHighestConfirmHeight(), nil
	}

	if *blockNrOrHash.BlockNumber == rpc.LatestBlockNumber {
		return api.cacheDB.GetLatestStateCache(), api.cacheDB.GetHighestConfirmHeight(), nil
	}

	// Retrieve by height
	blockHeight, _, _, err := api.getBlockNumber(*blockNrOrHash.BlockNumber)
	if err != nil {
		return nil, 0, err
	}

	reader := api.cacheDB.GetStateReaderByHeight(blockHeight)
	return reader, blockHeight, nil
}

// newRPCTransaction_realtime returns a transaction that will serialize to the RPC
// representation, with the given location metadata set (if available).
// Note that realtime API do not support blockHash.
func newRPCTransaction_realtime(tx types.Transaction, txblockhash libcommon.Hash, blockNumber uint64, index uint64, baseFee *big.Int) *jsonrpc.RPCTransaction {
	blockhash := txblockhash
	if blockhash == EmptyBlockHash {
		blockhash = MockBlockHash
	}

	result := jsonrpc.NewRPCTransaction(tx, blockhash, blockNumber, index, baseFee)
	result.BlockHash = &txblockhash
	return result
}

// formatBlockResponse creates a formatted block response from cache data
// This utility function consolidates the block formatting logic used by both
// GetBlockByNumber and GetBlockByHash methods
func (api *RealtimeAPIImpl) tryGetBlockResponseFromNumber(
	blockNum uint64,
	fullTx bool,
) (map[string]interface{}, error) {
	header, _, _, ok := api.cacheDB.Stateless.GetBlockInfo(blockNum)
	if !ok {
		return nil, fmt.Errorf("header not found for block %d", blockNum)
	}

	var transactions []types.Transaction
	txHashes, ok := api.cacheDB.Stateless.GetBlockTxs(blockNum)
	if ok {
		for _, txHash := range txHashes {
			if tx, _, _, _, exists := api.cacheDB.Stateless.GetTxInfo(txHash); exists {
				transactions = append(transactions, tx)
			} else {
				return nil, fmt.Errorf("transaction %s not found in cache", txHash.Hex())
			}
		}
	}

	block := types.NewBlockWithHeader(header).WithBody(transactions, nil)

	additionalFields := map[string]interface{}{
		"totalDifficulty": (*hexutil.Big)(header.Difficulty),
	}

	response, err := ethapi.RPCMarshalBlockEx(block, true, fullTx, nil, libcommon.Hash{}, additionalFields)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal block: %w", err)
	}

	return response, nil
}
