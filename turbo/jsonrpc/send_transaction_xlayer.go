package jsonrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	txpool2 "github.com/ledgerwatch/erigon/zk/txpool"

	"github.com/ledgerwatch/erigon-lib/chain"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	txPoolProto "github.com/ledgerwatch/erigon-lib/gointerfaces/txpool"

	"github.com/ledgerwatch/erigon-lib/kv"
	utils2 "github.com/ledgerwatch/erigon/cmd/utils"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/turbo/rpchelper"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/erigon/zk/utils"
	"github.com/ledgerwatch/erigon/zkevm/log"
)

type txRequest struct {
	ctx        context.Context
	encodedTx  hexutility.Bytes
	resultChan chan txResult
}

type txResult struct {
	hash common.Hash
	err  error
}

func (api *APIImpl) sendRawTransactionBulk(ctx context.Context, encodedTx hexutility.Bytes) (common.Hash, error) {
	t := utils.StartTimer("rpc", "sendrawtransaction")
	defer t.LogTimer()
	tx, err := api.db.BeginRo(ctx)
	if err != nil {
		return common.Hash{}, err
	}
	defer tx.Rollback()

	cc, err := api.chainConfig(ctx, tx)
	if err != nil {
		return common.Hash{}, err
	}
	chainId := cc.ChainID

	// [zkevm] - proxy the request if the chainID is ZK and not a sequencer
	if api.isZkNonSequencer(chainId) {
		// [zkevm] - proxy the request to the pool manager if the pool manager is set
		if api.isPoolManagerAddressSet() {
			return api.sendTxZk(api.PoolManagerUrl, encodedTx, chainId.Uint64())
		}

		return api.sendTxZk(api.l2RpcUrl, encodedTx, chainId.Uint64())
	}

	resultChan := make(chan txResult, 1)
	req := txRequest{
		ctx:        ctx,
		encodedTx:  encodedTx,
		resultChan: resultChan,
	}

	select {
	case api.txChan <- req:
		select {
		case res := <-resultChan:
			return res.hash, res.err
		case <-ctx.Done():
			return common.Hash{}, ctx.Err()
		}
	case <-ctx.Done():
		return common.Hash{}, ctx.Err()
	}
}

func (api *APIImpl) worker() {
	var txBulk []txRequest
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigc
		cancel()
	}()

	go api.listenApollo(ctx)

	txBulkMtx := new(sync.Mutex)
	bulkProcessCh := make(chan struct{})

	getTxAndBulkProcess := func() {
		var txBulkToProcess []txRequest
		txBulkMtx.Lock()
		if len(txBulk) > 0 {
			txBulkToProcess, txBulk = txBulk, nil
		}
		txBulkMtx.Unlock()
		if len(txBulkToProcess) > 0 {
			err := api.processBulk(txBulkToProcess)
			if err != nil {
				log.Error("process bulk failed", "err", err)
			}
		}
	}

	go func() {
		for {
			select {
			case req := <-api.txChan:
				txBulkMtx.Lock()
				txBulk = append(txBulk, req)
				txBulkLen := len(txBulk)
				txBulkMtx.Unlock()
				if !api.EnableNotify && txBulkLen >= api.BulkAddTxsSize {
					bulkProcessCh <- struct{}{}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if api.BulkAddTxsWaitTime == 0 {
		api.BulkAddTxsWaitTime = utils2.BulkAddTxsWaitTimeFlag.Value
	}
	ticker := time.NewTicker(api.BulkAddTxsWaitTime)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if api.EnableNotify && txpool2.IsAcquireTxPoolLock() {
				continue
			}
			getTxAndBulkProcess()
		case <-bulkProcessCh:
			getTxAndBulkProcess()
		case <-ctx.Done():
			return
		}
	}
}

func (api *APIImpl) processBulk(bulk []txRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := api.db.BeginRo(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	cc, err := api.chainConfig(ctx, tx)
	if err != nil {
		return err
	}
	chainId := cc.ChainID

	latestBlockNumber, err := rpchelper.GetLatestFinishedBlockNumber(tx)
	if err != nil {
		return err
	}

	header, err := api.blockByNumber(ctx, rpc.BlockNumber(latestBlockNumber), tx)
	if err != nil {
		return err
	}

	// now get the sender and put a lock in place for them
	signer := types.MakeSigner(cc, latestBlockNumber, header.Time())

	var rlpTxs [][]byte
	var results []txResult
	for _, req := range bulk {
		hash, err := api.validateTransaction(req.ctx, req.encodedTx, tx, cc, signer, chainId, header)
		if err != nil {
			log.Error("validateTransaction failed", "err", err)
			if req.resultChan != nil {
				req.resultChan <- txResult{hash: common.Hash{}, err: err}
			}
			continue
		}
		rlpTxs = append(rlpTxs, req.encodedTx)
		results = append(results, txResult{hash: hash, err: nil})
	}

	if len(rlpTxs) > 0 {
		res, err := api.txPool.Add(ctx, &txPoolProto.AddRequest{RlpTxs: rlpTxs})
		if err != nil {
			for i := range rlpTxs {
				if results[i].err == nil && results[i].hash != (common.Hash{}) {
					results[i].err = err
				}
				if bulk[i].resultChan != nil {
					bulk[i].resultChan <- results[i]
				}
			}
			return err
		}

		for i, result := range res.Imported {
			if result != txPoolProto.ImportResult_SUCCESS {
				results[i].err = fmt.Errorf("%s: %s", txPoolProto.ImportResult_name[int32(result)], res.Errors[i])
			}
			if bulk[i].resultChan != nil {
				bulk[i].resultChan <- results[i]
			}
		}
	}

	return nil
}

func (api *APIImpl) validateTransaction(ctx context.Context, encodedTx hexutility.Bytes, tx kv.Tx, cc *chain.Config, signer *types.Signer, chainId *big.Int, header *types.Block) (common.Hash, error) {
	txn, err := types.DecodeWrappedTransaction(encodedTx)
	if err != nil {
		return common.Hash{}, err
	}

	sender, err := txn.Sender(*signer)
	if err != nil {
		return common.Hash{}, err
	}
	api.SenderLocks.AddLock(sender)
	defer api.SenderLocks.ReleaseLock(sender)

	if txn.Type() != types.LegacyTxType {
		latestBlock, err := api.blockByNumber(ctx, rpc.LatestBlockNumber, tx)
		if err != nil {
			return common.Hash{}, err
		}
		if !cc.IsLondon(latestBlock.NumberU64()) {
			return common.Hash{}, errors.New("only legacy transactions are supported")
		}
		if txn.Type() == types.BlobTxType {
			return common.Hash{}, errors.New("blob transactions are not supported")
		}
	}

	// check if the price is too low if we are set to reject low gas price transactions
	if api.RejectLowGasPriceTransactions &&
		ShouldRejectLowGasPrice(
			txn.GetPrice().ToBig(),
			api.gasTracker.GetLowestPrice(),
			api.RejectLowGasPriceTolerance,
		) {
		return common.Hash{}, errors.New("transaction price is too low")
	}

	// If the transaction fee cap is already specified, ensure the
	// fee of the given transaction is _reasonable_.
	if err := checkTxFee(txn.GetPrice().ToBig(), txn.GetGas(), api.FeeCap); err != nil {
		return common.Hash{}, err
	}
	if !api.AllowPreEIP155Transactions && !txn.Protected() && !api.AllowUnprotectedTxs {
		return common.Hash{}, errors.New("only replay-protected (EIP-155) transactions allowed over RPC")
	}

	if txn.Protected() {
		txnChainId := txn.GetChainID()
		if chainId.Cmp(txnChainId.ToBig()) != 0 {
			return common.Hash{}, fmt.Errorf("invalid chain id, expected: %d got: %d", chainId, *txnChainId)
		}
	}

	hash := txn.Hash()
	// [zkevm] - check if the transaction is a bad one
	hermezDb := hermez_db.NewHermezDbReader(tx)
	badTxHashCounter, err := hermezDb.GetBadTxHashCounter(hash)
	if err != nil {
		return common.Hash{}, err
	}
	if badTxHashCounter >= api.BadTxAllowance {
		return common.Hash{}, errors.New("transaction uses too many counters to fit into a bulk")
	}

	if len(api.PreRunList) > 0 && utils2.CheckAddressExists(api.PreRunList, sender) {
		api.preRun(txn, chainId)
	}

	return hash, nil
}
