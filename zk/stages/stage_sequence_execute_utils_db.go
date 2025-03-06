package stages

import (
	"context"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/core/state"
	db2 "github.com/ledgerwatch/erigon/smt/pkg/db"
	smtNs "github.com/ledgerwatch/erigon/smt/pkg/smt"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/erigon/zkevm/log"
)

type stageDb struct {
	ctx   context.Context
	db    kv.RwDB
	dbsmt kv.RwDB

	tx          kv.RwTx
	txsmt       kv.RwTx
	hermezDb    *hermez_db.HermezDb
	eridb       *db2.EriDb
	stateReader *state.PlainStateReader
	smt         *smtNs.SMT
}

func newStageDb(ctx context.Context, db, dbsmt kv.RwDB) (sdb *stageDb, err error) {
	var tx kv.RwTx
	if tx, err = db.BeginRw(ctx); err != nil {
		log.Error("failed to start maindb tx", "err", err)
		return nil, err
	}

	var txsmt kv.RwTx
	if txsmt, err = dbsmt.BeginRw(ctx); err != nil {
		log.Error("failed to start smt tx", "err", err)
		return nil, err
	}

	sdb = &stageDb{
		ctx:   ctx,
		db:    db,
		dbsmt: dbsmt,
	}
	sdb.SetTx(tx, txsmt)
	return sdb, nil
}

func (sdb *stageDb) SetTx(tx, txsmt kv.RwTx) {
	sdb.tx = tx
	sdb.txsmt = txsmt
	sdb.hermezDb = hermez_db.NewHermezDb(tx)
	sdb.eridb = db2.NewEriDb(txsmt)
	sdb.stateReader = state.NewPlainStateReader(tx)
	sdb.smt = smtNs.NewSMT(sdb.eridb, false)
}

func (sdb *stageDb) CommitAndStart() (err error) {
	if err = sdb.tx.Commit(); err != nil {
		sdb.txsmt.Rollback()
		return err
	}
	if err = sdb.txsmt.Commit(); err != nil {
		return err
	}

	tx, err := sdb.db.BeginRw(sdb.ctx)
	if err != nil {
		return err
	}
	txsmt, err := sdb.dbsmt.BeginRw(sdb.ctx)
	if err != nil {
		return err
	}

	sdb.SetTx(tx, txsmt)
	return nil
}

func (sdb *stageDb) Commit() error {
	err := sdb.tx.Commit()
	if err != nil {
		sdb.txsmt.Rollback()
		return err
	}
	return sdb.txsmt.Commit()
}
