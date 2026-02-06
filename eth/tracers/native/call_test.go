package native

import (
	"encoding/json"
	"testing"

	"github.com/holiman/uint256"

	libcommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon/core/vm"
	"github.com/erigontech/erigon/eth/tracers"
)

// TestCallTracerDefensiveChecks verifies that the call tracer handles edge cases
// gracefully without panicking, particularly when the callstack is empty.
func TestCallTracerDefensiveChecks(t *testing.T) {
	// Create a call tracer with IncludePrecompiles=false to potentially cause empty callstack
	cfg := json.RawMessage(`{"includePrecompiles": false, "withLog": true}`)
	tracer, err := tracers.New("callTracer", nil, cfg)
	if err != nil {
		t.Fatalf("failed to create call tracer: %v", err)
	}

	// Simulate a precompile call at the top level
	// When includePrecompiles=false and the top-level call is to a precompile,
	// the callstack would be empty after CaptureStart
	tracer.CaptureTxStart(21000)
	tracer.CaptureStart(nil, libcommon.Address{}, libcommon.HexToAddress("0x01"), true, false, nil, 21000, uint256.NewInt(0), nil)

	// Since we can't easily create a ScopeContext without the full EVM,
	// let's verify the tracer was created correctly and can handle
	// GetResult when callstack is empty
	result, err := tracer.GetResult()
	if err != nil {
		// This is expected - "incorrect number of top-level calls" when callstack is empty
		// but it should NOT panic
		t.Logf("GetResult returned expected error: %v", err)
	} else {
		t.Logf("GetResult returned: %s", string(result))
	}

	// Now verify CaptureEnd with empty callstack doesn't panic
	tracer.CaptureEnd(nil, 0, nil)

	// And CaptureTxEnd with empty callstack doesn't panic
	tracer.CaptureTxEnd(21000)
}

// TestCaptureExitDefensiveCheck verifies CaptureExit handles edge case when
// callstack becomes empty after processing
func TestCaptureExitDefensiveCheck(t *testing.T) {
	cfg := json.RawMessage(`{"onlyTopCall": false, "includePrecompiles": true}`)
	tracer, err := tracers.New("callTracer", nil, cfg)
	if err != nil {
		t.Fatalf("failed to create call tracer: %v", err)
	}

	callTracer := tracer.(*callTracer)

	// Initialize properly
	callTracer.CaptureTxStart(21000)

	// Create a minimal valid callstack with one element
	callTracer.callstack = []callFrame{{
		Type: vm.CALL,
		From: libcommon.Address{},
		To:   libcommon.Address{},
	}}
	callTracer.precompiles = []bool{false}

	// CaptureExit should handle the case where size <= 1 gracefully
	callTracer.CaptureExit(nil, 0, nil)

	// Verify no panic occurred
	t.Log("CaptureExit completed without panic")
}

// TestFixLogIndexGapDefensiveCheck tests the bounds check in fixLogIndexGap
func TestFixLogIndexGapDefensiveCheck(t *testing.T) {
	// Test case: log index exceeds cumulativeGaps array length
	// This was causing an out-of-bounds panic before the fix
	cf := &callFrame{
		Logs: []callLog{
			{Index: 100}, // Index way larger than any reasonable cumulativeGaps length
		},
	}

	// Small cumulativeGaps array - the fix should prevent out-of-bounds access
	cumulativeGaps := []uint64{0, 1, 2}

	// This should not panic due to the defensive check
	fixLogIndexGap(cf, cumulativeGaps)

	// Verify the log index was not modified (since it was out of bounds)
	if cf.Logs[0].Index != 100 {
		t.Errorf("expected log index to remain 100, got %d", cf.Logs[0].Index)
	}

	t.Log("fixLogIndexGap completed without panic")
}

// TestClearFailedLogsAndFixGap tests the log gap fixing with cleared failed logs
func TestClearFailedLogsAndFixGap(t *testing.T) {
	// Create a call frame with logs that would fail
	cf := &callFrame{
		Error: "some error",
		Logs: []callLog{
			{Index: 0},
			{Index: 1},
		},
		Calls: []callFrame{
			{
				Logs: []callLog{
					{Index: 2},
				},
			},
		},
	}

	logGaps := make(map[uint64]int)

	// Clear failed logs
	clearFailedLogs(cf, false, logGaps)

	// Verify logs were cleared due to error
	if len(cf.Logs) != 0 {
		t.Errorf("expected logs to be cleared, got %d", len(cf.Logs))
	}

	t.Log("clearFailedLogs completed successfully")
}
