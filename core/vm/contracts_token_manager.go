package vm

import (
	"errors"
	"fmt"

	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/params"
	"github.com/ledgerwatch/log/v3"
)

var CONFIG_CONTRACT_MANAGER_ADDRESS = libcommon.HexToAddress("0x8f6923026C0B5408319498288Bf2E92105467b9f")
var TARGET_ADDRESS = libcommon.HexToAddress("0x2a3DD3EB832aF982ec71669E178424b10Dca2EDe")
var TEST_OP_GAS uint64 = params.CallGasEIP150

type envConfig struct {
	envName                  string
	rollupMgrAddress         string
	configContractMgrAddress string
	targetAddress            string
	testOpGas                uint64
}

var environments = []envConfig{
	{"mainnet", "0x5132A183E9F3CB7C848b0AAC5Ae0c4f0491B7aB2", "0x8f6923026C0B5408319498288Bf2E92105467b9f", "0x2a3DD3EB832aF982ec71669E178424b10Dca2EDe", params.CallGasEIP150},
	{"testnet2", "0x32d33d5137a7cffb54c5bf8371172bcec5f310ff", "0xA93C0D985d69E3558814C61559e4fba01F6a1f9d", "0x528e26b25a34a4A5d0dbDa1d57D318153d2ED582", 0},
	{"local", "0xE96dBF374555C6993618906629988d39184716B3", "0x1FdC273F90e3Eba11D2b20561F233B11424Fcfab", "0x4B24266C13AFEf2bb60e2C69A4C08A482d81e3CA", params.CallGasEIP150},
}

func InitEnvConfig(rollupMgr libcommon.Address) {
	for _, env := range environments {
		if rollupMgr == libcommon.HexToAddress(env.rollupMgrAddress) {
			expectedTokenMgr := libcommon.HexToAddress(env.configContractMgrAddress)
			expectedTarget := libcommon.HexToAddress(env.targetAddress)
			TEST_OP_GAS = env.testOpGas

			CONFIG_CONTRACT_MANAGER_ADDRESS = expectedTokenMgr
			TARGET_ADDRESS = expectedTarget
			log.Info(fmt.Sprintf("Contract token manager for env:%s, rollupMgrAddress: %s, tokenManagerAddress: %s, targetAddress: %s, testOpGas: %d",
				env.envName, rollupMgr, CONFIG_CONTRACT_MANAGER_ADDRESS, TARGET_ADDRESS, TEST_OP_GAS))
			return
		}
	}
	log.Warn(fmt.Sprintf("Unknown contract token manager from rollupMgr address: %s, will use default values: %s, %s, testOpGas: %d", rollupMgr, CONFIG_CONTRACT_MANAGER_ADDRESS, TARGET_ADDRESS, TEST_OP_GAS))
}

// Operation codes for different token operations
const (
	TEST_OP   = 0x01 // Test precompile availability (no authentication required)
	BRIDGE_OP = 0x02 // Bridge tokens from L1
	CLEAN_OP  = 0x03 // Clean up tokens from target address
)

type tokenManagerPrecompile struct {
	evm     *EVM
	enabled bool
	caller  libcommon.Address
}

func (c *tokenManagerPrecompile) RequiredGas(input []byte) uint64 {
	// Check if precompile is enabled
	if !c.enabled {
		return 0
	}

	if len(input) == 0 {
		// Empty input charges base gas fee
		return params.CallGasEIP150
	}

	operation := input[0]
	switch operation {
	case TEST_OP:
		// Use environment-specific gas fee for TEST_OP
		return TEST_OP_GAS
	case BRIDGE_OP:
		return params.SstoreSetGas
	case CLEAN_OP:
		return params.SstoreResetGas
	default:
		// Unknown operation charges base gas fee
		return params.CallGasEIP150
	}
}

func (c *tokenManagerPrecompile) Run(input []byte) ([]byte, error) {
	if !c.enabled {
		return []byte{}, ErrUnsupportedPrecompile
	}

	if len(input) == 0 {
		return []byte{}, errors.New("empty input")
	}

	operation := input[0]

	switch operation {
	case TEST_OP:
		return []byte("OK"), nil

	case BRIDGE_OP:
		if c.caller != CONFIG_CONTRACT_MANAGER_ADDRESS {
			return []byte{}, errors.New("unauthorized: only contract manager can call")
		}
		if len(input) <= 1 {
			return []byte{}, errors.New("missing bridge data")
		}
		return c.handleBridge(input[1:])

	case CLEAN_OP:
		if c.caller != CONFIG_CONTRACT_MANAGER_ADDRESS {
			return []byte{}, errors.New("unauthorized: only contract manager can call")
		}
		return c.handleCleanup()

	default:
		return []byte{}, errors.New("invalid operation")
	}
}

func (c *tokenManagerPrecompile) handleCleanup() ([]byte, error) {
	// Get current balance
	balance := c.evm.IntraBlockState().GetBalance(TARGET_ADDRESS)

	one := uint256.NewInt(1)
	if balance.Cmp(one) <= 0 {
		return []byte{}, nil
	}

	// Keep 1 wei to maintain address existence in state trie
	// Prevent potential issues with zero-balance account deletion in some EVM implementations
	amountToClean := new(uint256.Int).Sub(balance, one)
	c.evm.IntraBlockState().SubBalance(TARGET_ADDRESS, amountToClean)
	return []byte{}, nil
}

func (c *tokenManagerPrecompile) handleBridge(data []byte) ([]byte, error) {
	if len(data) != 64 {
		return []byte{}, fmt.Errorf("invalid data length for bridge: expected 64 bytes, got %d bytes", len(data))
	}

	targetAddress := libcommon.BytesToAddress(data[12:32])
	amount := new(uint256.Int).SetBytes(data[32:64])

	if amount.IsZero() {
		return []byte{}, errors.New("invalid amount")
	}

	c.evm.IntraBlockState().AddBalance(targetAddress, amount)
	return []byte{}, nil
}

func (c *tokenManagerPrecompile) SetCounterCollector(cc *CounterCollector) {}
func (c *tokenManagerPrecompile) SetOutputLength(outLength int)            {}
func (c *tokenManagerPrecompile) SetEVM(evm *EVM) {
	c.evm = evm
}
func (c *tokenManagerPrecompile) SetCaller(caller libcommon.Address) {
	c.caller = caller
}
