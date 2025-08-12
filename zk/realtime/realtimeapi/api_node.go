package realtimeapi

import (
	"context"
)

// Returns the status on whether the RT feature is enabled (when tag provided)
func (api *RealtimeAPIImpl) RealtimeEnabled(ctx context.Context) (bool, error) {
	return (api.cacheDB != nil && api.cacheDB.ReadyFlag.Load()), nil
}
