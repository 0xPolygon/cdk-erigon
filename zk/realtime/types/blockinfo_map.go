package types

import (
	"path/filepath"
	"sync"

	ethTypes "github.com/ledgerwatch/erigon/core/types"
)

type BlockInfo struct {
	Header  *ethTypes.Header
	TxCount int64
}

type BlockInfoMap struct {
	blockInfos map[uint64]*BlockInfo
	mu         sync.RWMutex
}

func NewBlockInfoMap(size int) *BlockInfoMap {
	return &BlockInfoMap{
		blockInfos: make(map[uint64]*BlockInfo, size),
	}
}

func (bm *BlockInfoMap) Get(blockNum uint64) (*ethTypes.Header, int64, bool) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	blockInfo, exists := bm.blockInfos[blockNum]
	if exists {
		return blockInfo.Header, blockInfo.TxCount, true
	}
	return nil, 0, exists
}

func (bm *BlockInfoMap) PutHeader(blockNum uint64, header *ethTypes.Header, prevTxCount int64) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.blockInfos[blockNum] = &BlockInfo{
		Header:  header,
		TxCount: -1,
	}

	// Update previous block header tx count
	prevBlockNum := blockNum - 1
	blockInfo, exists := bm.blockInfos[prevBlockNum]
	if exists {
		blockInfo.TxCount = prevTxCount
	}
}

func (bm *BlockInfoMap) Delete(blockNum uint64) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	delete(bm.blockInfos, blockNum)
}

func (bm *BlockInfoMap) Clear() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	for k := range bm.blockInfos {
		delete(bm.blockInfos, k)
	}
}

// -------------- Debug operations --------------
func (bm *BlockInfoMap) DebugDumpToFile(cacheDumpPath string) error {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	return WriteToJSON(filepath.Join(cacheDumpPath, "block_info_map.json"), bm.blockInfos)
}
