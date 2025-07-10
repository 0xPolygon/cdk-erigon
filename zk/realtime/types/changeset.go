package types

import (
	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
)

type Changeset struct {
	DeletedAccounts       map[libcommon.Address]struct{}
	BalanceChanges        map[libcommon.Address]*uint256.Int
	NonceChanges          map[libcommon.Address]uint64
	CodeHashChanges       map[libcommon.Address]libcommon.Hash
	CodeChanges           map[libcommon.Hash][]byte
	IncarnationChanges    map[libcommon.Address]uint64
	IncarnationMapChanges map[libcommon.Address]uint64
	StorageChanges        map[libcommon.Address]map[libcommon.Hash]*uint256.Int
}

func NewChangeset() *Changeset {
	return &Changeset{
		DeletedAccounts:       make(map[libcommon.Address]struct{}),
		BalanceChanges:        make(map[libcommon.Address]*uint256.Int),
		NonceChanges:          make(map[libcommon.Address]uint64),
		CodeHashChanges:       make(map[libcommon.Address]libcommon.Hash),
		CodeChanges:           make(map[libcommon.Hash][]byte),
		IncarnationChanges:    make(map[libcommon.Address]uint64),
		IncarnationMapChanges: make(map[libcommon.Address]uint64),
		StorageChanges:        make(map[libcommon.Address]map[libcommon.Hash]*uint256.Int),
	}
}
