package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/ledgerwatch/erigon/cmd/utils"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/node/nodecfg"
	"github.com/ledgerwatch/erigon/smt/pkg/blockinfo"
	"github.com/urfave/cli/v2"
)

func ApplyFlagsForEthXLayerConfig(ctx *cli.Context, cfg *ethconfig.Config) {
	sequencerBlockSealTime := cfg.Zk.SequencerBlockSealTime
	sequencerBatchSealTime := cfg.Zk.SequencerBatchSealTime
	sequencerMaxBlockSealTimeVal := ctx.String(utils.SequencerMaxBlockSealTime.Name)
	sequencerMaxBlockSealTime, err := time.ParseDuration(sequencerMaxBlockSealTimeVal)
	if err != nil || sequencerBlockSealTime > sequencerMaxBlockSealTime || sequencerMaxBlockSealTime > sequencerBatchSealTime {
		panic(fmt.Sprintf("Got error: %v, sequencer-block-seal-time: %s, sequencer-max-block-seal-time: %s, sequencer-batch-seal-time: %s", err, sequencerBlockSealTime, sequencerMaxBlockSealTime, sequencerBatchSealTime))
	}

	sequencerBatchCounterPercentage := ctx.Int(utils.SequencerBatchCounterPercentage.Name)
	err = vm.SetBatchCounterLimitPercentage(sequencerBatchCounterPercentage)
	if err != nil {
		panic(fmt.Sprintf("Got error: %v, sequencer-batch-counter-percentage: %d", err, sequencerBatchCounterPercentage))
	}

	cfg.XLayer = ethconfig.XLayerConfig{
		Apollo: ethconfig.ApolloClientConfig{
			Enable: ctx.Bool(utils.ApolloEnableFlag.Name),
			IP:     ctx.String(utils.ApolloIPAddr.Name),
			AppID:  ctx.String(utils.ApolloAppId.Name),
		},
		Nacos: ethconfig.NacosConfig{
			URLs:               ctx.String(utils.NacosURLsFlag.Name),
			NamespaceId:        ctx.String(utils.NacosNamespaceIdFlag.Name),
			ApplicationName:    ctx.String(utils.NacosApplicationNameFlag.Name),
			ExternalListenAddr: ctx.String(utils.NacosExternalListenAddrFlag.Name),
		},
		EnableInnerTx:                     ctx.Bool(utils.AllowInternalTransactions.Name),
		SequencerBatchSleepDuration:       ctx.Duration(utils.SequencerBatchSleepDuration.Name),
		SequencerReplay:                   ctx.Bool(utils.SequencerReplay.Name),
		SequencerReplayHaltOnBatchNumber:  ctx.Uint64(utils.SequencerReplayHaltOnBatchNumber.Name),
		SequencerReplayExternalDatastream: ctx.Bool(utils.SequencerReplayExternalDatastream.Name),
		SequencerReplayL1SyncOnly:         ctx.Bool(utils.SequencerReplayL1SyncOnly.Name),
		StandaloneSMTDatabase:             ctx.Bool(utils.StandaloneSMTDatabase.Name),
		ExecutorMock:                      ctx.Bool(utils.ExecutorMock.Name),
		BlockInfoConcurrent:               ctx.Bool(utils.BlockInfoConcurrent.Name),
		EnableAsyncCommit:                 ctx.Bool(utils.EnableAsyncCommit.Name),
		BulkAddTxs:                        ctx.Bool(utils.BulkAddTxsFlag.Name),
		BulkAddTxsSize:                    ctx.Int(utils.BulkAddTxsSizeFlag.Name),
		BulkAddTxsWaitTime:                ctx.Duration(utils.BulkAddTxsWaitTimeFlag.Name),
		EnableAddTxNotify:                 ctx.Bool(utils.EnableAddTxNotify.Name),
		SequencerSkipEmptyBlocks:          ctx.Bool(utils.SequencerSkipEmptyBlocks.Name),
		SequencerMaxBlockSealTime:         sequencerMaxBlockSealTime,
		GetLogsTimeout:                    ctx.Duration(utils.GetLogsTimeout.Name),
		GetLogsRetries:                    ctx.Int(utils.GetLogsRetries.Name),

		TraceLogPath:   ctx.String(utils.TraceLogPath.Name),
		EnableTraceLog: ctx.Bool(utils.EnableTraceLog.Name),
	}
	if cfg.XLayer.BlockInfoConcurrent {
		blockinfo.SetUseBlockInfoTree(true)
	}

	// For X Layer, pre run
	utils.SetPreRunList(ctx, cfg)

	if ctx.IsSet(utils.ApolloNamespaceName.Name) {
		ns := strings.Split(ctx.String(utils.ApolloNamespaceName.Name), ",")
		for idx, item := range ns {
			ns[idx] = strings.TrimSpace(item)
		}
		cfg.XLayer.Apollo.NamespaceName = strings.Join(ns, ",")
	}
}

func ApplyFlagsForNodeXLayerConfig(ctx *cli.Context, cfg *nodecfg.Config) {
	cfg.Http.HttpApiKeys = ctx.String(utils.HTTPApiKeysFlag.Name)
	cfg.Http.MethodRateLimit = ctx.String(utils.MethodRateLimitFlag.Name)
}
