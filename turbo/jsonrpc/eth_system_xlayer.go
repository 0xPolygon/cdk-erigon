package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	proto_txpool "github.com/ledgerwatch/erigon-lib/gointerfaces/txpool"
	"github.com/ledgerwatch/erigon/core/rawdb"
	"github.com/ledgerwatch/erigon/eth/gasprice"
	"github.com/ledgerwatch/erigon/ethclient"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/zk/apollo"
	"github.com/ledgerwatch/erigon/zk/metrics"
	"github.com/ledgerwatch/erigon/zk/sequencer"
	"github.com/ledgerwatch/erigon/zkevm/jsonrpc/client"
	"github.com/ledgerwatch/log/v3"
)

func (api *APIImpl) gasPriceXL(ctx context.Context) (*hexutil.Big, error) {
	if sequencer.IsSequencer() {
		return api.gasPriceNonRedirectedXL(ctx)
	}

	price, err := api.getGPFromTrustedNode("eth_gasPrice")
	if err != nil {
		log.Error(fmt.Sprintf("eth_gasPrice error: %v", err))
		return (*hexutil.Big)(api.L2GasPricer.GetConfig().Default), nil
	}

	return (*hexutil.Big)(price), nil
}

func (api *APIImpl) gasPriceNonRedirectedXL(ctx context.Context) (*hexutil.Big, error) {
	_, gasResult := api.gasCache.GetLatest()
	return (*hexutil.Big)(gasResult), nil
}

func (api *APIImpl) isCongested(ctx context.Context) bool {

	latestBlockTxNum, err := api.getLatestBlockTxNum(ctx)
	if err != nil {
		return false
	}
	isLatestBlockEmpty := latestBlockTxNum == 0

	poolStatus, err := api.txPool.Status(ctx, &proto_txpool.StatusRequest{})
	if err != nil {
		return false
	}

	isPendingTxCongested := int(poolStatus.PendingCount) >= api.L2GasPricer.GetConfig().XLayer.CongestionThreshold

	return !isLatestBlockEmpty && isPendingTxCongested
}

func (api *APIImpl) getLatestBlockTxNum(ctx context.Context) (int, error) {
	tx, err := api.db.BeginRo(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	b, err := api.blockByNumber(ctx, rpc.LatestBlockNumber, tx)
	if err != nil {
		return 0, err
	}
	return len(b.Transactions()), nil
}

func (api *APIImpl) getGPFromTrustedNode(method string) (*big.Int, error) {
	res, err := client.JSONRPCCall(api.l2RpcUrl, method)
	if err != nil {
		return nil, errors.New("failed to get gas price from trusted node")
	}

	if res.Error != nil {
		return nil, errors.New(res.Error.Message)
	}

	var gasPriceStr string
	err = json.Unmarshal(res.Result, &gasPriceStr)
	if err != nil {
		return nil, errors.New("failed to unmarshal gas price from trusted node")
	}

	gasPriceStr = gasPriceStr[2:]
	gp, err := strconv.ParseUint(gasPriceStr, 16, 64)
	if err != nil {
		return nil, errors.New("failed to parse gas price from trusted node")
	}

	return new(big.Int).SetUint64(gp), nil
}

func (api *APIImpl) runL2GasPriceSuggester() {
	ctx := api.L2GasPricer.GetCtx()

	if apollo.IsApolloConfigL2GasPricerEnabled() {
		api.L2GasPricer.UpdateConfig(apollo.GetApolloGasPricerConfig())
	}
	l1gp, err := gasprice.GetL1GasPrice(api.L1RpcUrl)
	// if err != nil, do nothing
	if err == nil {
		api.L2GasPricer.UpdateGasPriceAvg(l1gp)
	}
	updateTimer := time.NewTimer(api.L2GasPricer.GetConfig().XLayer.UpdatePeriod)
	for {
		select {
		case <-ctx.Done():
			log.Info("Finishing l2 gas price suggester...")
			return
		case <-updateTimer.C:
			if apollo.IsApolloConfigL2GasPricerEnabled() {
				api.L2GasPricer.UpdateConfig(apollo.GetApolloGasPricerConfig())
			}
			l1gp, err := gasprice.GetL1GasPrice(api.L1RpcUrl)
			if err == nil {
				api.L2GasPricer.UpdateGasPriceAvg(l1gp)
				api.gasCache.SetLatestRawGP(api.L2GasPricer.GetLastRawGP())
			}
			api.updateDynamicGP(ctx)
			updateTimer.Reset(api.L2GasPricer.GetConfig().XLayer.UpdatePeriod)
		}
	}
}

func (api *APIImpl) updateDynamicGP(ctx context.Context) {
	tx, err := api.db.BeginRo(ctx)
	if err != nil {
		log.Error(fmt.Sprintf("error db.BeginRo: %v", err))
		return
	}
	defer tx.Rollback()
	if err != nil {
		log.Error(fmt.Sprintf("error chainConfig: %v", err))
		return
	}
	oracle := gasprice.NewOracle(NewGasPriceOracleBackend(tx, api.BaseAPI), api.L2GasPricer.GetConfig(), api.gasCache)
	tipcap, err := oracle.SuggestTipCap(ctx)
	if err != nil {
		log.Error(fmt.Sprintf("error SuggestTipCap: %v", err))
		return
	}
	gasResult := big.NewInt(0)
	gasResult.Set(tipcap)
	if head := rawdb.ReadCurrentHeader(tx); head != nil && head.BaseFee != nil {
		gasResult.Add(tipcap, head.BaseFee)
	}

	rgp := api.L2GasPricer.GetLastRawGP()
	if gasResult.Cmp(rgp) < 0 {
		gasResult = new(big.Int).Set(rgp)
	}

	if !api.isCongested(ctx) {
		gasResult = getAvgPrice(rgp, gasResult)
	}

	lasthash, _ := api.gasCache.GetLatest()
	api.gasCache.SetLatest(lasthash, gasResult)

	metrics.RpcDynamicGasPrice.Set(float64(gasResult.Uint64()))
	log.Info(fmt.Sprintf("Updated dynamic gas price: %s", gasResult.String()))
}

func getAvgPrice(low *big.Int, high *big.Int) *big.Int {
	avg := new(big.Int).Add(low, high)
	avg = avg.Quo(avg, big.NewInt(2)) //nolint:gomnd
	return avg
}

func (api *APIImpl) MinGasPrice(ctx context.Context) (*hexutil.Big, error) {
	var minGP *big.Int
	if sequencer.IsSequencer() {
		minGP = api.gasCache.GetMinRawGPMoreRecent()
		return (*hexutil.Big)(minGP), nil
	}

	minGP, err := api.getGPFromTrustedNode("eth_minGasPrice")
	if err != nil {
		log.Error(fmt.Sprintf("eth_minGasPrice error: %v", err))
		return nil, err
	}

	return (*hexutil.Big)(minGP), nil
}

func (api *APIImpl) getBlockGasLimit(ctx context.Context) (*hexutil.Big, error) {

	tx, err := api.db.BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cc, err := api.chainConfig(ctx, tx)
	if err != nil {
		return nil, err
	}
	chainId := cc.ChainID
	if !api.isZkNonSequencer(chainId) {
		gasLimit := big.NewInt(int64(api.BlockGasLimit))
		return (*hexutil.Big)(gasLimit), nil
	}

	client, err := ethclient.DialContext(ctx, api.l2RpcUrl)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	price, err := client.BlockGasLimit(ctx)
	if err != nil {
		return nil, err
	}

	return (*hexutil.Big)(price), nil
}
