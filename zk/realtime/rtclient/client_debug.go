package rtclient

import (
	"encoding/json"
	"fmt"

	"github.com/ledgerwatch/erigon/zkevm/jsonrpc/client"
)

// RealtimeDumpStateCache dumps the state cache
func (rc *RealtimeClient) RealtimeDumpCache() error {
	response, err := client.JSONRPCCall(rc.url, "debug_realtimeDumpCache")
	if err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return nil
}

// RealtimeDumpStateCache dumps the state cache
func (rc *RealtimeClient) RealtimeCompareStateCache() ([]string, error) {
	response, err := client.JSONRPCCall(rc.url, "debug_realtimeCompareStateCache")
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result RealtimeDebugResult
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result.Mismatches, nil
}
