package sequencer

import (
	libcommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/zk/deposits"
)

// Local interface matching stages.TxYielder to avoid circular import.
type TxYielder interface {
	YieldNextTransaction() (types.Transaction, uint8, bool)
	AddMined(hash libcommon.Hash)
	Discard(hash libcommon.Hash)
	SetExecutionDetails(executionAt uint64, forkId uint64)
	BeginYielding()
	Cleanup()
}

// DepositTransactionYielder yields deposit-derived transactions from L1 logs.
// Note: actual conversion to DepositTx will be added when deposit tx type is present.
type DepositTransactionYielder struct {
	cache         *deposits.Cache
	currentL1Hash libcommon.Hash
	queue         []*deposits.Deposit
}

func NewDepositTransactionYielder(cache *deposits.Cache) *DepositTransactionYielder {
	return &DepositTransactionYielder{cache: cache}
}

// SetL1Origin selects which L1 block's deposits to yield.
func (d *DepositTransactionYielder) SetL1Origin(l1Hash libcommon.Hash) {
	d.currentL1Hash = l1Hash
	d.queue = d.cache.Pop(l1Hash)
}

func (d *DepositTransactionYielder) YieldNextTransaction() (types.Transaction, uint8, bool) {
	if len(d.queue) == 0 {
		return nil, 0, false
	}
	// TODO: convert deposit payload into real DepositTx when the type is added.
	d.queue = d.queue[1:]
	return nil, 0, false
}

func (d *DepositTransactionYielder) AddMined(_ libcommon.Hash)       {}
func (d *DepositTransactionYielder) Discard(_ libcommon.Hash)        {}
func (d *DepositTransactionYielder) SetExecutionDetails(_, _ uint64) {}
func (d *DepositTransactionYielder) BeginYielding()                  {}
func (d *DepositTransactionYielder) Cleanup()                        {}

// CombinedTransactionYielder tries deposits first, then falls back to pool.
type CombinedTransactionYielder struct {
	deposits *DepositTransactionYielder
	pool     TxYielder
}

func NewCombinedTransactionYielder(deposits *DepositTransactionYielder, pool TxYielder) *CombinedTransactionYielder {
	return &CombinedTransactionYielder{deposits: deposits, pool: pool}
}

func (c *CombinedTransactionYielder) YieldNextTransaction() (types.Transaction, uint8, bool) {
	if c.deposits != nil {
		if tx, gas, ok := c.deposits.YieldNextTransaction(); ok {
			return tx, gas, ok
		}
	}
	return c.pool.YieldNextTransaction()
}

func (c *CombinedTransactionYielder) AddMined(h libcommon.Hash) {
	if c.deposits != nil {
		c.deposits.AddMined(h)
	}
	c.pool.AddMined(h)
}

func (c *CombinedTransactionYielder) Discard(h libcommon.Hash) {
	if c.deposits != nil {
		c.deposits.Discard(h)
	}
	c.pool.Discard(h)
}

func (c *CombinedTransactionYielder) SetExecutionDetails(executionAt, forkId uint64) {
	if c.deposits != nil {
		c.deposits.SetExecutionDetails(executionAt, forkId)
	}
	c.pool.SetExecutionDetails(executionAt, forkId)
}

func (c *CombinedTransactionYielder) BeginYielding() {
	if c.deposits != nil {
		c.deposits.BeginYielding()
	}
	c.pool.BeginYielding()
}

func (c *CombinedTransactionYielder) Cleanup() {
	if c.deposits != nil {
		c.deposits.Cleanup()
	}
	c.pool.Cleanup()
}
