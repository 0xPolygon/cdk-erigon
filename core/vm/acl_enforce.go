package vm

import (
    libcommon "github.com/erigontech/erigon-lib/common"
)

// ACLCheckSelector is the 4-byte method selector for
// checkPermittedOrRevert(address,address,bytes)
// keccak256("checkPermittedOrRevert(address,address,bytes)")[:4] == 0xbf5afe38
const ACLCheckSelector uint32 = 0xbf5afe38

// ACLTrace is an optional hook for testing/diagnostics. If non-nil, aclEnforce
// invokes it before and after the ACL staticcall with the subject, target, input and error.
var ACLTrace func(stage string, subject, target libcommon.Address, input []byte, err error)

// aclBuildCheckCallData ABI-encodes checkPermittedOrRevert(address,address,bytes)
// Selector: 0xbf5afe38
func aclBuildCheckCallData(subject, target libcommon.Address, payload []byte) []byte {
    headWords := 3
    headSize := 32 * headWords
    tailLen := 32 + ((len(payload)+31)/32)*32
    total := 4 + headSize + tailLen
    out := make([]byte, total)
    // selector
    s := ACLCheckSelector
    out[0] = byte(s >> 24)
    out[1] = byte(s >> 16)
    out[2] = byte(s >> 8)
    out[3] = byte(s)
	// subject
	copy(out[4+12:4+32], subject.Bytes())
	// target
	copy(out[4+32+12:4+64], target.Bytes())
	// offset to bytes = headSize
	off := uint64(headSize)
	for i := 0; i < 8; i++ {
		out[4+64+31-i] = byte(off)
		off >>= 8
	}
	// tail start
	tailStart := 4 + headSize
	// length
	l := uint64(len(payload))
	for i := 0; i < 8; i++ {
		out[tailStart+31-i] = byte(l)
		l >>= 8
	}
	copy(out[tailStart+32:], payload)
	return out
}

// aclEnforce performs a STATICCALL to the ACL contract to validate the call.
// Uses a temporary EVM with RestoreState to avoid mutating state or triggering nested ACL.
func (evm *EVM) aclEnforce(target libcommon.Address, input []byte) error {
	if !evm.config.ACLEnabled {
		return nil
	}
	// Skip enforcement for precompiles
	if p, ok := evm.precompile(target); ok && p != nil {
		return nil
	}
	// Misconfiguration: empty ACL address
	if evm.config.ACLAddress == (libcommon.Address{}) {
		if evm.config.ACLFailOpen {
			return nil
		}
		return ErrExecutionReverted // generic error to abort
	}
	data := aclBuildCheckCallData(evm.Origin, target, input)
	if ACLTrace != nil {
		ACLTrace("before", evm.Origin, target, input, nil)
	}
	const gas uint64 = 500_000
	snap := evm.intraBlockState.Snapshot()
	// prevent recursion during internal staticcall
	prevInternal := evm.config.ACLInternal
	evm.config.ACLInternal = true
	_, _, err := evm.StaticCall(AccountRef(evm.Origin), evm.config.ACLAddress, data, gas)
	evm.config.ACLInternal = prevInternal
	evm.intraBlockState.RevertToSnapshot(snap)
	if ACLTrace != nil {
		ACLTrace("after", evm.Origin, target, input, err)
	}
	if err != nil {
		if evm.config.ACLFailOpen {
			return nil
		}
		return err
	}
	return nil
}
