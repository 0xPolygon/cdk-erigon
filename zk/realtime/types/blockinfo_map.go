package types

import (
	"path/filepath"
	"sync"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	ethTypes "github.com/ledgerwatch/erigon/core/types"
)

type BlockInfo struct {
	Header  *ethTypes.Header `json:"header"`
	TxCount int64            `json:"txCount"`
	Hash    libcommon.Hash   `json:"hash"`
}

type BlockInfoMap struct {
	blockInfos        map[uint64]*BlockInfo
	blockHashToHeight map[libcommon.Hash]uint64
	mu                sync.RWMutex
}

func NewBlockInfoMap(size int) *BlockInfoMap {
	return &BlockInfoMap{
		blockInfos:        make(map[uint64]*BlockInfo, size),
		blockHashToHeight: make(map[libcommon.Hash]uint64, size),
	}
}

func (bm *BlockInfoMap) Get(blockNum uint64) (*ethTypes.Header, int64, libcommon.Hash, bool) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	blockInfo, exists := bm.blockInfos[blockNum]
	if exists {
		return blockInfo.Header, blockInfo.TxCount, blockInfo.Hash, true
	}
	return nil, 0, libcommon.Hash{}, exists
}

func (bm *BlockInfoMap) GetBlockNumberByHash(blockHash libcommon.Hash) (uint64, bool) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	blockNum, exists := bm.blockHashToHeight[blockHash]
	return blockNum, exists
}

func (bm *BlockInfoMap) PutHeader(blockNum uint64, header *ethTypes.Header, prevBlockInfo *BlockInfo) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.blockInfos[blockNum] = &BlockInfo{
		Header:  header,
		TxCount: -1,
		Hash:    libcommon.Hash{},
	}

	// Update previous block info
	prevBlockNum := blockNum - 1
	if prevBlockInfo != nil {
		bm.blockInfos[prevBlockNum] = prevBlockInfo
		bm.blockHashToHeight[prevBlockInfo.Hash] = prevBlockNum
	}
}

func (bm *BlockInfoMap) Delete(blockNum uint64) {
	_, _, blockhash, exists := bm.Get(blockNum)
	bm.mu.Lock()
	defer bm.mu.Unlock()
	if exists {
		delete(bm.blockHashToHeight, blockhash)
		delete(bm.blockInfos, blockNum)
	}
}

func (bm *BlockInfoMap) Clear() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	for k := range bm.blockInfos {
		delete(bm.blockInfos, k)
	}
	for k := range bm.blockHashToHeight {
		delete(bm.blockHashToHeight, k)
	}
}

// -------------- Debug operations --------------
func (bm *BlockInfoMap) DebugDumpToFile(cacheDumpPath string) error {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	return WriteToJSON(filepath.Join(cacheDumpPath, "block_info_map.json"), bm.blockInfos)
}
