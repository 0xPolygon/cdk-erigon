package main

import (
	"context"
	"os"

	mdbx2 "github.com/erigontech/mdbx-go/mdbx"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/mdbx"
	"github.com/ledgerwatch/erigon/smt/pkg/db"

	logv3 "github.com/ledgerwatch/log/v3"
)

func openDBWithOpts(optsFilePath string, dbPath string, log logv3.Logger, isSMT bool) (kv.RwDB, error) {
	ctx := context.Background()

	jsonData, err := os.ReadFile(optsFilePath)
	if err != nil {
		log.Error("Error reading from file: %v", err)
	}

	opts, err := mdbx.MdbxOptsFromJSON(jsonData)
	if err != nil {
		log.Error("Error unmarshalling data: %v", err)
	}
	newopts := opts.Path(dbPath)
	newopts = newopts.Logger(log)
	// mdbx.WithChaindataTables is the default table config. This is fine for smt db because Open return before this is called.
	newopts = newopts.WithTableCfg(mdbx.WithChaindataTables)
	if isSMT {
		newopts = newopts.Flags(func(flags uint) uint { return flags | mdbx2.WriteMap })
	}
	return newopts.Open(ctx)
}

func main() {
	log := logv3.New()
	log.SetHandler(logv3.LvlFilterHandler(logv3.LvlDebug, logv3.StdoutHandler))

	args := os.Args[1:]
	if len(args) != 1 {
		log.Error("Usage: smt-db-split <db_path>")
		return
	}

	optsMainDBPath := args[0] + "/chaindata/opts_chaindb.json"
	optsSMTDBPath := args[0] + "/smt/opts_smt.json"
	dbMainDBPath := args[0] + "/chaindata"
	dbSMTDBPath := args[0] + "/smt"

	// mdbx databases (mdbx.dat) and JSON opts files must exist
	if _, err := os.Stat(dbMainDBPath + "/mdbx.dat"); os.IsNotExist(err) {
		log.Error("DB path does not exist", "err", err)
		return
	}
	if _, err := os.Stat(optsMainDBPath); os.IsNotExist(err) {
		log.Error("Main DB opts path does not exist", "err", err)
		return
	}
	if _, err := os.Stat(dbSMTDBPath + "/mdbx.dat"); os.IsNotExist(err) {
		log.Error("SMT DB path does not exist", "err", err)
		return
	}
	if _, err := os.Stat(optsSMTDBPath); os.IsNotExist(err) {
		log.Error("SMT DB opts path does not exist", "err", err)
		return
	}

	// delete SMT tables from chaindb
	log.Info("Start dropping SMT tables from chaindb ...")
	chaindb, err := openDBWithOpts(optsMainDBPath, dbMainDBPath, log, false)
	if err != nil {
		log.Error("Failed to open chaindb", "err", err)
		return
	}
	defer chaindb.Close()
	ctx := context.Background()
	txchain, err := chaindb.BeginRw(ctx)
	if err != nil {
		log.Error("Failed to chaindb.BeginRw", "err", err)
	}
	defer txchain.Rollback()
	for _, bucket := range db.HermezSmtTables {
		err = txchain.DropBucket(bucket)
		if err != nil {
			log.Error("Failed to drop SMT", "bucket", bucket, "from chaindb err", err)
			// return
		}
	}
	txchain.Commit()
	log.Info("Done dropping SMT tables from chaindb.")
	// <-- end of delete SMT tables from chaindb

	// delete chaindb tables from SMT DB
	log.Info("Start dropping ChainDB tables from SMT DB ...")
	smtdb, err := openDBWithOpts(optsSMTDBPath, dbSMTDBPath, log, true)
	if err != nil {
		log.Error("Failed to open smtdb", "err", err)
		return
	}
	defer smtdb.Close()
	txsmt, err := smtdb.BeginRw(ctx)
	if err != nil {
		log.Error("Failed to smtdb.BeginRw", "err", err)
	}
	defer txsmt.Rollback()
	for _, bucket := range kv.ChaindataTables {
		err = txsmt.DropBucket(bucket)
		if err != nil {
			log.Error("Failed to drop chaindb", "bucket", bucket, "from SMT db err", err)
			// return
		}
	}
	txsmt.Commit()
	log.Info("Done dropping ChainDB tables from SMT DB.")
	// <-- end of delete chaindb tables from SMT DB

	log.Info("Splitting done.")
}
