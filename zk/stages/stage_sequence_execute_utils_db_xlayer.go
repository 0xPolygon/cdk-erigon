package stages

import (
	"github.com/ledgerwatch/erigon/eth/stagedsync"
)

func (sdb *stageDb) Commit(s *stagedsync.StageState, blockNumber uint64, flushSmt bool) error {
	if sdb.supportAC && flushSmt {
		blockCache := sdb.eridb.RetriveAndCleanCache()
		s.SetSmtCache(blockNumber, blockCache)
	}

	err := sdb.tx.Commit()
	if err != nil {
		if sdb.dbsmt != nil {
			sdb.txsmt.Rollback()
			// TODO: should we clear the cache?
		}
		return err
	}

	if !sdb.supportAC && sdb.dbsmt != nil {
		return sdb.txsmt.Commit()
	} else {
		return nil
	}
}

func (sdb *stageDb) Rollback() {
	sdb.tx.Rollback()
	if sdb.txsmt != nil {
		sdb.txsmt.Rollback()
	}
}
