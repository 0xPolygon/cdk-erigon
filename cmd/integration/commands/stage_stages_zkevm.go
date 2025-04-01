package commands

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	common2 "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/wrap"
	"github.com/ledgerwatch/erigon/eth/stagedsync/stages"
	smtdb "github.com/ledgerwatch/erigon/smt/pkg/db"
	"github.com/ledgerwatch/erigon/turbo/debug"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/log/v3"
	"github.com/spf13/cobra"
)

var stateStagesZk = &cobra.Command{
	Use: "state_stages_zkevm",
	Short: `Run all StateStages in loop.
Examples:
state_stages_zkevm --datadir=/datadirs/hermez-mainnet--unwind-batch-no=10  # unwind so the tip is the highest block in batch number 10
state_stages_zkevm --datadir=/datadirs/hermez-mainnet --unwind-batch-no=2 --chain=hermez-bali --log.console.verbosity=4 --datadir-compare=/datadirs/pre-synced-block-100 # unwind to batch 2 and compare with another datadir
		`,
	Example: "go run ./cmd/integration state_stages_zkevm --config=... --verbosity=3 --unwind-batch-no=100",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, _ := common2.RootContext()
		logger := debug.SetupCobra(cmd, "integration")
		kv.InitStandaloneSMT(standaloneSmtDb)
		db, err := openDB(dbCfg(kv.ChainDB, chaindata), true, logger)
		if err != nil {
			logger.Error("Opening DB", "error", err)
			return
		}
		defer db.Close()

		var dbsmt kv.RwDB = nil
		if standaloneSmtDb {
			dbsmt, err = openDB(dbCfg(kv.SmtDB, smtDbPath), false, logger)
			if err != nil {
				logger.Error("Opening SMT DB", "error", err)
				return
			}
			defer dbsmt.Close()
		}

		if err := unwindZk(ctx, db, dbsmt); err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Error(err.Error())
			}
			return
		}

		if len(datadirCompare) > 0 {
			compareDB(db, kv.ChainDB, "chaindata", logger)
			if standaloneSmtDb {
				compareDB(db, kv.SmtDB, "smt", logger)
			}
		}
	},
}

func compareDB(db kv.RwDB, label kv.Label, dbDirName string, logger log.Logger) {
	dbCompare, err := openDB(dbCfg(label, filepath.Join(datadirCompare, dbDirName)), true, logger)
	if err != nil {
		logger.Error("Opening DB", "error", err)
		return
	}
	defer dbCompare.Close()

	diff, err := compareDbs(db, dbCompare, label)
	if err != nil {
		log.Error(err.Error())
		return
	}
	if len(diff) > 0 {
		log.Error("Databases are different")
		for _, d := range diff {
			log.Error(d)
		}
		return
	}
}

func init() {
	withConfig(stateStagesZk)
	withChain(stateStagesZk)
	withDataDir2(stateStagesZk)
	withDataDirCompare(stateStagesZk)
	withUnwind(stateStagesZk)
	withUnwindBatchNo(stateStagesZk) // populates package global flag unwindBatchNo
	withStandaloneSmtDb(stateStagesZk)
	rootCmd.AddCommand(stateStagesZk)
}

// unwindZk unwinds to the batch number set in the unwindBatchNo flag (package global)
func unwindZk(ctx context.Context, db, dbsmt kv.RwDB) error {
	_, _, stateStages := newSyncZk(ctx, db, dbsmt)

	tx, err := db.BeginRw(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var txsmt kv.RwTx = nil
	if dbsmt != nil {
		txsmt, err = dbsmt.BeginRw(ctx)
		if err != nil {
			return err
		}
		defer txsmt.Rollback()
		if err := smtdb.CreateEriDbBuckets(txsmt); err != nil {
			return err
		}
	} else {
		if err := smtdb.CreateEriDbBuckets(tx); err != nil {
			return err
		}
	}

	if err := hermez_db.CreateHermezBuckets(tx); err != nil {
		return err
	}

	stateStages.DisableStages(stages.Snapshots)

	err = stateStages.UnwindToBatch(unwindBatchNo, tx)
	if err != nil {
		return err
	}

	err = stateStages.RunUnwind(db, wrap.TxContainer{Tx: tx, TxSmt: txsmt})
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		if txsmt != nil {
			txsmt.Rollback()
		}
		return err
	}

	if txsmt != nil {
		return txsmt.Commit()
	}
	return nil
}

func compareDbs(db1, db2 kv.RwDB, label kv.Label) ([]string, error) {
	var discrepancies []string

	excludedTables := []string{}
	if label == kv.ChainDB {
		excludedTables = append(excludedTables, kv.Senders)
	}

	var tables []string
	switch label {
	case kv.ChainDB:
		tables = kv.ChaindataTables
	case kv.SmtDB:
		tables = kv.TablesSmt
	default:
		return nil, fmt.Errorf("unsupported label %s", label)
	}

LOOP:
	for _, table := range tables {
		// if table is excluded, skip it
		for _, excludedTable := range excludedTables {
			if table == excludedTable {
				continue LOOP
			}
		}

		count1, err := countKeysInDb(db1, table)
		if err != nil {
			return nil, fmt.Errorf("error counting keys in unwound db for table %s: %w", table, err)
		}

		count2, err := countKeysInDb(db2, table)
		if err != nil {
			return nil, fmt.Errorf("error counting keys in comparison db for table %s: %w", table, err)
		}

		if count1 != count2 {
			discrepancies = append(discrepancies, fmt.Sprintf("Table %s: Unwound DB has %d entries, Comparison DB has %d entries", table, count1, count2))
		}
	}

	return discrepancies, nil
}

func countKeysInDb(db kv.RwDB, table string) (uint64, error) {
	txn, err := db.BeginRo(context.Background())
	if err != nil {
		return 0, err
	}
	defer txn.Rollback()

	count, err := txn.BucketSize(table)
	if err != nil {
		return 0, err
	}

	return count, nil
}
