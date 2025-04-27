package stages

import (
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/types"
)

// task represents a transaction processing task
type task struct {
	idx     int
	txBytes []byte
	id      common.Hash
	sender  common.Address
}

// result represents the outcome of a transaction processing task
type result struct {
	idx      int
	tx       types.Transaction
	id       common.Hash
	toRemove bool
}

// contains checks if an id is in the slice
func contains(slice []common.Hash, id common.Hash) bool {
	for _, item := range slice {
		if item == id {
			return true
		}
	}
	return false
}
