package vm

import (
	"testing"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"gotest.tools/v3/assert"
)

func TestTokenAddress(t *testing.T) {
	for _, env := range environments {
		InitEnvConfig(libcommon.HexToAddress(env.rollupMgrAddress))
		assert.Equal(t, CONFIG_CONTRACT_MANAGER_ADDRESS, libcommon.HexToAddress(env.configContractMgrAddress))
		assert.Equal(t, TARGET_ADDRESS, libcommon.HexToAddress(env.targetAddress))
	}
}
