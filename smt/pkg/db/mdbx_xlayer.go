package db

import (
	"github.com/ledgerwatch/erigon-lib/kv/membatch"
	"github.com/ledgerwatch/erigon/smt/pkg/utils"
)

func (m *EriDb) SetCache(smtCachedMapValue map[string]map[string][]byte) {
	if smtCachedMapValue == nil {
		smtCachedMapValue = make(map[string]map[string][]byte)
	}

	mapCache, ok := m.tx.(*membatch.Mapmutation)
	if !ok {
		return // don't roll back a kvRw tx
	}

	mapCache.SetCache(smtCachedMapValue)
}

func (m *EriDb) RetriveAndCleanCache() map[string]map[string][]byte {
	return nil
}

func (m *EriRoDb) GetLastHeight() (uint64, error) {
	data, err := m.kvTxRoSMT.GetOne(TableStats, []byte(MetaLastHeight))
	if err != nil {
		return 0, err
	}

	return utils.ConvertBytesToUint64(data)
}

func (m *EriDb) SetLastHeight(blockHeight uint64) error {
	v := utils.ConvertUint64ToBytes(blockHeight)
	return m.tx.Put(TableStats, []byte(MetaLastHeight), []byte(v))
}
