package types

import "encoding/json"

type ErrorTriggerMessage struct {
	BlockNumber uint64 `json:"blockNumber"`
}

func (msg ErrorTriggerMessage) MarshalJSON() ([]byte, error) {
	type ErrorTriggerMessage struct {
		BlockNumber uint64 `json:"blockNumber"`
	}

	var enc ErrorTriggerMessage
	enc.BlockNumber = msg.BlockNumber

	return json.Marshal(&enc)
}
