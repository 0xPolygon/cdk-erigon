package main

import (
	"context"
	"os"

	"github.com/c2h5oh/datasize"
	mdbx2 "github.com/erigontech/mdbx-go/mdbx"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/mdbx"
	"github.com/ledgerwatch/erigon/smt/pkg/db"

	logv3 "github.com/ledgerwatch/log/v3"
)

func openDBWithOpts(label kv.Label, dbPath string, log logv3.Logger) (kv.RwDB, error) {
	ctx := context.Background()

	opts := mdbx.NewMDBX(log).Path(dbPath).Label(label).WithTableCfg(mdbx.WithChaindataTables)

	// Open the environment to get the page size and map size from mdbx.dat
	env, err := mdbx2.NewEnv()
	if err != nil {
		return nil, err
	}
	err = env.Open(dbPath, opts.GetFlags(), 0664)
	if err != nil {
		log.Error("Error openning env info", "error", err)
	}
	in, err := env.Info(nil)
	if err != nil {
		log.Error("Error getting env info", "error", err)
	}
	env.Close()

	newMapSize := datasize.ByteSize(in.MapSize)
	log.Info("Setting page size to the value in mdbx.dat.", "Value (in bytes)", in.PageSize)
	log.Info("Setting map size to the value in mdbx.dat.", "Value (in TB)", newMapSize.TBytes())
	log.Info("Setting flags to the value in mdbx.dat.", "Value", in.Flags)

	return opts.Flags(func(flags uint) uint {
		newFlags := int(in.Flags)
		// make sure is not readonly
		newFlags &= ^mdbx2.Readonly
		newFlags |= mdbx2.WriteMap
		return uint(newFlags)
	}).PageSize(uint64(in.PageSize)).MapSize(newMapSize).Open(ctx)
}

func main() {
	log := logv3.New()
	log.SetHandler(logv3.LvlFilterHandler(logv3.LvlDebug, logv3.StdoutHandler))

	args := os.Args[1:]
	if len(args) != 1 {
		log.Error("Usage: smt-db-split <db_path>")
		os.Exit(1)
	}

	kv.InitStandaloneSMT(true)

	dbMainDBPath := args[0] + "/chaindata"
	dbSMTDBPath := args[0] + "/smt"

	// mdbx databases (mdbx.dat)
	if _, err := os.Stat(dbMainDBPath + "/mdbx.dat"); os.IsNotExist(err) {
		log.Error("DB path does not exist", "error", err)
		os.Exit(1)
	}
	if _, err := os.Stat(dbSMTDBPath + "/mdbx.dat"); os.IsNotExist(err) {
		log.Error("SMT DB path does not exist", "error", err)
		os.Exit(1)
	}

	// delete SMT tables from chaindb
	log.Info("Start dropping SMT tables from chaindb ...")
	chaindb, err := openDBWithOpts(kv.ChainDB, dbMainDBPath, log)
	if err != nil {
		log.Error("Failed to open chaindb", "error", err)
		os.Exit(1)
	}
	defer chaindb.Close()
	ctx := context.Background()
	txchain, err := chaindb.BeginRw(ctx)
	if err != nil {
		log.Error("Failed to start transaction for chaindb", "error", err)
		os.Exit(1)
	}
	defer txchain.Rollback()
	for _, bucket := range db.HermezSmtTables {
		err = txchain.DropBucket(bucket)
		if err != nil {
			log.Error("Failed to drop SMT", "bucket", bucket, "from chaindb. Error", err)
			txchain.Rollback()
			os.Exit(1)
		}
	}
	err = txchain.Commit()
	if err != nil {
		log.Error("Failed to commit chaindb", "error", err)
		txchain.Rollback()
		os.Exit(1)
	}
	log.Info("Done dropping SMT tables from chaindb.")
	// <-- end of delete SMT tables from chaindb

	// delete chaindb tables from SMT DB
	log.Info("Start dropping ChainDB tables from SMT DB ...")
	smtdb, err := openDBWithOpts(kv.SmtDB, dbSMTDBPath, log)
	if err != nil {
		log.Error("Failed to open SMT DB", "error", err)
		os.Exit(1)
	}
	defer smtdb.Close()
	txsmt, err := smtdb.BeginRw(ctx)
	if err != nil {
		log.Error("Failed to smtdb.BeginRw", "error", err)
		os.Exit(1)
	}
	defer txsmt.Rollback()
	for _, bucket := range kv.ChaindataTables {
		err = txsmt.DropBucket(bucket)
		if err != nil {
			log.Error("Failed to drop chaindb", "bucket", bucket, "from SMT db. Error", err)
			txsmt.Rollback()
			os.Exit(1)
		}
	}
	err = txsmt.Commit()
	if err != nil {
		log.Error("Failed to commit smtdb", "error", err)
		txsmt.Rollback()
		os.Exit(1)
	}
	log.Info("Done dropping ChainDB tables from SMT DB.")
	// <-- end of delete chaindb tables from SMT DB

	log.Info("Splitting done.")
}
