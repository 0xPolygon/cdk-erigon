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
	"github.com/ledgerwatch/erigon/zk/utils"
	"github.com/ledgerwatch/log/v3"
)

func getNextPoolTransactions(ctx context.Context, cfg SequenceBlockCfg, executionAt, forkId uint64, alreadyYielded mapset.Set[[32]byte]) ([]types.Transaction, []common.Hash, bool, error) {
	var ids []common.Hash
	var transactions []types.Transaction
	var allConditionsOk bool
	var err error

	gasLimit := utils.GetBlockGasLimitForFork(forkId)

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
	batchCounters *vm.BatchCounterCollector,
	blockContext *evmtypes.BlockContext,
	header *types.Header,
	transaction types.Transaction,
	effectiveGasPrice uint8,
	l1Recovery bool,
	forkId, l1InfoIndex uint64,
	blockDataSizeChecker *BlockDataChecker,
	ethBlockGasPool *core.GasPool,
) (*types.Receipt, *core.ExecutionResult, *vm.TransactionCounter, overflowType, error) {
	var batchDataOverflow, overflow bool
	var err error

	txCounters := vm.NewTransactionCounter(transaction, sdb.smt.GetDepth(), uint16(forkId), cfg.zk.VirtualCountersSmtReduction, cfg.zk.ShouldCountersBeUnlimited(l1Recovery))
	overflow, err = batchCounters.AddNewTransactionCounters(txCounters)

	// run this only once the first time, do not add it on rerun
	if blockDataSizeChecker != nil {
		txL2Data, err := txCounters.GetL2DataCache()
		if err != nil {
			return nil, nil, txCounters, overflowNone, err
		}
		batchDataOverflow = blockDataSizeChecker.AddTransactionData(txL2Data)
		if batchDataOverflow {
			log.Info("BatchL2Data limit reached. Not adding last transaction", "txHash", transaction.Hash())
		}
	}
	if err != nil {
		return nil, nil, txCounters, overflowNone, err
	}
	anyOverflow := overflow || batchDataOverflow
	if anyOverflow && !l1Recovery {
		log.Debug("Transaction preexecute overflow detected", "txHash", transaction.Hash(), "counters", batchCounters.CombineCollectorsNoChanges().UsedAsString())
		return nil, nil, txCounters, overflowCounters, nil
	}

	// if not normalcy we want to create a gas pool per transaction (zkevm block gas limit is infinite), if normalcy create a pool per block.
	var gasPool *core.GasPool
	if !cfg.chainConfig.IsNormalcy(blockContext.BlockNumber) {
		gasPool = new(core.GasPool).AddGas(transactionGasLimit)
	} else {
		gasPool = ethBlockGasPool
	}

	// set the counter collector on the config so that we can gather info during the execution
	cfg.zkVmConfig.CounterCollector = txCounters.ExecutionCounters()

	// TODO: possibly inject zero tracer here!

	snapshot := ibs.Snapshot()
	ibs.Init(transaction.Hash(), common.Hash{}, 0)

	evm := vm.NewZkEVM(*blockContext, evmtypes.TxContext{}, ibs, cfg.chainConfig, *cfg.zkVmConfig)

	gasUsed := header.GasUsed

	receipt, execResult, _, err := core.ApplyTransaction_zkevm(
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

	if err != nil {
		if errors.Is(err, core.ErrGasLimitReached) {
			log.Debug("Transaction gas limit reached", "txHash", transaction.Hash())
			return nil, nil, txCounters, overflowGas, nil
		}
		return nil, nil, txCounters, overflowNone, err
	}

	if err = txCounters.ProcessTx(ibs, execResult.ReturnData); err != nil {
		return nil, nil, txCounters, overflowNone, err
	}

	batchCounters.UpdateExecutionAndProcessingCountersCache(txCounters)
	// now that we have executed we can check again for an overflow
	if overflow, err = batchCounters.CheckForOverflow(l1InfoIndex != 0); err != nil {
		return nil, nil, txCounters, overflowNone, err
	}

	counters := batchCounters.CombineCollectorsNoChanges().UsedAsString()
	if overflow {
		log.Debug("Transaction overflow detected", "txHash", transaction.Hash(), "coutners", counters)
		ibs.RevertToSnapshot(snapshot)
		return nil, nil, txCounters, overflowCounters, nil
	}
	if gasUsed > header.GasLimit {
		log.Debug("Transaction overflows block gas limit", "txHash", transaction.Hash(), "txGas", receipt.GasUsed, "blockGasUsed", header.GasUsed)
		ibs.RevertToSnapshot(snapshot)
		return nil, nil, txCounters, overflowGas, nil
	}
	log.Debug("Transaction added", "txHash", transaction.Hash(), "coutners", counters)

	// add the gas only if not reverted. This should not be moved above the overflow check
	header.GasUsed = gasUsed

	// we need to keep hold of the effective percentage used
	// todo [zkevm] for now we're hard coding to the max value but we need to calc this properly
	if err = sdb.hermezDb.WriteEffectiveGasPricePercentage(transaction.Hash(), effectiveGasPrice); err != nil {
		return nil, nil, txCounters, overflowNone, err
	}

	ibs.FinalizeTx(evm.ChainRules(), noop)

	return receipt, execResult, txCounters, overflowNone, nil
}
