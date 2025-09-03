package state

import (
	"fmt"
	"sort"

	"github.com/ledgerwatch/erigon/core/types"
	ethTypes "github.com/ledgerwatch/erigon/core/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

type TxInfo struct {
	BlockNumber uint64
	Tx          ethTypes.Transaction
	Receipt     *ethTypes.Receipt
	InnerTxs    []*zktypes.InnerTx
	Entries     Entries
}

func (sdb *IntraBlockState) GenerateChangesetSinceSnapshotAndSendTxInfo(revid int, txInfoChan chan TxInfo, tx types.Transaction, receipt *types.Receipt, innerTxs []*zktypes.InnerTx) {
	// Find the snapshot in the stack of valid snapshots.
	idx := sort.Search(len(sdb.validRevisions), func(i int) bool {
		return sdb.validRevisions[i].id >= revid
	})
	if idx == len(sdb.validRevisions) || sdb.validRevisions[idx].id != revid {
		panic(fmt.Errorf("revision id %v cannot be reverted", revid))
	}
	snapshot := sdb.validRevisions[idx].journalIndex
	entries := Entries{
		entries:  &sdb.journal.entries,
		snapshot: snapshot,
	}

	txInfoChan <- TxInfo{
		BlockNumber: receipt.BlockNumber.Uint64(),
		Tx:          tx,
		Receipt:     receipt,
		InnerTxs:    innerTxs,
		Entries:     entries,
	}
}

func (sdb *IntraBlockState) GenerateChangeset() *realtimeTypes.Changeset {
	entries := Entries{
		entries:  &sdb.journal.entries,
		snapshot: 0,
	}
	return CollectChangeset(entries)
}
