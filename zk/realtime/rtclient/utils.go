package rtclient

import (
	"encoding/json"
	"strconv"
)

func transHexToUint64(hex json.RawMessage) (uint64, error) {
	var result string
	err := json.Unmarshal(hex, &result)
	if err != nil {
		return 0, err
	}

	if len(result) > 1 && (result[:2] == "0x" || result[:2] == "0X") {
		result = result[2:]
	}

	result1, err := strconv.ParseUint(result, 16, 64)
	if err != nil {
		return 0, err
	}

	return result1, nil
}
