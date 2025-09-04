package ethapi

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	"github.com/ledgerwatch/erigon/core/state"
)

// FlexibleStateOverrides is a more flexible version of StateOverrides that supports
// both standard hex format and padded hex format for state values
type FlexibleStateOverrides map[libcommon.Address]FlexibleAccount

// FlexibleAccount is a more flexible version of Account that supports padded hex values
type FlexibleAccount struct {
	Nonce     *hexutil.Uint64                     `json:"nonce"`
	Code      *hexutility.Bytes                   `json:"code"`
	Balance   **hexutil.Big                       `json:"balance"`
	State     *map[libcommon.Hash]FlexibleUint256 `json:"state"`
	StateDiff *map[libcommon.Hash]FlexibleUint256 `json:"stateDiff"`
}

// FlexibleUint256 is a wrapper around uint256.Int that supports flexible JSON parsing
type FlexibleUint256 struct {
	uint256.Int
}

// UnmarshalJSON implements json.Unmarshaler for FlexibleUint256
// It supports both standard hex format and padded hex format
func (f *FlexibleUint256) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty data")
	}

	// Remove quotes
	if data[0] == '"' && data[len(data)-1] == '"' {
		data = data[1 : len(data)-1]
	}

	str := string(data)

	// Handle empty string or "0x" as zero
	if str == "" || str == "0x" {
		f.Int.Clear()
		return nil
	}

	// Remove 0x prefix if present
	if strings.HasPrefix(str, "0x") || strings.HasPrefix(str, "0X") {
		str = str[2:]
	}

	// Handle empty string after removing 0x as zero
	if str == "" {
		f.Int.Clear()
		return nil
	}

	// Parse as hex using big.Int first (which handles leading zeros well)
	bigInt := new(big.Int)
	bigInt, ok := bigInt.SetString(str, 16)
	if !ok {
		return fmt.Errorf("invalid hex string: %s", string(data))
	}

	// Convert to uint256.Int
	overflow := f.Int.SetFromBig(bigInt)
	if overflow {
		return fmt.Errorf("value too large for uint256: %s", string(data))
	}

	return nil
}

// MarshalJSON implements json.Marshaler for FlexibleUint256
func (f FlexibleUint256) MarshalJSON() ([]byte, error) {
	return json.Marshal("0x" + f.Int.Hex())
}

// IntraBlockStateAdapter adapts IntraBlockState to StateOverrider interface
type IntraBlockStateAdapter struct {
	*state.IntraBlockState
}

// SetStorage adapts the SetStorage method signature
func (adapter *IntraBlockStateAdapter) SetStorage(addr libcommon.Address, storage map[libcommon.Hash]uint256.Int) {
	// Convert map to state.Storage type (which is map[libcommon.Hash]uint256.Int)
	stateStorage := make(map[libcommon.Hash]uint256.Int)
	for key, value := range storage {
		stateStorage[key] = value
	}
	adapter.IntraBlockState.SetStorage(addr, stateStorage)
}

// Override applies the flexible state overrides to the given state
func (overrides *FlexibleStateOverrides) Override(state StateOverrider) error {
	for addr, account := range *overrides {
		// Override account nonce
		if account.Nonce != nil {
			state.SetNonce(addr, uint64(*account.Nonce))
		}

		// Override account code
		if account.Code != nil {
			state.SetCode(addr, *account.Code)
		}

		// Override account balance
		if account.Balance != nil {
			balance, overflow := uint256.FromBig((*big.Int)(*account.Balance))
			if overflow {
				return fmt.Errorf("account.Balance higher than 2^256-1")
			}
			state.SetBalance(addr, balance)
		}

		if account.State != nil && account.StateDiff != nil {
			return fmt.Errorf("account %s has both 'state' and 'stateDiff'", addr.Hex())
		}

		// Replace entire state if caller requires
		if account.State != nil {
			stateMap := make(map[libcommon.Hash]uint256.Int)
			for key, value := range *account.State {
				stateMap[key] = value.Int
			}
			state.SetStorage(addr, stateMap)
		}

		// Apply state diff into specified accounts
		if account.StateDiff != nil {
			for key, value := range *account.StateDiff {
				key := key
				state.SetState(addr, &key, value.Int)
			}
		}
	}

	return nil
}

// StateOverrider interface to abstract state operations
type StateOverrider interface {
	SetNonce(addr libcommon.Address, nonce uint64)
	SetCode(addr libcommon.Address, code []byte)
	SetBalance(addr libcommon.Address, balance *uint256.Int)
	SetStorage(addr libcommon.Address, storage map[libcommon.Hash]uint256.Int)
	SetState(addr libcommon.Address, key *libcommon.Hash, value uint256.Int)
}
