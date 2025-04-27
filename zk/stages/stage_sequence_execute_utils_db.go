package stages

import (
	"context"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/core/state"
	db2 "github.com/ledgerwatch/erigon/smt/pkg/db"
	smtNs "github.com/ledgerwatch/erigon/smt/pkg/smt"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/log/v3"
)

type stageDb struct {
	ctx context.Context
	db  kv.RwDB

	tx          kv.RwTx
	hermezDb    *hermez_db.HermezDb
	eridb       smtNs.DB
	stateReader *state.PlainStateReader
	smt         *smtNs.SMT

	// For X Layer, split db and ac
	dbsmt     kv.RwDB
	txsmt     kv.Tx
	supportAC bool
}

func newStageDb(ctx context.Context, db, dbsmt kv.RwDB, supportAC bool) (sdb *stageDb, err error) {
	var tx kv.RwTx
	if tx, err = db.BeginRw(ctx); err != nil {
		log.Error("failed to start maindb tx", "err", err)
		return nil, err
	}

	// For X Layer, split db and ac
	sdb = &stageDb{
		supportAC: supportAC,
		ctx:       ctx,
		db:        db,
		dbsmt:     dbsmt,
	}

	if supportAC {
		// For X Layer, split db and ac
		// Support Async IO, only need to create read-only transaction
		var txsmt kv.Tx = nil
		if dbsmt != nil {
			// use multi mdbx
			if txsmt, err = dbsmt.BeginRo(ctx); err != nil {
				log.Error("failed to start smt tx", "err", err)
				return nil, err
			}
			eridb := db2.NewEriCacheDb(sdb.ctx, txsmt, tx)
			sdb.SetTx(tx, txsmt, eridb)
		} else {
			// use only one mdbx
			eridb := db2.NewEriDb(tx, tx)
			sdb.SetTx(tx, tx, eridb)
		}
	} else {
		// For X Layer, split db and ac
		// Support Sync IO，so need to create read-write transaction
		var txsmt kv.RwTx = nil
		if dbsmt != nil {
			// use multi mdbx
			if txsmt, err = dbsmt.BeginRw(ctx); err != nil {
				log.Error("failed to start smt tx", "err", err)
				return nil, err
			}
			eridb := db2.NewEriDb(txsmt, tx)
			sdb.SetTx(tx, txsmt, eridb)
		} else {
			// use only one mdbx
			eridb := db2.NewEriDb(tx, tx)
			sdb.SetTx(tx, tx, eridb)
		}
	}

	return sdb, nil
}

func (sdb *stageDb) SetTx(tx kv.RwTx, txsmt kv.Tx, eridb smtNs.DB) {
	sdb.tx = tx
	sdb.hermezDb = hermez_db.NewHermezDb(tx)
	sdb.stateReader = state.NewPlainStateReader(tx)

	// For X Layer, split db and ac
	sdb.txsmt = txsmt
	sdb.eridb = eridb
	sdb.smt = smtNs.NewSMT(sdb.eridb, false)
}

func (sdb *stageDb) CommitAndStart() (err error) {
	if err = sdb.tx.Commit(); err != nil {
		// For X Layer, split db and ac
		if !sdb.supportAC && sdb.dbsmt != nil {
			sdb.txsmt.Rollback()
		}
		return err
	}

	tx, err := sdb.db.BeginRw(sdb.ctx)
	if err != nil {
		return err
	}

	// For X Layer, split db and ac
	if !sdb.supportAC {
		// Support Sync IO，so need to create read-write transaction
		if sdb.dbsmt != nil {
			// use multi mdbx
			if err = sdb.txsmt.Commit(); err != nil {
				return err
			}

			txsmt, err := sdb.dbsmt.BeginRw(sdb.ctx)
			if err != nil {
				return err
			}
			eridb := db2.NewEriDb(txsmt, tx)

			sdb.SetTx(tx, txsmt, eridb)
		} else {
			// use only one mdbx, tx has already commit and create new tx
			eridb := db2.NewEriDb(tx, tx)
			sdb.SetTx(tx, tx, eridb)
		}
	} else {
		// Support Async IO, only need to create read-only transaction
		if sdb.dbsmt != nil {
			// use multi mdbx, no need to commit txsmt here and also no need to create new tx
			sdb.SetTx(tx, sdb.txsmt, sdb.eridb)
		} else {
			// use only one mdbx, tx has already commit and create new tx
			eridb := db2.NewEriDb(tx, tx)
			sdb.SetTx(tx, tx, eridb)
		}
	}

	return nil
}
