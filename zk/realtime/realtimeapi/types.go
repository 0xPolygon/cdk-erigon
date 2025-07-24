package realtimeapi

type DebugResult struct {
	ConfirmHeight   uint64   `json:"confirmHeight"`
	ExecutionHeight uint64   `json:"executionHeight"`
	Mismatches      []string `json:"mismatches"`
}
