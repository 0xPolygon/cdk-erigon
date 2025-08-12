package types

import (
	"encoding/json"
	"fmt"

	ethTypes "github.com/ledgerwatch/erigon/core/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
)

type BlockMessage struct {
	Header        *ethTypes.Header
	PrevBlockInfo *realtimeTypes.BlockInfo
}

func (msg BlockMessage) Validate(executionHeight uint64) error {
	if _, _, err := msg.GetBlockInfo(); err != nil {
		return err
	}
	if msg.Header.Number.Uint64() <= executionHeight {
		// Ignore block msgs from previous blocks
		return fmt.Errorf("received old block message, blockNum: %d executionHeight: %d", msg.Header.Number.Uint64(), executionHeight)
	}

	return nil
}

func (msg BlockMessage) GetBlockInfo() (*ethTypes.Header, *realtimeTypes.BlockInfo, error) {
	if msg.Header == nil {
		return nil, nil, fmt.Errorf("header is nil")
	}
	if msg.Header.Number.Uint64() == 0 {
		return nil, nil, fmt.Errorf("block number is 0")
	}
	if msg.PrevBlockInfo == nil {
		return nil, nil, fmt.Errorf("prev block info is nil")
	}

	return msg.Header, msg.PrevBlockInfo, nil
}

func (msg BlockMessage) MarshalJSON() ([]byte, error) {
	type BlockMessage struct {
		Header        *ethTypes.Header         `json:"header"`
		PrevBlockInfo *realtimeTypes.BlockInfo `json:"prevBlockInfo"`
	}

	var enc BlockMessage
	enc.Header = msg.Header
	enc.PrevBlockInfo = msg.PrevBlockInfo

	return json.Marshal(&enc)
}
