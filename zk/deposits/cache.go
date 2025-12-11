package deposits

import (
	"math/big"
	"sort"
	"sync"

	libcommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon/core/types"
)

// Parsed deposit log payload (no tx type yet).
type Deposit struct {
	From       libcommon.Address
	To         *libcommon.Address // nil if isCreation
	Mint       *big.Int           // supports 256-bit values
	Value      *big.Int
	Gas        uint64
	IsCreation bool
	Data       []byte
	SourceHash libcommon.Hash // keccak(L1BlockHash||logIndex)
	Log        types.Log      // original log for reference
}

type BlockDeposits struct {
	L1BlockHash   libcommon.Hash
	L1BlockNumber uint64
	Deposits      []*Deposit
}

// Cache groups parsed deposits by their L1 block hash and preserves arrival order.
type Cache struct {
	mtx sync.Mutex
	// key: L1 block hash
	blocks map[libcommon.Hash]*BlockDeposits
	order  []libcommon.Hash
}

func NewCache() *Cache {
	return &Cache{
		blocks: make(map[libcommon.Hash]*BlockDeposits),
	}
}

// AddDeposits groups deposits by their block hash.
func (c *Cache) AddDeposits(deps []*Deposit) {
	if len(deps) == 0 {
		return
	}
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for _, d := range deps {
		bh := libcommon.Hash(d.Log.BlockHash)
		block, ok := c.blocks[bh]
		if !ok {
			block = &BlockDeposits{
				L1BlockHash:   bh,
				L1BlockNumber: d.Log.BlockNumber,
			}
			c.blocks[bh] = block
			c.order = append(c.order, bh)
		}
		block.Deposits = append(block.Deposits, d)
	}
}

// PopNext returns deposits for the earliest queued L1 block whose block number is greater than afterBlockNumber.
func (c *Cache) PopNext(afterBlockNumber uint64) *BlockDeposits {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for len(c.order) > 0 {
		hash := c.order[0]
		c.order = c.order[1:]
		block := c.blocks[hash]
		delete(c.blocks, hash)
		if block == nil {
			continue
		}
		if block.L1BlockNumber <= afterBlockNumber {
			continue
		}
		sort.Slice(block.Deposits, func(i, j int) bool {
			return block.Deposits[i].Log.Index < block.Deposits[j].Log.Index
		})
		return block
	}
	return nil
}
