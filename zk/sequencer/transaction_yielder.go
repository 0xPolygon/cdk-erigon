package sequencer

import (
	"github.com/erigontech/erigon/core/types"
	"golang.org/x/net/context"
	"github.com/erigontech/erigon/zk/txpool"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon/zk/utils"
	types2 "github.com/erigontech/erigon-lib/types"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/eth/ethconfig"
	"time"
	"sync"
)

type PoolTransactionYielder struct {
	ctx context.Context
	cfg ethconfig.Zk

	readyMtx              sync.Mutex
	readyTransactions     []common.Hash
	readyTransactionBytes map[common.Hash][]byte
	toSkip                map[common.Hash]struct{}

	pool      *txpool.TxPool
	yieldSize uint16

	// the pool db which sits separate from the usual erigon db
	db kv.RwDB

	decodedTxCache *expirable.LRU[common.Hash, *types.Transaction]

	// executionAt and forkId are used during the yielding process and will be
	// updated by the sequencer every time a new block is being processed.
	executionAt uint64
	forkId      uint64

	startedYielding    bool
	startedYieldingMtx sync.Mutex
}

func NewPoolTransactionYielder(
	ctx context.Context,
	cfg ethconfig.Zk,
	pool *txpool.TxPool,
	yieldSize uint16,
	db kv.RwDB,
	decodedTxCache *expirable.LRU[common.Hash, *types.Transaction],
) *PoolTransactionYielder {
	// Initialize the channel with the specified size
	readyTransactions := make([]common.Hash, 0)

	return &PoolTransactionYielder{
		readyTransactions:     readyTransactions,
		readyTransactionBytes: make(map[common.Hash][]byte),
		readyMtx:              sync.Mutex{},
		toSkip:                make(map[common.Hash]struct{}),
		ctx:                   ctx,
		cfg:                   cfg,
		pool:                  pool,
		yieldSize:             yieldSize,
		db:                    db,
		decodedTxCache:        decodedTxCache,
		startedYielding:       false,
		startedYieldingMtx:    sync.Mutex{},
	}
}

func (y *PoolTransactionYielder) YieldNextTransaction() (types.Transaction, uint8, bool) {
	var tx types.Transaction
	var effectiveGas uint8
	var err error
	var yieldedIdx int

	y.readyMtx.Lock()
	defer y.readyMtx.Unlock()

	for idx, hash := range y.readyTransactions {
		if _, found := y.toSkip[hash]; found {
			continue
		}
		if txBytes, found := y.readyTransactionBytes[hash]; found {
			txPtr, inCache := y.decodedTxCache.Get(hash)
			if inCache {
				tx = *txPtr
			} else {
				tx, err = types.DecodeTransaction(txBytes)
				if err != nil {
					log.Warn("[extractTransaction] Failed to decode transaction from ready queue, skipping and removing from queue",
						"error", err,
						"id", hash.String())
					y.pool.MarkForDiscardFromPendingBest(hash)
					y.toSkip[hash] = struct{}{}
					continue // Skip this transaction if decoding fails
				}
				y.decodedTxCache.Add(hash, &tx)
			}
			effectiveGas = deriveEffectiveGasPrice(y.cfg, tx)
			yieldedIdx = idx
			break
		}
	}

	return tx, effectiveGas, tx != nil && yieldedIdx < len(y.readyTransactions)
}

func (y *PoolTransactionYielder) RemoveMinedTransactions(hashes []common.Hash) {
	y.readyMtx.Lock()
	defer y.readyMtx.Unlock()

	for _, hash := range hashes {
		if _, found := y.decodedTxCache.Get(hash); found {
			y.decodedTxCache.Remove(hash)
		}
	}

	// ensure we take a fresh view on the pool
	y.toSkip = make(map[common.Hash]struct{})
}

func (y *PoolTransactionYielder) AddMined(hash common.Hash) {
	y.readyMtx.Lock()
	defer y.readyMtx.Unlock()
	y.toSkip[hash] = struct{}{}

	// remove the transaction from the readyTransactions slice. this will save burning CPU for the next yielding
	// for a transaction to execute.  we still maintain the hash in the toSkip map to avoid yielding it again
	// when we refresh the pool best list into the readyTransactions slice. there is a window where we have
	// executed something, but the pool hasn't removed it yet, so we could yield it again before this has happened
	for idx, readyHash := range y.readyTransactions {
		if readyHash == hash {
			// Remove the transaction from the slice
			y.readyTransactions = append(y.readyTransactions[:idx], y.readyTransactions[idx+1:]...)
			break
		}
	}
}

func (y *PoolTransactionYielder) SetExecutionDetails(executionAt, forkId uint64) {
	y.executionAt = executionAt
	y.forkId = forkId
}

func (y *PoolTransactionYielder) BeginYielding() {
	y.startedYieldingMtx.Lock()
	defer y.startedYieldingMtx.Unlock()

	if y.startedYielding {
		return
	}

	y.startedYielding = true

	go y.startLoop()
}

func (y *PoolTransactionYielder) startLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-y.ctx.Done():
			log.Info("Transaction yielder context done, stopping yielding")
			y.setYieldingState(false)
			return
		case <-ticker.C:
			y.performNextRefresh()
		}
	}
}

func (y *PoolTransactionYielder) setYieldingState(state bool) {
	y.startedYieldingMtx.Lock()
	defer y.startedYieldingMtx.Unlock()
	y.startedYielding = state
}

func (y *PoolTransactionYielder) performNextRefresh() {
	txHashes, txBytes, err := y.refreshPoolTransactions(y.executionAt, y.forkId)
	if err != nil {
		log.Error("Error while yielding next transactions", "error", err)
		time.Sleep(500 * time.Millisecond) // could be a transient error, wait before retrying
	}

	y.readyMtx.Lock()
	defer y.readyMtx.Unlock()

	y.readyTransactions = y.readyTransactions[:0] // Clear the ready transactions slice

	for idx, hash := range txHashes {
		y.readyTransactions = append(y.readyTransactions, hash)
		y.readyTransactionBytes[hash] = txBytes[idx]
	}
}

func (y *PoolTransactionYielder) refreshPoolTransactions(executionAt, forkId uint64) ([]common.Hash, [][]byte, error) {
	gasLimit := utils.GetBlockGasLimitForFork(forkId)

	ti := utils.StartTimer("txpool", "get-transactions")
	defer ti.LogTimer()

	y.pool.PreYield()
	defer y.pool.PostYield()

	var ids []common.Hash
	var txBytes [][]byte

	err := y.db.View(y.ctx, func(poolTx kv.Tx) error {
		slots := types2.TxsRlp{}
		_, _, err := y.pool.YieldBest(y.yieldSize, &slots, poolTx, executionAt, gasLimit, 0)
		if err != nil {
			return err
		}
		ids, txBytes, err = y.extractTransactionsFromSlot(&slots)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return ids, txBytes, nil
}

func (y *PoolTransactionYielder) extractTransactionsFromSlot(slot *types2.TxsRlp) ([]common.Hash, [][]byte, error) {
	ids := make([]common.Hash, 0, len(slot.TxIds))
	txBytes := make([][]byte, 0, len(slot.Txs))

	for idx, bytes := range slot.Txs {
		// get the id of the transaction and
		ids = append(ids, slot.TxIds[idx])
		txBytes = append(txBytes, bytes)
	}

	return ids, txBytes, nil
}

type LimboTransactionYielder struct {
	transactions []types.Transaction
	cfg          ethconfig.Zk
}

func NewLimboTransactionYielder(transactions []types.Transaction, cfg ethconfig.Zk) *LimboTransactionYielder {
	return &LimboTransactionYielder{
		transactions: transactions,
		cfg:          cfg,
	}
}

func (l *LimboTransactionYielder) YieldNextTransaction() (types.Transaction, uint8, bool) {
	if len(l.transactions) == 0 {
		return nil, 0, false
	}

	tx := l.transactions[0]
	effectiveGas := deriveEffectiveGasPrice(l.cfg, tx)
	l.transactions = l.transactions[1:] // Remove the transaction after yielding it

	return tx, effectiveGas, true
}

func (l *LimboTransactionYielder) AddMined(_ common.Hash) {
	// no need to maintain this
}

func (l *LimboTransactionYielder) RemoveMinedTransactions(hashes []common.Hash) {
	// do nothing as we remove transactions immediately after yielding them
}

func (l *LimboTransactionYielder) SetExecutionDetails(_, _ uint64) {
	// LimboTransactionYielder does not use executionAt and forkId, so this method can be empty
}

func (l *LimboTransactionYielder) BeginYielding() {
	// do nothing
}

type RecoveryTransactionYielder struct {
	transactions         []types.Transaction
	effectivePercentages []uint8
}

func NewRecoveryTransactionYielder(transactions []types.Transaction, effectivePercentages []uint8) *RecoveryTransactionYielder {
	return &RecoveryTransactionYielder{
		transactions:         transactions,
		effectivePercentages: effectivePercentages,
	}
}

func (d *RecoveryTransactionYielder) YieldNextTransaction() (types.Transaction, uint8, bool) {
	if len(d.transactions) == 0 {
		return nil, 0, false
	}

	tx := d.transactions[0]
	effectiveGas := d.effectivePercentages[0]

	if len(d.transactions) > 0 {
		d.transactions = d.transactions[1:] // Remove the transaction after yielding it
	}
	if len(d.effectivePercentages) > 0 {
		d.effectivePercentages = d.effectivePercentages[1:]
	}

	return tx, effectiveGas, true
}

func (d *RecoveryTransactionYielder) AddMined(_ common.Hash) {
	// no need to maintain this
}

func (d *RecoveryTransactionYielder) RemoveMinedTransactions(hashes []common.Hash) {
	// do nothing as we remove transactions immediately after yielding them
}

func (d *RecoveryTransactionYielder) SetExecutionDetails(_, _ uint64) {
	// RecoveryTransactionYielder does not use executionAt and forkId, so this method can be empty
}

func (d *RecoveryTransactionYielder) BeginYielding() {
	// do nothing
}
