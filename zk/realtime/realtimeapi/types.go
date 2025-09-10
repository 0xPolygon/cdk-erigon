package realtimeapi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ledgerwatch/erigon/core/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

type RealtimeSubResult struct {
	Header    *types.Header      `json:"Header,omitempty"`
	TxHash    string             `json:"TxHash,omitempty"`
	TxData    types.Transaction  `json:"TxData,omitempty"`
	Receipt   *types.Receipt     `json:"Receipt,omitempty"`
	InnerTxs  []*zktypes.InnerTx `json:"InnerTxs,omitempty"`
	BlockTime uint64             `json:"BlockTime,omitempty"`
}

func (r *RealtimeSubResult) UnmarshalJSON(data []byte) error {
	type TempRealtimeSubResult struct {
		Header    *types.Header      `json:"Header,omitempty"`
		TxHash    string             `json:"TxHash,omitempty"`
		TxData    json.RawMessage    `json:"TxData,omitempty"`
		Receipt   *types.Receipt     `json:"Receipt,omitempty"`
		InnerTxs  []*zktypes.InnerTx `json:"InnerTxs,omitempty"`
		BlockTime uint64             `json:"BlockTime,omitempty"`
	}

	var temp TempRealtimeSubResult
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	r.Header = temp.Header
	r.TxHash = temp.TxHash
	r.Receipt = temp.Receipt
	r.InnerTxs = temp.InnerTxs
	r.BlockTime = temp.BlockTime

	if len(temp.TxData) > 0 {
		// Unmarshal the raw TxData into types.Transaction
		r.TxData, _ = types.UnmarshalTransactionFromJSON(temp.TxData)
	}

	return nil
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
