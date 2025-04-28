package apollo

import (
	"fmt"
	"time"

	"github.com/apolloconfig/agollo/v4/storage"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/cmd/utils"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/node/nodecfg"
	"github.com/ledgerwatch/log/v3"
	"github.com/urfave/cli/v2"
)

// loadSequencer loads the apollo sequencer config cache on startup
func (c *Client) loadSequencer(value interface{}) {
	ctx, _, err := c.getConfigContext(value)
	if err != nil {
		utils.Fatalf("load sequencer from apollo config failed, err: %v", err)
	}

	// Load sequencer config changes
	loadSequencerConfig(ctx)
	log.Info(fmt.Sprintf("loaded sequencer from apollo config: %+v", value.(string)))
}

// fireSequencer fires the apollo sequencer config change
func (c *Client) fireSequencer(ctx *cli.Context, value *storage.ConfigChange) {
	loadSequencerConfig(ctx)
	log.Info(fmt.Sprintf("apollo sequencer old config : %+v", value.OldValue.(string)))
	log.Info(fmt.Sprintf("apollo sequencer config changed: %+v", value.NewValue.(string)))

	// Set sequencer flag on fire configuration changes
	setSequencerFlag()
}

// loadSequencerConfig loads the dynamic sequencer apollo configurations
func loadSequencerConfig(ctx *cli.Context) {
	UnsafeGetApolloConfig().Lock()
	defer UnsafeGetApolloConfig().Unlock()

	loadNodeSequencerConfig(ctx, &UnsafeGetApolloConfig().NodeCfg)
	loadEthSequencerConfig(ctx, &UnsafeGetApolloConfig().EthCfg)
}

// loadNodeSequencerConfig loads the dynamic sequencer apollo node configurations
func loadNodeSequencerConfig(ctx *cli.Context, nodeCfg *nodecfg.Config) {
	// Load sequencer config
}

// loadEthSequencerConfig loads the dynamic sequencer apollo eth configurations
func loadEthSequencerConfig(ctx *cli.Context, ethCfg *ethconfig.Config) {
	// Load generic ZK config
	loadZkConfig(ctx, ethCfg)

	// Load sequencer config
	if ctx.IsSet(utils.SequencerBlockSealTime.Name) {
		ethCfg.Zk.SequencerBlockSealTime = ctx.Duration(utils.SequencerBlockSealTime.Name)
	}
	if ctx.IsSet(utils.SequencerBatchSealTime.Name) {
		ethCfg.Zk.SequencerBatchSealTime = ctx.Duration(utils.SequencerBatchSealTime.Name)
	}
	if ctx.IsSet(utils.SequencerBatchSleepDuration.Name) {
		ethCfg.Zk.XLayer.SequencerBatchSleepDuration = ctx.Duration(utils.SequencerBatchSleepDuration.Name)
	}
	if ctx.IsSet(utils.SequencerHaltOnBatchNumber.Name) {
		ethCfg.Zk.SequencerHaltOnBatchNumber = ctx.Uint64(utils.SequencerHaltOnBatchNumber.Name)
	}
	if ctx.IsSet(utils.EnableAsyncCommit.Name) {
		ethCfg.Zk.XLayer.EnableAsyncCommit = ctx.Bool(utils.EnableAsyncCommit.Name)
	}
	if ctx.IsSet(utils.BulkAddTxsFlag.Name) {
		ethCfg.Zk.XLayer.BulkAddTxs = ctx.Bool(utils.BulkAddTxsFlag.Name)
	}
	if ctx.IsSet(utils.BulkAddTxsSizeFlag.Name) {
		ethCfg.Zk.XLayer.BulkAddTxsSize = ctx.Int(utils.BulkAddTxsSizeFlag.Name)
	}
	if ctx.IsSet(utils.BulkAddTxsWaitTimeFlag.Name) {
		ethCfg.Zk.XLayer.BulkAddTxsWaitTime = ctx.Duration(utils.BulkAddTxsWaitTimeFlag.Name)
	}
	if ctx.IsSet(utils.EnableAddTxNotify.Name) {
		ethCfg.Zk.XLayer.EnableAddTxNotify = ctx.Bool(utils.EnableAddTxNotify.Name)
	}
	if ctx.IsSet(utils.YieldSizeFlag.Name) {
		ethCfg.YieldSize = ctx.Uint64(utils.YieldSizeFlag.Name)
	}
	if ctx.IsSet(utils.PreRunAddressList.Name) {
		addrHexes := libcommon.CliString2Array(ctx.String(utils.PreRunAddressList.Name))

		ethCfg.XLayer.PreRunList = make(map[libcommon.Address]struct{}, len(addrHexes))
		for _, addr := range addrHexes {
			ethCfg.XLayer.PreRunList[libcommon.HexToAddress(addr)] = struct{}{}
		}
	}
	if ctx.IsSet(utils.BlockInfoConcurrent.Name) {
		ethCfg.XLayer.BlockInfoConcurrent = ctx.Bool(utils.BlockInfoConcurrent.Name)
	}
}

// setSequencerFlag sets the dynamic sequencer apollo flag
func setSequencerFlag() {
	UnsafeGetApolloConfig().Lock()
	defer UnsafeGetApolloConfig().Unlock()
	UnsafeGetApolloConfig().setSequencerFlag()
}

func GetFullBatchSleepDuration(localDuration time.Duration) time.Duration {
	if IsApolloConfigSequencerEnabled() {
		UnsafeGetApolloConfig().RLock()
		defer UnsafeGetApolloConfig().RUnlock()
		return UnsafeGetApolloConfig().EthCfg.Zk.XLayer.SequencerBatchSleepDuration
	}
	return localDuration
}

func GetSequencerHalt(localHaltBatchNumber uint64) uint64 {
	if IsApolloConfigSequencerEnabled() {
		UnsafeGetApolloConfig().RLock()
		defer UnsafeGetApolloConfig().RUnlock()
		return UnsafeGetApolloConfig().EthCfg.Zk.SequencerHaltOnBatchNumber
	}
	return localHaltBatchNumber
}

func GetYieldSize(localYieldSize uint16) uint16 {
	if IsApolloConfigSequencerEnabled() {
		UnsafeGetApolloConfig().RLock()
		defer UnsafeGetApolloConfig().RUnlock()
		return uint16(UnsafeGetApolloConfig().EthCfg.YieldSize)
	}
	return localYieldSize
}
