package stagedsync

import (
	"github.com/ledgerwatch/erigon/zk/smt"
)

func (s *Sync) GetCache() *smt.SmtCache {
	return s.cache
}

func (s *Sync) GetSmtCache() map[string]map[string][]byte {
	return s.cache.GetSmtCache()
}

func (s *Sync) GetSmtSnapshotCache(blockNumber uint64) map[string]map[string][]byte {
	return s.cache.CascadeGetCurrentBatchSnapshotCache(blockNumber)
}

func (s *Sync) SetSmtCache(blockNumber uint64, blockCache map[string]map[string][]byte) {
	s.cache.SetSmtCache(blockNumber, blockCache)
}

func (s *Sync) FlushSmtCacheWait() {
	s.flushWG.Wait()
}

func (s *Sync) FlushSmtCacheSignalInc() {
	s.flushWG.Add(1)
}

func (s *Sync) FlushSmtCacheDone() {
	s.flushWG.Done()
}

func (s *Sync) FlushSmtCache(batchPush, grace bool) error {
	return s.cache.FlushSmtCache(batchPush, grace)
}

func (s *Sync) ResetCurrentBatchCache(resetBlockHeight uint64) {
	s.cache.ResetCurrentBatch(resetBlockHeight)
}
