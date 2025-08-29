package types

import (
	"path/filepath"
	"sync"

	"github.com/ledgerwatch/erigon-lib/common"
	ethTypes "github.com/ledgerwatch/erigon/core/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

type TxInfo struct {
	BlockNumber uint64
	Tx          ethTypes.Transaction
	Receipt     *ethTypes.Receipt
	InnerTxs    []*zktypes.InnerTx
	Changeset   *Changeset
}

type TxInfoMap struct {
	txInfos  map[common.Hash]TxInfo
	blockTxs map[uint64]map[common.Hash]struct{}
	mu       sync.RWMutex
}

func NewTxInfoMap(blockCacheSize int, txCacheSize int) *TxInfoMap {
	return &TxInfoMap{
		txInfos:  make(map[common.Hash]TxInfo, txCacheSize),
		blockTxs: make(map[uint64]map[common.Hash]struct{}, blockCacheSize),
	}
}

func (rm *TxInfoMap) Put(blockNumber uint64, txHash common.Hash, tx ethTypes.Transaction, receipt *ethTypes.Receipt, innerTxs []*zktypes.InnerTx) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	txInfo := TxInfo{
		BlockNumber: blockNumber,
		Tx:          tx,
		Receipt:     receipt,
		InnerTxs:    innerTxs,
	}

	rm.txInfos[txHash] = txInfo
	if _, exists := rm.blockTxs[blockNumber]; !exists {
		rm.blockTxs[blockNumber] = make(map[common.Hash]struct{})
	}
	rm.blockTxs[blockNumber][txHash] = struct{}{}
}

func (rm *TxInfoMap) Delete(blockNumber uint64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	txHashes, exists := rm.blockTxs[blockNumber]
	if !exists {
		return
	}
	for txHash := range txHashes {
		delete(rm.txInfos, txHash)
	}
	delete(rm.blockTxs, blockNumber)
}

func (rm *TxInfoMap) GetTx(txHash common.Hash) (ethTypes.Transaction, *ethTypes.Receipt, uint64, []*zktypes.InnerTx, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	txInfo, exists := rm.txInfos[txHash]
	return txInfo.Tx, txInfo.Receipt, txInfo.BlockNumber, txInfo.InnerTxs, exists
}

func (rm *TxInfoMap) GetBlockTxs(blockNumber uint64) []common.Hash {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	hashes := make([]common.Hash, 0)
	hashSet, exists := rm.blockTxs[blockNumber]
	if !exists {
		return hashes
	}

	for hash := range hashSet {
		hashes = append(hashes, hash)
	}

	return hashes
}

func (rm *TxInfoMap) Clear() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	for k := range rm.txInfos {
		delete(rm.txInfos, k)
	}

	for k := range rm.blockTxs {
		delete(rm.blockTxs, k)
	}
}

// -------------- Debug operations --------------
func (rm *TxInfoMap) DebugDumpToFile(cacheDumpPath string) error {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return WriteToJSON(filepath.Join(cacheDumpPath, "tx_info_map.json"), rm.txInfos)
}
