package stagedsync

func (s *StageState) GetSmtCache() map[string]map[string][]byte {
	return s.state.GetSmtCache()
}

func (s *StageState) GetSmtHistorySnapshotCache(blockNumber uint64) map[string]map[string][]byte {
	return s.state.GetSmtSnapshotCache(blockNumber)
}

func (s *StageState) SetSmtCache(blockNumber uint64, blockCache map[string]map[string][]byte) {
	s.state.SetSmtCache(blockNumber, blockCache)
	s.BlockNumber = blockNumber
}

func (s *StageState) FlushSmtCache(batchPush, grace bool) error {
	return s.state.FlushSmtCache(batchPush, grace)
}

func (s *StageState) FlushSmtCacheWait() {
	s.state.FlushSmtCacheWait()
}

func (s *StageState) FlushSmtCacheSignalInc() {
	s.state.FlushSmtCacheSignalInc()
}

func (s *StageState) FlushSmtCacheDone() {
	s.state.FlushSmtCacheDone()
}

func (s *StageState) ResetCurrentBatchCache(resetBlockHeight uint64) {
	s.state.ResetCurrentBatchCache(resetBlockHeight)
}
