package apollo

import libcommon "github.com/ledgerwatch/erigon-lib/common"

// Note: Both pool and sequencer namespaces are allowed to set and use dynamic OkPay configs

// CheckOkPayAddress checks if the address is in the OkPay sender accounts list
func (cfg *ApolloConfig) CheckOkPayAddress(localOkPayAccountsList libcommon.OrderedList[libcommon.Address], addr libcommon.Address) bool {
	cfg.RLock()
	defer cfg.RUnlock()

	if cfg.isPoolEnabled() || cfg.isSeqEnabled() {
		return cfg.EthCfg.DeprecatedTxPool.OkPaySenderAccountsList.Contains(addr)
	}

	return localOkPayAccountsList.Contains(addr)
}

// GetOkPayBlockPriorityTxsLimit returns the max number of OkPay txs that we will prioritize per block
func (cfg *ApolloConfig) GetOkPayBlockPriorityTxsLimit(localOkPayBlockPriorityTxsLimit uint64) uint64 {
	cfg.RLock()
	defer cfg.RUnlock()

	if cfg.isPoolEnabled() || cfg.isSeqEnabled() {
		return cfg.EthCfg.DeprecatedTxPool.OkPayBlockPriorityTxsLimit
	}

	return localOkPayBlockPriorityTxsLimit
}
