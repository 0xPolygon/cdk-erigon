package types

import (
	"fmt"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/types"
)

type BlockInfo struct {
	Header    *types.Header  `json:"header"`
	TxCount   int64          `json:"txCount"`
	Hash      libcommon.Hash `json:"hash"`
	Changeset *Changeset     `json:"changeset,omitempty"`
}

func (msg BlockInfo) Validate(executionHeight uint64) error {
	if msg.Header == nil {
		return fmt.Errorf("header is nil")
	}
	if msg.Header.Number.Uint64() == 0 {
		return fmt.Errorf("block number is 0")
	}
	if msg.Header.Number.Uint64() < executionHeight {
		// Ignore block msgs from previous blocks
		return fmt.Errorf("received old block message, blockNum: %d executionHeight: %d", msg.Header.Number.Uint64(), executionHeight)
	}

	return nil
}

func (msg BlockInfo) IsConfirmedBlock() bool {
	return msg.TxCount >= 0 && msg.Hash != (libcommon.Hash{})
}
