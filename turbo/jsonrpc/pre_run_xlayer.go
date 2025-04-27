package jsonrpc

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ledgerwatch/erigon-lib/chain"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/rpc"
	ethapi2 "github.com/ledgerwatch/erigon/turbo/adapter/ethapi"
	"github.com/ledgerwatch/erigon/turbo/rpchelper"
	"github.com/ledgerwatch/erigon/turbo/transactions"
	"github.com/ledgerwatch/log/v3"
)

type PreRunProcessor struct {
	taskChan chan preRunTask
	api      *APIImpl
}

type preRunTask struct {
	txn     types.Transaction
	chainId *big.Int
}

func NewPreRunProcessor(api *APIImpl, chanNum int, taskNum int) *PreRunProcessor {
	p := &PreRunProcessor{
		taskChan: make(chan preRunTask, chanNum),
		api:      api,
	}

	for i := 0; i < taskNum; i++ {
		go p.processLoop()
	}

	return p
}

func (p *PreRunProcessor) processLoop() {
	for task := range p.taskChan {
		_, _ = p.api.preRunWorker(task.txn, task.chainId)
	}
}

func (p *PreRunProcessor) Submit(txn types.Transaction, chainId *big.Int) {
	select {
	case p.taskChan <- preRunTask{txn: txn, chainId: chainId}:
	default:
		log.Warn("PreRun task queue is full, skipping preRun")
	}
}

func (api *APIImpl) initPreRunWorkers(chanNum int, taskNum int) {
	api.preRunProcessor = NewPreRunProcessor(api, chanNum, taskNum)
}

func (api *APIImpl) preRun(txn types.Transaction, chainId *big.Int) {
	api.preRunProcessor.Submit(txn, chainId)
}

func (api *APIImpl) preRunWorker(txn types.Transaction, chainId *big.Int) (hexutil.Uint64, error) {
	signer := types.LatestSignerForChainID(chainId)
	fromAddress, err := txn.Sender(*signer)
	if err != nil {
		return 0, err
	}
	fromAddressHex := libcommon.HexToAddress(fromAddress.String())
	gas := txn.GetGas()
	gp := new(big.Int).SetBytes(txn.GetPrice().Bytes())
	newGP := (*hexutil.Big)(gp)
	value := new(big.Int).SetBytes(txn.GetValue().Bytes())
	newValue := (*hexutil.Big)(value)
	nonce := txn.GetNonce()

	data := txn.GetData()
	hexData := hexutil.Bytes(data)
	hexUtilityData := (*hexutility.Bytes)(&hexData)
	args := ethapi2.CallArgs{
		From:     &fromAddressHex,
		To:       txn.GetTo(),
		Gas:      (*hexutil.Uint64)(&gas),
		GasPrice: newGP,
		Value:    newValue,
		Nonce:    (*hexutil.Uint64)(&nonce),
		Data:     hexUtilityData,
		Input:    hexUtilityData,
		ChainID:  (*hexutil.Big)(chainId),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dbtx, err := api.db.BeginRo(ctx)
	if err != nil {
		return 0, err
	}
	defer dbtx.Rollback()

	var dbtxsmt kv.Tx = nil
	if api.dbsmt != nil {
		dbtxsmt, err = api.dbsmt.BeginRo(ctx)
		if err != nil {
			return 0, err
		}
		defer dbtxsmt.Rollback()
	}

	block, stateReader, chainConfig, err := api.prepareBlockAndState(ctx, dbtx)
	if err != nil {
		return 0, err
	}

	caller, err := transactions.NewReusableCaller(
		api.engine(),
		stateReader,
		nil,
		block.HeaderNoCopy(),
		args,
		api.GasCap,
		latestNumOrHash,
		dbtx,
		dbtxsmt,
		api._blockReader,
		chainConfig,
		api.evmCallTimeout,
		api.VirtualCountersSmtReduction,
		false,
	)
	if err != nil {
		return 0, err
	}

	fixedGas := uint64(10000000)
	result, err := caller.DoCallWithNewGas(ctx, fixedGas)
	if err != nil {
		return 0, err
	}

	if result.Failed() {
		log.Error("Prerun execution failed", "gas", fixedGas, "from", fromAddress, "to", txn.GetTo(), "revert", result.Revert())
		if len(result.Revert()) > 0 {
			return 0, ethapi2.NewRevertError(result)
		}
		return 0, result.Err
	}

	return hexutil.Uint64(fixedGas), nil
}

func (api *APIImpl) prepareBlockAndState(ctx context.Context, dbtx kv.Tx) (*types.Block, state.StateReader, *chain.Config, error) {
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber)
	latestCanBlockNumber, latestCanHash, isLatest, err := rpchelper.GetCanonicalBlockNumber_zkevm(bNrOrHash, dbtx, api.filters)
	if err != nil {
		return nil, nil, nil, err
	}

	block := api.tryBlockFromLru(latestCanHash)
	if block == nil {
		var err error
		block, err = api.blockWithSenders(ctx, dbtx, latestCanHash, latestCanBlockNumber)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	if block == nil {
		return nil, nil, nil, fmt.Errorf("could not find latest block")
	}

	chainConfig, err := api.chainConfig(ctx, dbtx)
	if err != nil {
		return nil, nil, nil, err
	}

	stateReader, err := rpchelper.CreateStateReaderFromBlockNumber(
		ctx, dbtx, latestCanBlockNumber, isLatest, 0,
		api.stateCache, api.historyV3(dbtx), chainConfig.ChainName,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	return block, stateReader, chainConfig, nil
}
