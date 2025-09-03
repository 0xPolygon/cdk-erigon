package cache

import (
	"context"
	"fmt"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	ethTypes "github.com/ledgerwatch/erigon/core/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

type StatelessCache struct {
	blockInfoMap *realtimeTypes.BlockInfoMap
	txInfoMap    *realtimeTypes.TxInfoMap
}

func NewStatelessCache(blockCacheSize int, txCacheSize int) *StatelessCache {
	return &StatelessCache{
		blockInfoMap: realtimeTypes.NewBlockInfoMap(blockCacheSize),
		txInfoMap:    realtimeTypes.NewTxInfoMap(blockCacheSize, txCacheSize),
	}
}

func (cache *StatelessCache) Clear() {
	cache.blockInfoMap.Clear()
	cache.txInfoMap.Clear()
}

// -------------- Read operations --------------
func (cache *StatelessCache) GetBlockInfo(blockNum uint64) (*ethTypes.Header, int64, libcommon.Hash, bool) {
	return cache.blockInfoMap.Get(blockNum)
}

func (cache *StatelessCache) GetBlockInfoByHash(blockHash libcommon.Hash) (*ethTypes.Header, int64, libcommon.Hash, bool) {
	blockNum, exists := cache.blockInfoMap.GetBlockNumberByHash(blockHash)
	if !exists {
		return nil, 0, libcommon.Hash{}, false
	}
	return cache.blockInfoMap.Get(blockNum)
}

func (cache *StatelessCache) GetBlockNumberByHash(blockHash libcommon.Hash) (uint64, bool) {
	return cache.blockInfoMap.GetBlockNumberByHash(blockHash)
}

func (cache *StatelessCache) GetTxInfo(txHash libcommon.Hash) (ethTypes.Transaction, *ethTypes.Receipt, uint64, []*zktypes.InnerTx, bool) {
	return cache.txInfoMap.GetTx(txHash)
}

func (cache *StatelessCache) GetBlockTxs(blockNum uint64) ([]libcommon.Hash, bool) {
	if _, _, _, ok := cache.blockInfoMap.Get(blockNum); !ok {
		return nil, false
	}
	return cache.txInfoMap.GetBlockTxs(blockNum), true
}

// -------------- Write operations --------------
func (cache *StatelessCache) PutNewBlockInfo(blockNum uint64, blockInfo *realtimeTypes.BlockInfo) {
	cache.blockInfoMap.PutNewBlockInfo(blockNum, blockInfo)
}

func (cache *StatelessCache) PutConfirmedBlockInfo(blockNum uint64, blockInfo *realtimeTypes.BlockInfo) {
	cache.blockInfoMap.PutConfirmedBlockInfo(blockNum, blockInfo)
}

func (cache *StatelessCache) PutTxInfo(blockNum uint64, txHash libcommon.Hash, tx ethTypes.Transaction, receipt *ethTypes.Receipt, innerTxs []*zktypes.InnerTx) {
	cache.txInfoMap.Put(blockNum, txHash, tx, receipt, innerTxs)
}

func (cache *StatelessCache) DeleteBlock(blockNum uint64) {
	cache.blockInfoMap.Delete(blockNum)
	cache.txInfoMap.Delete(blockNum)
}

// -------------- For HeaderReader --------------
func (cache *StatelessCache) Header(ctx context.Context, tx kv.Getter, hash libcommon.Hash, blockNum uint64) (*ethTypes.Header, error) {
	header, _, _, ok := cache.GetBlockInfo(blockNum)
	if !ok {
		return nil, fmt.Errorf("header not found for block number %d", blockNum)
	}
	return header, nil
}

func (cache *StatelessCache) HeaderByNumber(ctx context.Context, tx kv.Getter, blockNum uint64) (*ethTypes.Header, error) {
	header, _, _, ok := cache.GetBlockInfo(blockNum)
	if !ok {
		return nil, fmt.Errorf("header not found for block number %d", blockNum)
	}
	return header, nil
}

func (cache *StatelessCache) HeaderByHash(ctx context.Context, tx kv.Getter, hash libcommon.Hash) (*ethTypes.Header, error) {
	header, _, _, ok := cache.GetBlockInfoByHash(hash)
	if !ok {
		return nil, fmt.Errorf("header not found for block hash %s", hash.Hex())
	}
	return header, nil
}

func (cache *StatelessCache) ReadAncestor(db kv.Getter, hash libcommon.Hash, number, ancestor uint64, maxNonCanonical *uint64) (libcommon.Hash, uint64) {
	// Unimplemented
	return libcommon.Hash{}, 0
}

func (cache *StatelessCache) HeadersRange(ctx context.Context, walker func(header *ethTypes.Header) error) error {
	// Unimplemented
	return nil
}

func (cache *StatelessCache) Integrity(ctx context.Context) error {
	// Unimplemented
	return nil
}

// -------------- Debug operations --------------
func (cache *StatelessCache) DebugDumpToFile(cacheDumpPath string) error {
	err := cache.blockInfoMap.DebugDumpToFile(cacheDumpPath)
	if err != nil {
		return err
	}
	return cache.txInfoMap.DebugDumpToFile(cacheDumpPath)
}
