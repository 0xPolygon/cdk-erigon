package apollo

import (
	"fmt"
	"math"

	"github.com/apolloconfig/agollo/v4/storage"
	"github.com/ledgerwatch/erigon/cmd/utils"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/eth/gasprice/gaspricecfg"
	"github.com/ledgerwatch/erigon/node/nodecfg"
	"github.com/ledgerwatch/log/v3"
	"github.com/urfave/cli/v2"
)

// loadL2GasPricer loads the apollo l2gaspricer config cache on startup
func (c *Client) loadL2GasPricer(value interface{}) {
	ctx, _, err := c.getConfigContext(value)
	if err != nil {
		utils.Fatalf("load l2gaspricer from apollo config failed, err: %v", err)
	}

	// Load l2gaspricer config changes
	loadL2GasPricerConfig(ctx)
	log.Info(fmt.Sprintf("loaded l2gaspricer from apollo config: %+v", value.(string)))
}

// fireL2GasPricer fires the apollo l2gaspricer config change
func (c *Client) fireL2GasPricer(ctx *cli.Context, value *storage.ConfigChange) {
	loadL2GasPricerConfig(ctx)
	log.Info(fmt.Sprintf("apollo l2gaspricer old config : %+v", value.OldValue.(string)))
	log.Info(fmt.Sprintf("apollo l2gaspricer config changed: %+v", value.NewValue.(string)))

	// Set gp flag on fire configuration changes
	setL2GasPricerFlag()
}

// loadL2GasPricerConfig loads the dynamic gas pricer apollo configurations
func loadL2GasPricerConfig(ctx *cli.Context) {
	UnsafeGetApolloConfig().Lock()
	defer UnsafeGetApolloConfig().Unlock()

	loadNodeL2GasPricerConfig(ctx, &UnsafeGetApolloConfig().NodeCfg)
	loadEthL2GasPricerConfig(ctx, &UnsafeGetApolloConfig().EthCfg)
}

// loadNodeL2GasPricerConfig loads the dynamic gas pricer apollo node configurations
func loadNodeL2GasPricerConfig(ctx *cli.Context, nodeCfg *nodecfg.Config) {
	// Load l2gaspricer config
}

// loadEthL2GasPricerConfig loads the dynamic gas pricer apollo eth configurations
func loadEthL2GasPricerConfig(ctx *cli.Context, ethCfg *ethconfig.Config) {
	// Load generic ZK config
	loadZkConfig(ctx, ethCfg)

	// Load generic gas pricer config
	if ctx.IsSet(utils.EffectiveGasPriceForEthTransfer.Name) {
		effectiveGasPriceForEthTransferVal := ctx.Float64(utils.EffectiveGasPriceForEthTransfer.Name)
		effectiveGasPriceForEthTransferVal = math.Max(effectiveGasPriceForEthTransferVal, 0)
		effectiveGasPriceForEthTransferVal = math.Min(effectiveGasPriceForEthTransferVal, 1)
		ethCfg.Zk.EffectiveGasPriceForEthTransfer = uint8(math.Round(effectiveGasPriceForEthTransferVal * 255.0))
	}
	if ctx.IsSet(utils.EffectiveGasPriceForErc20Transfer.Name) {
		effectiveGasPriceForErc20TransferVal := ctx.Float64(utils.EffectiveGasPriceForErc20Transfer.Name)
		effectiveGasPriceForErc20TransferVal = math.Max(effectiveGasPriceForErc20TransferVal, 0)
		effectiveGasPriceForErc20TransferVal = math.Min(effectiveGasPriceForErc20TransferVal, 1)
		ethCfg.Zk.EffectiveGasPriceForErc20Transfer = uint8(math.Round(effectiveGasPriceForErc20TransferVal * 255.0))
	}
	if ctx.IsSet(utils.EffectiveGasPriceForContractInvocation.Name) {
		effectiveGasPriceForContractInvocationVal := ctx.Float64(utils.EffectiveGasPriceForContractInvocation.Name)
		effectiveGasPriceForContractInvocationVal = math.Max(effectiveGasPriceForContractInvocationVal, 0)
		effectiveGasPriceForContractInvocationVal = math.Min(effectiveGasPriceForContractInvocationVal, 1)
		ethCfg.Zk.EffectiveGasPriceForContractInvocation = uint8(math.Round(effectiveGasPriceForContractInvocationVal * 255.0))
	}
	if ctx.IsSet(utils.EffectiveGasPriceForContractDeployment.Name) {
		effectiveGasPriceForContractDeploymentVal := ctx.Float64(utils.EffectiveGasPriceForContractDeployment.Name)
		effectiveGasPriceForContractDeploymentVal = math.Max(effectiveGasPriceForContractDeploymentVal, 0)
		effectiveGasPriceForContractDeploymentVal = math.Min(effectiveGasPriceForContractDeploymentVal, 1)
		ethCfg.Zk.EffectiveGasPriceForContractDeployment = uint8(math.Round(effectiveGasPriceForContractDeploymentVal * 255.0))
	}
	if ctx.IsSet(utils.DefaultGasPrice.Name) {
		ethCfg.Zk.DefaultGasPrice = ctx.Uint64(utils.DefaultGasPrice.Name)
	}
	if ctx.IsSet(utils.MaxGasPrice.Name) {
		ethCfg.Zk.MaxGasPrice = ctx.Uint64(utils.MaxGasPrice.Name)
	}
	if ctx.IsSet(utils.GasPriceFactor.Name) {
		ethCfg.Zk.GasPriceFactor = ctx.Float64(utils.GasPriceFactor.Name)
	}

	// Load l2gaspricer config
	ethCfg.GPO = ethconfig.Defaults.GPO
	utils.SetApolloGPOXLayer(ctx, &ethCfg.GPO)
}

// setL2GasPricerFlag sets the dynamic gas pricer apollo flag
func setL2GasPricerFlag() {
	UnsafeGetApolloConfig().Lock()
	defer UnsafeGetApolloConfig().Unlock()
	UnsafeGetApolloConfig().setGPFlag()
}

func GetApolloGasPricerConfig() gaspricecfg.Config {
	UnsafeGetApolloConfig().Lock()
	defer UnsafeGetApolloConfig().Unlock()
	return UnsafeGetApolloConfig().EthCfg.GPO
}
