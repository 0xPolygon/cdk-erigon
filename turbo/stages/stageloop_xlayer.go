package stages

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ledgerwatch/erigon-lib/kv/mdbx"
	"github.com/ledgerwatch/erigon-lib/kv/membatch"

	"github.com/ledgerwatch/log/v3"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/zk/sequencer"

	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/eth/stagedsync"
	"github.com/ledgerwatch/erigon/zk/smt"
	zkStages "github.com/ledgerwatch/erigon/zk/stages"
)

func AsyncFlushSmtData(ctx context.Context,
	_db kv.RwDB,
	s *stagedsync.Sync,
	config ethconfig.XLayerConfig,
	logger log.Logger,
	smtFlushDoneCh chan struct{},
) {
	if !sequencer.IsSequencer() || !config.EnableAsyncCommit {
		logger.Info("AsyncFlushSmtData skipped",
			"isSequencer", sequencer.IsSequencer(),
			"enableAsyncCommit", config.EnableAsyncCommit)
		return
	}

	db, ok := _db.(*mdbx.MdbxKV)
	if !ok {
		logger.Error("invalid database type, expected *mdbx.MdbxKV", "type", fmt.Sprintf("%T", _db))
		return
	}

	cache := s.GetCache()
	const maxWorkers = 6
	workerPool := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	defer func() {
		logger.Info("Waiting for all flush operations to complete...")
		wg.Wait()
		logger.Info("All flush operations completed, exiting AsyncFlushSmtData")
		smtFlushDoneCh <- struct{}{}
	}()

	if config.SequencerReplay {
		replayDone, _ := zkStages.WaitResequenceBatchDone()
		go func() {
			<-replayDone
			logger.Info("AsyncFlushSmtData received replay done signal")
			handleShutdown(s, config, &wg, workerPool, db, cache, logger)
			logger.Info("Waiting for all flush operations to complete...")
			wg.Wait()
			logger.Info("All flush operations completed, exiting AsyncFlushSmtData")
			os.Exit(0)
		}()
	}

	for {
		select {
		case saveData, ok := <-cache.SmtCacheDataCh:
			if !ok {
				logger.Info("SmtCacheCh closed, stopping AsyncFlushSmtData")
				return
			}
			dispatchFlushTask(context.Background(), &wg, workerPool, db, cache, saveData, logger)

		case <-ctx.Done():
			logger.Info("AsyncFlushSmtData received stop signal", "reason", ctx.Err())
			handleShutdown(s, config, &wg, workerPool, db, cache, logger)
			return
		}
	}
}

func dispatchFlushTask(ctx context.Context, wg *sync.WaitGroup, workerPool chan struct{},
	db *mdbx.MdbxKV, cache *smt.SmtCache, saveData smt.SmtCacheSave, logger log.Logger) {
	select {
	case workerPool <- struct{}{}: // get working slot
		wg.Add(1)
		go func() {
			now := time.Now()
			defer func() {
				<-workerPool // release working slot
				wg.Done()
				logger.Info("FlushDataToDB finished", "cost", time.Since(now), "blockHeight", saveData.BlockHeight)
			}()
			FlushDataToDB(ctx, db, logger, cache, saveData)
		}()

	case <-ctx.Done():
		logger.Debug("Skipped flush task due to context cancellation", "reason", ctx.Err())
	}
}

func handleShutdown(s *stagedsync.Sync, config ethconfig.XLayerConfig,
	wg *sync.WaitGroup, workerPool chan struct{}, db *mdbx.MdbxKV, cache *smt.SmtCache, logger log.Logger) {
	s.FlushSmtCache(config.StandaloneSMTDatabase, true)

	for {
		select {
		case saveData, ok := <-cache.SmtCacheDataCh:
			if !ok {
				logger.Info("SmtCacheCh closed during shutdown")
				return
			}
			dispatchFlushTask(context.Background(), wg, workerPool, db, cache, saveData, logger)
		default:
			logger.Debug("No more data in SmtCacheDataCh during shutdown")
			return
		}
	}
}

func FlushDataToDB(ctx context.Context, db *mdbx.MdbxKV, logger log.Logger, cache *smt.SmtCache, saveData smt.SmtCacheSave) {
	err := db.Batch(func(tx kv.RwTx) error {
		batch := membatch.NewHashBatch(tx, ctx.Done(), "", logger)
		defer batch.Close()
		batch.SetCache(saveData.SmtData)
		return batch.Flush(ctx, tx)
	})
	if err != nil {
		logger.Error("Failed to flush data to DB", "error", err)
		return
	}
	cache.TruncateSmtCacheList(saveData.BlockHeight)
}
