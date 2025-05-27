package apollo

import (
	"fmt"
	"time"

	"github.com/apolloconfig/agollo/v4/storage"
	"github.com/ledgerwatch/erigon/cmd/utils"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/node/nodecfg"
	"github.com/ledgerwatch/log/v3"
	"github.com/urfave/cli/v2"
)

// loadSequencer loads the apollo sequencer config cache on startup
func (c *Client) loadSequencer(value interface{}) {
	ctx, err := c.getConfigContext(value)
	if err != nil {
		utils.Fatalf("load sequencer from apollo config failed, err: %v", err)
	}

	// Load sequencer config changes
	loadSequencerConfig(ctx)
	log.Info(fmt.Sprintf("loaded sequencer from apollo config: %+v", value.(string)))
}

// fireSequencer fires the apollo sequencer config change
func (c *Client) fireSequencer(key string, value *storage.ConfigChange) {
	ctx, err := c.getConfigContext(value.NewValue)
	if err != nil {
		log.Error(fmt.Sprintf("fire sequencer from apollo config failed, err: %v", err))
		return
	}

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
	if ctx.IsSet(utils.SequencerBlockSingleBlockVerify.Name) {
		ethCfg.Zk.SequencerBlockSingleBlockVerify = ctx.Bool(utils.SequencerBlockSingleBlockVerify.Name)
	}
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

	// For OkPay
	utils.SetApolloOkPayXLayer(ctx, &ethCfg.DeprecatedTxPool)
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
