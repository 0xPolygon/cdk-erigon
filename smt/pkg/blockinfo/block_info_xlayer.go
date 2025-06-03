package blockinfo

import (
	"context"
	"fmt"

	"github.com/ledgerwatch/log/v3"

	"github.com/ledgerwatch/erigon/smt/pkg/smt"
	zktx "github.com/ledgerwatch/erigon/zk/tx"

	"github.com/ledgerwatch/erigon-lib/common"
)

var initBlockInfoTreeConcurrent bool

func SetUseBlockInfoTree(value bool) {
	log.Info(fmt.Sprintf("using concurrent block info tree calculation: %v\n", value))
	initBlockInfoTreeConcurrent = value
}

// BuildBlockInfoTreeSerial is a serial implementation of block info tree generation
// used for testing and comparison with the parallel implementation
func BuildBlockInfoTreeSerial(
	coinbase *common.Address,
	blockNumber,
	blockTime,
	blockGasLimit,
	blockGasUsed uint64,
	ger common.Hash,
	l1BlockHash common.Hash,
	previousStateRoot common.Hash,
	transactionInfos *[]ExecutedTxInfo,
) (*common.Hash, error) {
	infoTree := NewBlockInfoTree()
	keys, vals, err := infoTree.GenerateBlockHeader(&previousStateRoot, coinbase, blockNumber, blockGasLimit, blockTime, &ger, &l1BlockHash)
	if err != nil {
		return nil, err
	}

	log.Trace("info-tree-header",
		"blockNumber", blockNumber,
		"previousStateRoot", previousStateRoot.String(),
		"coinbase", coinbase.String(),
		"blockGasLimit", blockGasLimit,
		"blockGasUsed", blockGasUsed,
		"blockTime", blockTime,
		"ger", ger.String(),
		"l1BlockHash", l1BlockHash.String(),
	)
	var logIndex int64 = 0
	for i, txInfo := range *transactionInfos {
		receipt := txInfo.Receipt
		t := txInfo.Tx

		l2TxHash, err := zktx.ComputeL2TxHash(
			t.GetChainID().ToBig(),
			t.GetValue(),
			t.GetPrice(),
			t.GetNonce(),
			t.GetGas(),
			t.GetTo(),
			txInfo.Signer,
			t.GetData(),
		)
		if err != nil {
			return nil, err
		}

		log.Trace("info-tree-tx", "block", blockNumber, "idx", i, "hash", l2TxHash.String())

		genKeys, genVals, err := infoTree.GenerateBlockTxKeysVals(&l2TxHash, i, receipt, logIndex, receipt.CumulativeGasUsed, txInfo.EffectiveGasPrice)
		if err != nil {
			return nil, err
		}
		keys = append(keys, genKeys...)
		vals = append(vals, genVals...)

		logIndex += int64(len(receipt.Logs))
	}

	key, val, err := generateBlockGasUsed(blockGasUsed)
	if err != nil {
		return nil, err
	}
	keys = append(keys, key)
	vals = append(vals, val)

	insertBatchCfg := smt.NewInsertBatchConfig(context.Background(), "block_info_tree", false)
	root, err := infoTree.smt.InsertBatch(insertBatchCfg, keys, vals, nil, nil)
	if err != nil {
		return nil, err
	}
	rootHash := common.BigToHash(root.NewRootScalar.ToBigInt())

	log.Trace("info-tree-root", "block", blockNumber, "root", rootHash.String())

	return &rootHash, nil
}
