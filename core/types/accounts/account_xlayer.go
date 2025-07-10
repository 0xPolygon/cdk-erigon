package accounts

import (
	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
)

// Helper function for deep copy
func DeepCopyAccount(acc *Account) *Account {
	if acc == nil {
		return nil
	}
	return &Account{
		Initialised:     acc.Initialised,
		Nonce:           acc.Nonce,
		Balance:         *uint256.NewInt(0).Set(&acc.Balance),
		Root:            libcommon.BytesToHash(acc.Root.Bytes()),
		CodeHash:        libcommon.BytesToHash(acc.CodeHash.Bytes()),
		Incarnation:     acc.Incarnation,
		PrevIncarnation: acc.PrevIncarnation,
	}
}
