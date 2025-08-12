package realtimeapi

import (
	"fmt"
	"strings"

	"github.com/ledgerwatch/erigon/core/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

type RealtimeSubResult struct {
	Header   *types.Header      `json:"Header,omitempty"`
	TxHash   string             `json:"TxHash,omitempty"`
	TxData   types.Transaction  `json:"TxData,omitempty"`
	Receipt  *types.Receipt     `json:"Receipt,omitempty"`
	InnerTxs []*zktypes.InnerTx `json:"InnerTxs,omitempty"`
}

type RealtimeDebugResult struct {
	ConfirmHeight   uint64   `json:"confirmHeight"`
	ExecutionHeight uint64   `json:"executionHeight"`
	Mismatches      []string `json:"mismatches"`
}

type RealtimeTag int64

const (
	Latest  = RealtimeTag(-1)
	Pending = RealtimeTag(-2)
)

func (t *RealtimeTag) UnmarshalJSON(data []byte) error {
	input := strings.TrimSpace(string(data))
	if len(input) >= 2 && input[0] == '"' && input[len(input)-1] == '"' {
		input = input[1 : len(input)-1]
	}

	switch input {
	case "latest":
		*t = Latest
	case "pending":
		*t = Pending
	default:
		return fmt.Errorf("invalid tag")
	}

	return nil
}

func (t RealtimeTag) MarshalJSON() ([]byte, error) {
	switch t {
	case Latest:
		return []byte(`"latest"`), nil
	case Pending:
		return []byte(`"pending"`), nil
	default:
		return nil, fmt.Errorf("invalid RealtimeTag value: %d", int64(t))
	}
}
