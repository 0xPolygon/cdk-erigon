// Copyright 2018 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package rawdb

import (
	"fmt"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/log/v3"
)

// WriteTxLookupEntries stores a positional metadata for every transaction from
// a block, enabling hash based transaction and receipt lookups.
func WriteTxLookupEntries_zkEvm(db kv.Putter, block *types.Block) error {
	for _, tx := range block.Transactions() {
		data := block.Number().Bytes()
		if err := db.Put(kv.TxLookup, tx.Hash().Bytes(), data); err != nil {
			return fmt.Errorf("db.Put %s: %W", kv.TxLookup, err)
		}
	}

	return nil
}

func TruncateTxLookupEntries_zkEvm(db kv.RwTx, fromBlockNum, toBlockNum uint64) (err error) {
	for i := fromBlockNum; i <= toBlockNum; i++ {
		block, err := ReadBlockByNumber(db, i)
		if err != nil {
			log.Warn("Error reading block during TruncateTxLookupEntries_zkEvm", "blockNumber", i, "error", err)
			continue // Skip this block instead of failing the entire operation
		}

		// Skip if block doesn't exist - this can happen during L1 recovery
		if block == nil {
			log.Info("Block not found during TruncateTxLookupEntries_zkEvm", "blockNumber", i)
			continue
		}

		// Skip if block has no transactions
		if len(block.Transactions()) == 0 {
			continue
		}

		for _, tx := range block.Transactions() {
			if err := db.Delete(kv.TxLookup, tx.Hash().Bytes()); err != nil {
				return fmt.Errorf("db.Delete %s: %w", kv.TxLookup, err)
			}
		}
	}

	return nil
}
