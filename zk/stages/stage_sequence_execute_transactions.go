package stages

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"

	"io"

	mapset "github.com/deckarep/golang-set/v2"
	types2 "github.com/ledgerwatch/erigon-lib/types"
	"github.com/ledgerwatch/erigon/core"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/core/vm/evmtypes"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
	"github.com/ledgerwatch/erigon/zk/utils"
	"github.com/ledgerwatch/log/v3"
)

func getNextPoolTransactions(ctx context.Context, cfg SequenceBlockCfg, executionAt, forkId uint64, alreadyYielded mapset.Set[[32]byte]) ([]types.Transaction, []common.Hash, bool, error) {
	var ids []common.Hash
	var transactions []types.Transaction
	var allConditionsOk bool
	var err error

	//gasLimit := utils.GetBlockGasLimitForFork(forkId)
	gasLimit := cfg.zk.XLayer.DynamicBlockGasLimit // For X Layer, use the dynamicblock gas limit

	ti := utils.StartTimer("txpool", "get-transactions")
	defer ti.LogTimer()

	cfg.txPool.PreYield()
	defer cfg.txPool.PostYield()

	// For X Layer, optimize getTransactions
	slots := types2.TxsRlp{}
	if err := cfg.txPoolDb.View(ctx, func(poolTx kv.Tx) error {
		if allConditionsOk, _, err = cfg.txPool.YieldBest(cfg.yieldSize, &slots, poolTx, executionAt, gasLimit, 0, alreadyYielded); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, nil, allConditionsOk, err
	}

	// For X Layer, optimize getTransactions
	yieldedTxs, yieldedIds, toRemove, err := extractTransactionsFromSlot(&slots, executionAt, cfg)
	if err != nil {
		return nil, nil, allConditionsOk, err
	}
	for _, txId := range toRemove {
		cfg.txPool.MarkForDiscardFromPendingBest(txId)
	}
	transactions = append(transactions, yieldedTxs...)
	ids = append(ids, yieldedIds...)

	for _, tx := range transactions {
		utils.LogTrace(
			tx.Hash().String(),         // txhash
			utils.ServiceNameSequencer, // serviceName
			utils.StepSeqReceiveTx.ID,  // processId
			utils.StepSeqReceiveTx.Key, // processWord
			executionAt+1,              // blockHeight
			"",                         // blockHash
			0,                          // blockTime
			int8(tx.Type()),            // transactionType
		)
	}

	return transactions, ids, allConditionsOk, err
}

func getLimboTransaction(ctx context.Context, cfg SequenceBlockCfg, txHash *common.Hash, executionAt uint64) ([]types.Transaction, error) {
	var transactions []types.Transaction
	// ensure we don't spin forever looking for transactions, attempt for a while then exit up to the caller
	if err := cfg.txPoolDb.View(ctx, func(poolTx kv.Tx) error {
		slots, err := cfg.txPool.GetLimboTxRplsByHash(poolTx, txHash)
		if err != nil {
			return err
		}

		if slots != nil {
			// ignore the toRemove value here, we know the RLP will be sound as we had to read it from the pool
			// in the first place to get it into limbo
			transactions, _, _, err = extractTransactionsFromSlot(slots, executionAt, cfg)
			if err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return transactions, nil
}

func extractTransactionsFromSlot(slot *types2.TxsRlp, currentHeight uint64, cfg SequenceBlockCfg) ([]types.Transaction, []common.Hash, []common.Hash, error) {
	// For X Layer, optimize extractTransactionsFromSlot
	if len(slot.Txs) != len(slot.TxIds) {
		return nil, nil, nil, fmt.Errorf("mismatched lengths: Txs=%d, TxIds=%d", len(slot.Txs), len(slot.TxIds))
	}

	// Early exit if the transaction list is empty
	if len(slot.Txs) == 0 {
		return []types.Transaction{}, []common.Hash{}, []common.Hash{}, nil
	}

	numWorkers := runtime.NumCPU() / 2
	if numWorkers < 1 {
		numWorkers = 1
	}

	tasks := make(chan task, len(slot.Txs))
	results := make(chan result, len(slot.Txs))

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	// For X Layer, optimize extractTransactionsFromSlot
	// Start workers
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for t := range tasks {
				tx, err := types.DecodeTransaction(t.txBytes)
				res := result{idx: t.idx, id: t.id}
				if err == io.EOF {
					continue
				}
				if err != nil {
					log.Warn("Failed to decode transaction from pool, skipping and removing",
						"error", err, "id", t.id)
					res.toRemove = true
					results <- res
					continue
				}

				if (t.sender != common.Address{}) {
					tx.SetSender(t.sender)
				}

				tx.Hash() // Pre-calculate transaction hash
				res.tx = tx
				results <- res
			}
		}()
	}

	// Distribute tasks
	for i, txBytes := range slot.Txs {
		tasks <- task{idx: i, txBytes: txBytes, id: slot.TxIds[i], sender: slot.Senders.AddressAt(i)}
	}
	close(tasks)

	// Wait for workers to finish
	wg.Wait()
	close(results)

	// Collect results in order
	txMap := make([]types.Transaction, len(slot.Txs))
	idMap := make([]common.Hash, len(slot.Txs))
	toRemove := make([]common.Hash, 0, len(slot.Txs)/10)
	validCount := 0

	for res := range results {
		if res.toRemove {
			toRemove = append(toRemove, res.id)
		} else {
			txMap[res.idx] = res.tx
			idMap[res.idx] = res.id
			validCount++
		}
	}

	// For X Layer, optimize extractTransactionsFromSlot
	// Build ordered results
	transactions := make([]types.Transaction, 0, validCount)
	ids := make([]common.Hash, 0, validCount)
	for i := 0; i < len(slot.Txs); i++ {
		if !contains(toRemove, slot.TxIds[i]) {
			transactions = append(transactions, txMap[i])
			ids = append(ids, idMap[i])
		}
	}

	return transactions, ids, toRemove, nil
}

type overflowType uint8

const (
	overflowNone overflowType = iota
	overflowCounters
	overflowGas
)

func attemptAddTransaction(
	cfg SequenceBlockCfg,
	sdb *stageDb,
	ibs *state.IntraBlockState,
	blockContext *evmtypes.BlockContext,
	header *types.Header,
	transaction types.Transaction,
	effectiveGasPrice uint8,
	l1Recovery bool,
	forkId, l1InfoIndex uint64,
	ethBlockGasPool *core.GasPool,
	txIndex int,
) (*types.Receipt, *core.ExecutionResult, []*zktypes.InnerTx, overflowType, error) {
	// Batch data size checking removed along with counters

	// if not normalcy we want to create a gas pool per transaction (zkevm block gas limit is infinite), if normalcy create a pool per block.
	var gasPool *core.GasPool
	if !cfg.chainConfig.IsNormalcy(blockContext.BlockNumber) {
		gasPool = new(core.GasPool).AddGas(cfg.zk.XLayer.DynamicBlockGasLimit)
	} else {
		gasPool = ethBlockGasPool
	}

	// Remove counter collector from config since we're not using counters
	cfg.zkVmConfig.CounterCollector = nil

	snapshot := ibs.Snapshot()
	ibs.Init(transaction.Hash(), common.Hash{}, txIndex)

	evm := vm.NewZkEVM(*blockContext, evmtypes.TxContext{}, ibs, cfg.chainConfig, *cfg.zkVmConfig)

	gasUsed := header.GasUsed

	receipt, execResult, innerTxs, err := core.ApplyTransaction_zkevm(
		cfg.chainConfig,
		cfg.engine,
		evm,
		gasPool,
		ibs,
		noop,
		header,
		transaction,
		&gasUsed,
		effectiveGasPrice,
		false,
	)

	if err == nil && receipt != nil {
		utils.LogTrace(
			transaction.Hash().String(), // txhash
			utils.ServiceNameSequencer,  // serviceName
			utils.StepSeqPackageTx.ID,   // processId
			utils.StepSeqPackageTx.Key,  // processWord
			header.Number.Uint64(),      // blockHeight
			"",                          // blockHash
			0,                           // blockTime
			int8(transaction.Type()),    // transactionType
		)
	}

	if err != nil {
		if errors.Is(err, core.ErrGasLimitReached) {
			log.Debug("Transaction gas limit reached", "txHash", transaction.Hash())
			return nil, nil, nil, overflowGas, nil
		}
		return nil, nil, nil, overflowNone, err
	}

	if gasUsed > header.GasLimit {
		log.Debug("Transaction overflows block gas limit", "txHash", transaction.Hash(), "txGas", receipt.GasUsed, "blockGasUsed", header.GasUsed)
		ibs.RevertToSnapshot(snapshot)
		return nil, nil, nil, overflowGas, nil
	}

	// For X Layer, check if the transaction overflows the dynamic block gas limit
	if gasUsed > cfg.zk.XLayer.DynamicBlockGasLimit {
		log.Info("Transaction overflows block gas limit", "txHash", transaction.Hash(), "txGas", receipt.GasUsed, "blockGasUsed", header.GasUsed)
		ibs.RevertToSnapshot(snapshot)
		return nil, nil, nil, overflowGas, nil
	}

	// ==================== BridgeEvent Interception Check ====================
	if err := interceptBridgeTransactionIfNeeded(receipt, transaction, &cfg.zk.XLayer.BridgeIntercept); err != nil {
		// Revert state and reject transaction
		ibs.RevertToSnapshot(snapshot)
		return nil, nil, nil, overflowNone, err
	}

	log.Debug("Transaction added", "txHash", transaction.Hash())

	// add the gas only if not reverted
	header.GasUsed = gasUsed

	// we need to keep hold of the effective percentage used
	// todo [zkevm] for now we're hard coding to the max value but we need to calc this properly
	if err = sdb.hermezDb.WriteEffectiveGasPricePercentage(transaction.Hash(), effectiveGasPrice); err != nil {
		return nil, nil, nil, overflowNone, err
	}

	if cfg.zk.XLayer.Realtime.Enable && cfg.kafkaTxInfoChan != nil {
		ibs.GenerateChangesetSinceSnapshotAndSendTxInfo(snapshot, cfg.kafkaTxInfoChan, transaction, receipt, innerTxs, header.Time)
	}

	ibs.FinalizeTx(evm.ChainRules(), noop)

	return receipt, execResult, innerTxs, overflowNone, nil
}
