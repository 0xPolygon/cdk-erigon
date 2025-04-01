package utils

import (
	"fmt"
	"math/big"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/urfave/cli/v2"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/eth/gasprice/gaspricecfg"
)

var (
	// X Layer apollo
	ApolloEnableFlag = cli.BoolFlag{
		Name:  "zkevm.apollo-enabled",
		Usage: "Apollo enable flag.",
	}
	ApolloIPAddr = cli.StringFlag{
		Name:  "zkevm.apollo-ip-addr",
		Usage: "Apollo IP address.",
	}
	ApolloAppId = cli.StringFlag{
		Name:  "zkevm.apollo-app-id",
		Usage: "Apollo App ID.",
	}
	ApolloNamespaceName = cli.StringFlag{
		Name:  "zkevm.apollo-namespace-name",
		Usage: "Apollo namespace name.",
	}
	// X Layer nacos
	NacosURLsFlag = cli.StringFlag{
		Name:  "zkevm.nacos-urls",
		Usage: "Nacos urls.",
		Value: "",
	}
	NacosNamespaceIdFlag = cli.StringFlag{
		Name:  "zkevm.nacos-namespace-id",
		Usage: "Nacos namespace Id.",
		Value: "",
	}
	NacosApplicationNameFlag = cli.StringFlag{
		Name:  "zkevm.nacos-application-name",
		Usage: "Nacos application name",
		Value: "",
	}
	NacosExternalListenAddrFlag = cli.StringFlag{
		Name:  "zkevm.nacos-external-listen-addr",
		Usage: "Nacos external listen addr.",
		Value: "",
	}
	// Pool
	TxPoolEnableWhitelistFlag = cli.BoolFlag{
		Name:  "txpool.enable.whitelist",
		Usage: "Enable or disable tx sender white list",
		Value: false,
	}
	TxPoolWhiteList = cli.StringFlag{
		Name:  "txpool.whitelist",
		Usage: "Comma separated list of addresses, who can send transactions",
		Value: "",
	}
	TxPoolBlockedList = cli.StringFlag{
		Name:  "txpool.blockedlist",
		Usage: "Comma separated list of addresses, who can't send and receive transactions",
		Value: "",
	}
	TxPoolPackBatchSpecialList = cli.StringFlag{
		Name:  "txpool.packbatchspeciallist",
		Usage: "support free gas for claim addrs",
		Value: "",
	}
	TxPoolGasPriceMultiple = cli.StringFlag{
		Name:  "txpool.gaspricemultiple",
		Usage: "GasPriceMultiple is the factor claim tx gas price should mul",
		Value: "",
	}
	TxPoolEnableFreeGasByNonce = cli.BoolFlag{
		Name:  "txpool.enablefreegasbynonce",
		Usage: "EnableFreeGasByNonce enable free gas",
		Value: false,
	}
	TxPoolFreeGasExAddrs = cli.StringFlag{
		Name:  "txpool.freegasexaddress",
		Usage: "FreeGasExAddress is the ex address which can be free gas for the transfer receiver",
	}
	TxPoolFreeGasCountPerAddr = cli.Uint64Flag{
		Name:  "txpool.freegascountperaddr",
		Usage: "FreeGasCountPerAddr is the count limit of free gas tx per address",
	}
	TxPoolFreeGasLimit = cli.Uint64Flag{
		Name:  "txpool.freegaslimit",
		Usage: "FreeGasLimit is the max gas allowed use to do a free gas tx",
	}
	TxPoolEnableFreeGasList = cli.BoolFlag{
		Name:  "txpool.enablefreegaslist",
		Usage: "Enable or disable free gas for a specific project",
	}
	TxPoolFreeGasList = cli.StringFlag{
		Name:  "txpool.freegaslist",
		Usage: "FreeGasList Project in JSON Format",
	}
	TxPoolEnableTimsort = cli.BoolFlag{
		Name:  "txpool.enabletimsort",
		Usage: "EnableTimsort enable timsort to instead of built-in sorting",
		Value: false,
	}
	// Gas Pricer
	GpoTypeFlag = cli.StringFlag{
		Name:  "gpo.type",
		Usage: "raw gas price strategy type: default, follower, fixed",
		Value: "default",
	}
	GpoUpdatePeriodFlag = cli.StringFlag{
		Name:  "gpo.update-period",
		Usage: "raw gas price update period",
		Value: "10s",
	}
	GpoFactorFlag = cli.Float64Flag{
		Name:  "gpo.factor",
		Usage: "raw gas price facotr",
		Value: 0.15,
	}
	GpoKafkaURLFlag = cli.StringFlag{
		Name:  "gpo.kafka-url",
		Usage: "raw gas price kafka url",
		Value: "",
	}
	GpoTopicFlag = cli.StringFlag{
		Name:  "gpo.topic",
		Usage: "raw gas price topic",
		Value: "",
	}
	GpoGroupIDFlag = cli.StringFlag{
		Name:  "gpo.group-id",
		Usage: "raw gas price group id",
		Value: "",
	}
	GpoUsernameFlag = cli.StringFlag{
		Name:  "gpo.username",
		Usage: "raw gas price username",
		Value: "",
	}
	GpoPasswordFlag = cli.StringFlag{
		Name:  "gpo.password",
		Usage: "raw gas price password",
		Value: "",
	}
	GpoRootCAPathFlag = cli.StringFlag{
		Name:  "gpo.root-ca-path",
		Usage: "raw gas price root ca path",
		Value: "",
	}
	GpoL1CoinIdFlag = cli.IntFlag{
		Name:  "gpo.l1-coin-id",
		Usage: "raw gas price l1 coin id",
		Value: 0,
	}
	GpoL2CoinIdFlag = cli.IntFlag{
		Name:  "gpo.l2-coin-id",
		Usage: "raw gas price l2 coin id",
		Value: 0,
	}
	GpoDefaultL1CoinPriceFlag = cli.Float64Flag{
		Name:  "gpo.default-l1-coin-price",
		Usage: "raw gas price default l1 coin price",
		Value: 0,
	}
	GpoDefaultL2CoinPriceFlag = cli.Float64Flag{
		Name:  "gpo.default-l2-coin-price",
		Usage: "raw gas price default l2 coin price",
		Value: 0,
	}
	GpoGasPriceUsdtFlag = cli.Float64Flag{
		Name:  "gpo.gas-price-usdt",
		Usage: "raw gas price usdt",
		Value: 0,
	}
	GpoCongestionThresholdFlag = cli.IntFlag{
		Name:  "gpo.congestion-threshold",
		Usage: "Used to determine whether pending tx has reached the threshold for congestion",
		Value: 0,
	}
	SequencerBatchSleepDuration = cli.DurationFlag{
		Name:  "zkevm.sequencer-batch-sleep-duration",
		Usage: "Full batch sleep duration is the time the sequencer sleeps between each full batch iteration.",
		Value: 0 * time.Second,
	}
	// Sequencer
	AllowInternalTransactions = cli.BoolFlag{
		Name:  "zkevm.allow-internal-transactions",
		Usage: "Allow the sequencer to proceed internal transactions",
		Value: false,
	}
	// RPC
	HTTPApiKeysFlag = cli.StringFlag{
		Name: "http.apikeys",
		Usage: `API keys for the HTTP-RPC server and you can add rate limit to this apikey , format:
				{"project":"project1","key":"apikey1","timeout":"2023-12-12"}
				{"project":"project2","key":"apikey2","timeout":"2023-12-12"}
				{"project":"project3","key":"apikey3","timeout":"2023-12-12","methods":["method1","method2"],"count":1,"bucket":1}`,
		Value: "",
	}
	MethodRateLimitFlag = cli.StringFlag{
		Name:  "http.methodratelimit",
		Usage: "Method rate limit in requests per second, format: {\"method\":[\"method1\",\"method2\"],\"count\":1,\"bucket\":1}, eg. {\"methods\":[\"eth_call\",\"eth_blockNumber\"],\"count\":10,\"bucket\":1}",
		Value: "",
	}

	PreRunAddressList = cli.StringFlag{
		Name:  "zkevm.pre-run-address-list",
		Usage: "Pre run address list while receiving a transaction",
		Value: "",
	}
	PreRunCacheSize = cli.IntFlag{
		Name:  "zkevm.pre-run-cache-size",
		Usage: "Size of pre-run cache",
		Value: 10000,
	}
	PreRunCacheTTL = cli.DurationFlag{
		Name:  "zkevm.pre-run-cache-ttl",
		Usage: "pre-run cache entry TTL",
		Value: time.Hour,
	}
	PreRunChanNum = cli.IntFlag{
		Name:  "zkevm.pre-run-chan-num",
		Usage: "pre-run chan num",
		Value: 10000,
	}
	PreRunTaskNum = cli.IntFlag{
		Name:  "zkevm.pre-run-task-num",
		Usage: "pre-run task num",
		Value: 8,
	}
	BulkAddTxsFlag = cli.BoolFlag{
		Name:  "zkevm.bulk-add-txs",
		Usage: "Whether to enable bulk transaction addition",
		Value: false,
	}
	BulkAddTxsSizeFlag = cli.IntFlag{
		Name:  "zkevm.bulk-add-txs-size",
		Usage: "the size of the batched txs added to txpool",
		Value: 30,
	}
	BulkAddTxsWaitTimeFlag = cli.DurationFlag{
		Name:  "zkevm.bulk-add-txs-wait-time",
		Usage: "maximum waiting time for bulk adding transactions",
		Value: 5 * time.Millisecond,
	}
	EnableAddTxNotify = cli.BoolFlag{
		Name:  "zkevm.enable-add-tx-notify",
		Usage: "Enable notifications to limit added transactions",
		Value: false,
	}

	// Local Replay
	SequencerReplay = cli.BoolFlag{
		Name:  "zkevm.sequencer-replay",
		Usage: "Local replay feature, only works when zkevm.sequencer-resequence enabled",
		Value: false,
	}
	SequencerReplayHaltOnBatchNumber = cli.Uint64Flag{
		Name:  "zkevm.sequencer-replay-halt-on-batch-number",
		Usage: "Halt the sequencer on this batch number when replaying",
		Value: 0,
	}
	SequencerReplayExternalDatastream = cli.BoolFlag{
		Name:  "zkevm.sequencer-replay-external-datastream",
		Usage: "When enabled, the sequencer will create a new data stream server connected to an external datastream file and read batches from it",
		Value: false,
	}
	SequencerReplayL1SyncOnly = cli.BoolFlag{
		Name:  "zkevm.sequencer-replay-l1-sync-only",
		Usage: "When enabled, the sequencer will only sync the L1 info and exit",
		Value: false,
	}
	// Executor
	ExecutorMock = cli.BoolFlag{
		Name:  "zkevm.executor-mock",
		Usage: "Only for testing use. Generate the witness and return the verifierBundle without actually sending payload to executor.",
		Value: false,
	}
	// block info concurrent calculation
	BlockInfoConcurrent = cli.BoolFlag{
		Name:  "zkevm.block-info-concurrent",
		Usage: "Enable concurrent block info calculation",
		Value: false,
	}
)

func setGPOXLayer(ctx *cli.Context, cfg *gaspricecfg.Config) {
	if ctx.IsSet(DefaultGasPrice.Name) {
		cfg.Default = big.NewInt(ctx.Int64(DefaultGasPrice.Name))
	}
	if ctx.IsSet(GpoTypeFlag.Name) {
		cfg.XLayer.Type = ctx.String(GpoTypeFlag.Name)
	}
	if ctx.IsSet(GpoUpdatePeriodFlag.Name) {
		period, err := time.ParseDuration(ctx.String(GpoUpdatePeriodFlag.Name))
		if err != nil {
			panic(fmt.Sprintf("could not parse GpoUpdatePeriodFlag value %s", ctx.String(GpoUpdatePeriodFlag.Name)))
		}
		cfg.XLayer.UpdatePeriod = period
	}
	if ctx.IsSet(GpoFactorFlag.Name) {
		cfg.XLayer.Factor = ctx.Float64(GpoFactorFlag.Name)
	}
	if ctx.IsSet(GpoKafkaURLFlag.Name) {
		cfg.XLayer.KafkaURL = ctx.String(GpoKafkaURLFlag.Name)
	}
	if ctx.IsSet(GpoTopicFlag.Name) {
		cfg.XLayer.Topic = ctx.String(GpoTopicFlag.Name)
	}
	if ctx.IsSet(GpoGroupIDFlag.Name) {
		cfg.XLayer.GroupID = ctx.String(GpoGroupIDFlag.Name)
	}
	if ctx.IsSet(GpoUsernameFlag.Name) {
		cfg.XLayer.Username = ctx.String(GpoUsernameFlag.Name)
	}
	if ctx.IsSet(GpoPasswordFlag.Name) {
		cfg.XLayer.Password = ctx.String(GpoPasswordFlag.Name)
	}
	if ctx.IsSet(GpoRootCAPathFlag.Name) {
		cfg.XLayer.RootCAPath = ctx.String(GpoRootCAPathFlag.Name)
	}
	if ctx.IsSet(GpoL1CoinIdFlag.Name) {
		cfg.XLayer.L1CoinId = ctx.Int(GpoL1CoinIdFlag.Name)
	}
	if ctx.IsSet(GpoL2CoinIdFlag.Name) {
		cfg.XLayer.L2CoinId = ctx.Int(GpoL2CoinIdFlag.Name)
	}
	if ctx.IsSet(GpoDefaultL1CoinPriceFlag.Name) {
		cfg.XLayer.DefaultL1CoinPrice = ctx.Float64(GpoDefaultL1CoinPriceFlag.Name)
	}
	if ctx.IsSet(GpoDefaultL2CoinPriceFlag.Name) {
		cfg.XLayer.DefaultL2CoinPrice = ctx.Float64(GpoDefaultL2CoinPriceFlag.Name)
	}
	if ctx.IsSet(GpoGasPriceUsdtFlag.Name) {
		cfg.XLayer.GasPriceUsdt = ctx.Float64(GpoGasPriceUsdtFlag.Name)
	}
	if ctx.IsSet(GpoCongestionThresholdFlag.Name) {
		cfg.XLayer.CongestionThreshold = ctx.Int(GpoCongestionThresholdFlag.Name)
	}

	// Default price check
	if cfg.Default == nil || cfg.Default.Int64() <= 0 {
		cfg.Default = new(big.Int).Set(gaspricecfg.DefaultXLayerPrice)
	}
}

func setTxPoolXLayer(ctx *cli.Context, cfg *ethconfig.DeprecatedTxPoolConfig) {
	if ctx.IsSet(TxPoolEnableWhitelistFlag.Name) {
		cfg.EnableWhitelist = ctx.Bool(TxPoolEnableWhitelistFlag.Name)
	}
	if ctx.IsSet(TxPoolWhiteList.Name) {
		// Parse the command separated flag
		addrHexes := libcommon.CliString2Array(ctx.String(TxPoolWhiteList.Name))
		cfg.WhiteList = *libcommon.NewOrderedListOfAddresses(len(addrHexes))
		for _, senderHex := range addrHexes {
			cfg.WhiteList.Add(libcommon.HexToAddress(senderHex))
		}
		cfg.WhiteList.Sort()
	}
	if ctx.IsSet(TxPoolBlockedList.Name) {
		// Parse the command separated flag
		addrHexes := libcommon.CliString2Array(ctx.String(TxPoolBlockedList.Name))
		cfg.BlockedList = *libcommon.NewOrderedListOfAddresses(len(addrHexes))
		for _, senderHex := range addrHexes {
			cfg.BlockedList.Add(libcommon.HexToAddress(senderHex))
		}
		cfg.BlockedList.Sort()
	}
	if ctx.IsSet(TxPoolPackBatchSpecialList.Name) {
		addrHexes := libcommon.CliString2Array(ctx.String(TxPoolPackBatchSpecialList.Name))
		cfg.FreeClaimGasAddrs = *libcommon.NewOrderedListOfAddresses(len(addrHexes))
		for _, senderHex := range addrHexes {
			cfg.FreeClaimGasAddrs.Add(libcommon.HexToAddress(senderHex))
		}
		cfg.FreeClaimGasAddrs.Sort()
	}
	if ctx.IsSet(TxPoolGasPriceMultiple.Name) {
		cfg.GasPriceMultiple = ctx.Uint64(TxPoolGasPriceMultiple.Name)
	}
	if ctx.IsSet(TxPoolEnableFreeGasByNonce.Name) {
		cfg.EnableFreeGasByNonce = ctx.Bool(TxPoolEnableFreeGasByNonce.Name)
	}
	if ctx.IsSet(TxPoolFreeGasExAddrs.Name) {
		addrHexes := libcommon.CliString2Array(ctx.String(TxPoolFreeGasExAddrs.Name))
		cfg.FreeGasExAddrs = *libcommon.NewOrderedListOfAddresses(len(addrHexes))
		for _, senderHex := range addrHexes {
			cfg.FreeGasExAddrs.Add(libcommon.HexToAddress(senderHex))
		}
		cfg.FreeGasExAddrs.Sort()
	}
	if ctx.IsSet(TxPoolFreeGasCountPerAddr.Name) {
		cfg.FreeGasCountPerAddr = ctx.Uint64(TxPoolFreeGasCountPerAddr.Name)
	}
	if ctx.IsSet(TxPoolFreeGasLimit.Name) {
		cfg.FreeGasLimit = ctx.Uint64(TxPoolFreeGasLimit.Name)
	}
	if ctx.IsSet(TxPoolEnableFreeGasList.Name) {
		cfg.EnableFreeGasList = ctx.Bool(TxPoolEnableFreeGasList.Name)
	}
	if ctx.IsSet(TxPoolFreeGasList.Name) {
		freeGasListStr := ctx.String(TxPoolFreeGasList.Name)
		if len(freeGasListStr) > 0 {
			if err := jsoniter.UnmarshalFromString(freeGasListStr, &cfg.FreeGasList); err != nil {
				panic("unable to unmarshal freeGasList:" + err.Error())
			}
		}
	}
	if ctx.IsSet(TxPoolEnableTimsort.Name) {
		cfg.EnableTimsort = ctx.Bool(TxPoolEnableTimsort.Name)
	}
}

// SetApolloGPOXLayer is a public wrapper function to internally call setGPO
func SetApolloGPOXLayer(ctx *cli.Context, cfg *gaspricecfg.Config) {
	setGPO(ctx, cfg)
}

// SetApolloPoolXLayer is a public wrapper function to internally call setTxPool
func SetApolloPoolXLayer(ctx *cli.Context, fullCfg *ethconfig.Config) {
	setTxPool(ctx, fullCfg)
}

// CheckAddressExists check if the address exists in the address map
func CheckAddressExists(addressMap map[libcommon.Address]struct{}, target libcommon.Address) bool {
	_, exists := addressMap[target]
	return exists
}

// SetPreRunList is set pre run list and cache size, ttl, chan num, task num
func SetPreRunList(ctx *cli.Context, cfg *ethconfig.Config) {
	if ctx.IsSet(PreRunAddressList.Name) {
		addrHexes := libcommon.CliString2Array(ctx.String(PreRunAddressList.Name))

		cfg.XLayer.PreRunList = make(map[libcommon.Address]struct{}, len(addrHexes))
		for _, addr := range addrHexes {
			cfg.XLayer.PreRunList[libcommon.HexToAddress(addr)] = struct{}{}
		}
		cfg.XLayer.PreRunCacheSize = ctx.Int(PreRunCacheSize.Name)
		cfg.XLayer.PreRunCacheTTL = ctx.Duration(PreRunCacheTTL.Name)
		cfg.XLayer.PreRunChanNum = ctx.Int(PreRunChanNum.Name)
		cfg.XLayer.PreRunTaskNum = ctx.Int(PreRunTaskNum.Name)
	}
}

func SetBulkAddTxs(ctx *cli.Context, cfg *ethconfig.Config) {
	cfg.XLayer.BulkAddTxs = ctx.Bool(BulkAddTxsFlag.Name)
	cfg.XLayer.BulkAddTxsSize = ctx.Int(BulkAddTxsFlag.Name)
	cfg.XLayer.BulkAddTxsWaitTime = ctx.Duration(BulkAddTxsWaitTimeFlag.Name)
	cfg.XLayer.EnableAddTxNotify = ctx.Bool(EnableAddTxNotify.Name)
}
