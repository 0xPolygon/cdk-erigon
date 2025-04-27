package apollo

import (
	"fmt"

	"github.com/apolloconfig/agollo/v4/storage"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/cmd/utils"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/node/nodecfg"
	"github.com/ledgerwatch/log/v3"
	"github.com/urfave/cli/v2"
)

// loadPool loads the apollo pool config cache on startup
func (c *Client) loadPool(value interface{}) {
	ctx, _, err := c.getConfigContext(value)
	if err != nil {
		utils.Fatalf("load pool from apollo config failed, err: %v", err)
	}

	// Load pool config changes
	loadPoolConfig(ctx)
	log.Info(fmt.Sprintf("loaded pool from apollo config: %+v", value.(string)))
}

// firePool fires the apollo pool config change
func (c *Client) firePool(ctx *cli.Context, value *storage.ConfigChange) {
	loadPoolConfig(ctx)
	log.Info(fmt.Sprintf("apollo pool old config : %+v", value.OldValue.(string)))
	log.Info(fmt.Sprintf("apollo pool config changed: %+v", value.NewValue.(string)))

	// Set pool flag on fire configuration changes
	setPoolFlag()
}

// loadPoolConfig loads the dynamic pool apollo configurations
func loadPoolConfig(ctx *cli.Context) {
	UnsafeGetApolloConfig().Lock()
	defer UnsafeGetApolloConfig().Unlock()

	loadNodePoolConfig(ctx, &UnsafeGetApolloConfig().NodeCfg)
	loadEthPoolConfig(ctx, &UnsafeGetApolloConfig().EthCfg)
}

// loadNodePoolConfig loads the dynamic pool apollo node configurations
func loadNodePoolConfig(ctx *cli.Context, nodeCfg *nodecfg.Config) {
	// Load pool config
}

// loadEthL2GasPricerConfig loads the dynamic gas pricer apollo eth configurations
func loadEthPoolConfig(ctx *cli.Context, ethCfg *ethconfig.Config) {
	// Load generic ZK config
	loadZkConfig(ctx, ethCfg)

	// Load deprecated pool config
	utils.SetApolloPoolXLayer(ctx, ethCfg)
}

func setPoolFlag() {
	UnsafeGetApolloConfig().Lock()
	defer UnsafeGetApolloConfig().Unlock()
	UnsafeGetApolloConfig().setPoolFlag()
}

// -------------------------- txpool config methods --------------------------
// Note that due to the circular dependency constraints on the txpool, we will
// pass the the apollo singleton instance directly into the txpool. Thus, pool
// method definitions defer in design and are defined as methods instead.
//
// Mutex read/write locks are to be held with every exposed methods to ensure
// atomicity.

func (cfg *ApolloConfig) CheckBlockedAddr(localBlockedList libcommon.OrderedList[libcommon.Address], addr libcommon.Address) bool {
	cfg.RLock()
	defer cfg.RUnlock()

	if cfg.isPoolEnabled() {
		return cfg.EthCfg.DeprecatedTxPool.BlockedList.Contains(addr)
	}
	return localBlockedList.Contains(addr)
}

func (cfg *ApolloConfig) GetEnableWhitelist(localEnableWhitelist bool) bool {
	cfg.RLock()
	defer cfg.RUnlock()

	if cfg.isPoolEnabled() {
		return cfg.EthCfg.DeprecatedTxPool.EnableWhitelist
	}
	return localEnableWhitelist
}

func (cfg *ApolloConfig) CheckWhitelistAddr(localWhitelist libcommon.OrderedList[libcommon.Address], addr libcommon.Address) bool {
	cfg.RLock()
	defer cfg.RUnlock()

	if cfg.isPoolEnabled() {
		return cfg.EthCfg.DeprecatedTxPool.WhiteList.Contains(addr)
	}
	return localWhitelist.Contains(addr)
}

func (cfg *ApolloConfig) CheckFreeClaimAddr(localFreeClaimGasAddrs libcommon.OrderedList[libcommon.Address], addr libcommon.Address) bool {
	cfg.RLock()
	defer cfg.RUnlock()

	if cfg.isPoolEnabled() {
		return cfg.EthCfg.DeprecatedTxPool.FreeClaimGasAddrs.Contains(addr)
	}
	return localFreeClaimGasAddrs.Contains(addr)
}

func (cfg *ApolloConfig) CheckFreeGasExAddr(localFreeGasExAddrs libcommon.OrderedList[libcommon.Address], addr libcommon.Address) bool {
	cfg.RLock()
	defer cfg.RUnlock()

	if cfg.isPoolEnabled() {
		return cfg.EthCfg.DeprecatedTxPool.FreeGasExAddrs.Contains(addr)
	}
	return localFreeGasExAddrs.Contains(addr)
}

func (cfg *ApolloConfig) GetEnableFreeGasList(localEnableFreeGasList bool) bool {
	cfg.RLock()
	defer cfg.RUnlock()

	if cfg.isPoolEnabled() {
		return cfg.EthCfg.DeprecatedTxPool.EnableFreeGasList
	}
	return localEnableFreeGasList
}
