package cli

import (
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	libcommon "github.com/ledgerwatch/erigon-lib/common"

	"github.com/ledgerwatch/erigon/cmd/utils"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/node/nodecfg"
	"github.com/ledgerwatch/erigon/smt/pkg/blockinfo"
	"github.com/ledgerwatch/erigon/zk/nacos"
	"github.com/ledgerwatch/erigon/zk/realtime"
	"github.com/ledgerwatch/erigon/zk/realtime/kafka"
	"github.com/ledgerwatch/log/v3"
	"github.com/urfave/cli/v2"
)

const EnvKafkaConsumerGroupID = "REALTIME_KAFKA_CONSUMER_GROUP_ID"

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

	// For realtime. Get GroupID from flag
	groupID := ctx.String(utils.RealtimeKafkaSyncGroupID.Name)
	if envGroupID := os.Getenv(EnvKafkaConsumerGroupID); envGroupID != "" {
		// Override consumer group id if env variable is set
		groupID = envGroupID
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
		Realtime: realtime.RealtimeConfig{
			Enable:               ctx.Bool(utils.RealtimeEnableFlag.Name),
			EnableSubscribe:      ctx.Bool(utils.RealtimeEnableSubscribeFlag.Name),
			CacheHeightThreshold: ctx.Uint64(utils.RealtimeCacheHeightThreshold.Name),
			CacheDumpPath:        ctx.String(utils.RealtimeCacheDumpPath.Name),
			Kafka: kafka.KafkaConfig{
				BootstrapServers: strings.Split(ctx.String(utils.RealtimeKafkaSyncBootstrapServers.Name), ","),
				BlockTopic:       ctx.String(utils.RealtimeKafkaSyncBlockTopic.Name),
				TxTopic:          ctx.String(utils.RealtimeKafkaSyncTxTopic.Name),
				ErrorTopic:       ctx.String(utils.RealtimeKafkaSyncErrorTopic.Name),
				ClientID:         ctx.String(utils.RealtimeKafkaSyncClientID.Name),
				GroupID:          groupID,
			},
		},
		BridgeIntercept: ethconfig.BridgeInterceptConfig{
			BridgeContractAddress: ctx.String(utils.BridgeInterceptBridgeContractAddress.Name),
			TargetTokenAddress:    ctx.String(utils.BridgeInterceptTargetTokenAddress.Name),
			MaxBridgeAmount: func() *big.Int {
				amount, _ := new(big.Int).SetString(ctx.String(utils.BridgeInterceptMaxBridgeAmount.Name), 10)
				return amount
			}(),
			WhitelistEnabled:   ctx.Bool(utils.BridgeInterceptWhitelistEnabled.Name),
			WhitelistAddresses: []libcommon.Address{},
		},
		DynamicBlockGasLimit: ctx.Uint64(utils.DynamicBlockGasLimit.Name),
		EnableLatestDataStreamBlockNumberGlobalVariableForRpc: ctx.Bool(utils.EnableLatestDataStreamBlockNumberGlobalVariableForRpc.Name),
		DataStreamUnwindToBlock:                               ctx.Uint64(utils.DataStreamUnwindToBlock.Name),
		SyncSeqLogs:                                           ctx.Bool(utils.SyncSeqLogs.Name),
	}
	if cfg.XLayer.BlockInfoConcurrent {
		blockinfo.SetUseBlockInfoTree(true)
	}
	SetVerificationConfigs(ctx, cfg)

	// For X Layer, pre run
	utils.SetPreRunList(ctx, cfg)

	if ctx.IsSet(utils.ApolloNamespaceName.Name) {
		ns := strings.Split(ctx.String(utils.ApolloNamespaceName.Name), ",")
		for idx, item := range ns {
			ns[idx] = strings.TrimSpace(item)
		}
		cfg.XLayer.Apollo.NamespaceName = strings.Join(ns, ",")
	}

	utils.SetInterceptWhitelist(ctx, cfg)
}

func ApplyFlagsForNodeXLayerConfig(ctx *cli.Context, cfg *nodecfg.Config) {
	cfg.Http.HttpApiKeys = ctx.String(utils.HTTPApiKeysFlag.Name)
	cfg.Http.MethodRateLimit = ctx.String(utils.MethodRateLimitFlag.Name)
}

func SetVerificationConfigs(ctx *cli.Context, cfg *ethconfig.Config) {
	cfg.XLayer.AnalysisGroupVerification.BatchDelay = ctx.Uint64(utils.VerificationBatchDelay.Name)
	cfg.XLayer.AnalysisGroupVerification.SkipAPI = ctx.Bool(utils.SkipAnalysisGroupAPI.Name)
	cfg.XLayer.AnalysisGroupVerification.APIPath = ctx.String(utils.AnalysisGroupAPIPath.Name)

	// Set AnalysisGroupServiceName
	if ctx.IsSet(utils.AnalysisGroupServiceName.Name) {
		serviceName := ctx.String(utils.AnalysisGroupServiceName.Name)

		if cfg.XLayer.AnalysisGroupVerification.SkipAPI {
			log.Warn("skip analysis group api but service name is set", "service name", serviceName)
		}
		var err error
		cfg.XLayer.AnalysisGroupVerification.NacosClient, err = nacos.NewNacosClient(ctx.String(utils.AnalysisGroupNacosUrls.Name), ctx.String(utils.AnalysisGroupNacosNamespace.Name), serviceName)
		if err != nil && !cfg.XLayer.AnalysisGroupVerification.SkipAPI {
			panic(fmt.Sprintf("failed to create nacos client for analysis group: %s", err))
		}
	}
}
