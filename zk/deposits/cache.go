package deposits

import (
	"math/big"
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

// Cache groups parsed deposits by their L1 block hash.
type Cache struct {
	mtx sync.Mutex
	// key: L1 block hash
	deps map[libcommon.Hash][]*Deposit
}

func NewCache() *Cache {
	return &Cache{
		deps: make(map[libcommon.Hash][]*Deposit),
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
		c.deps[bh] = append(c.deps[bh], d)
	}
}

// Pop returns and removes deposits for the given L1 block hash.
func (c *Cache) Pop(l1Hash libcommon.Hash) []*Deposit {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	out := c.deps[l1Hash]
	delete(c.deps, l1Hash)
	return out
}
