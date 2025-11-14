package vm

import (
    libcommon "github.com/erigontech/erigon-lib/common"
    "github.com/erigontech/erigon-lib/log/v3"
    "github.com/holiman/uint256"
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

// ACLOwnerSelector is the 4-byte selector for owner()
// keccak256("owner()")[:4] == 0x8da5cb5b
const ACLOwnerSelector uint32 = 0x8da5cb5b

// aclBuildOwnerCallData ABI-encodes owner() call data
func aclBuildOwnerCallData() []byte {
    out := make([]byte, 4)
    s := ACLOwnerSelector
    out[0] = byte(s >> 24)
    out[1] = byte(s >> 16)
    out[2] = byte(s >> 8)
    out[3] = byte(s)
    return out
}

// eip1967ImplSlot is bytes32(uint256(keccak256('eip1967.proxy.implementation')) - 1)
var eip1967ImplSlot = libcommon.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")

// aclImplementationAddress reads the EIP-1967 implementation address of the configured ACL proxy.
func (evm *EVM) aclImplementationAddress() libcommon.Address {
    var out uint256.Int
    evm.intraBlockState.GetState(evm.config.ACL.Address, &eip1967ImplSlot, &out)
    if out.IsZero() {
        return libcommon.Address{}
    }
    b := out.Bytes32()
    var impl libcommon.Address
    copy(impl[:], b[12:32])
    return impl
}

// aclRegistryAddress attempts to read storage slot 0 of the ACL proxy, which in our checker layout
// holds `address public registry`. Since the checker runs via DELEGATECALL in the proxy, the storage
// resides at the proxy address.
func (evm *EVM) aclRegistryAddress() libcommon.Address {
    var slot libcommon.Hash // zero slot
    var out uint256.Int
    evm.intraBlockState.GetState(evm.config.ACL.Address, &slot, &out)
    if out.IsZero() {
        return libcommon.Address{}
    }
    b := out.Bytes32()
    var reg libcommon.Address
    copy(reg[:], b[12:32])
    return reg
}

func (evm *EVM) aclInBypassList(addr libcommon.Address) bool {
    if len(evm.config.ACL.Bypass) == 0 {
        return false
    }
    for _, a := range evm.config.ACL.Bypass {
        if a == addr {
            return true
        }
    }
    return false
}

// aclIsOwner checks whether subject equals owner() of the ACL proxy when OwnerBypass is enabled.
func (evm *EVM) aclIsOwner(subject libcommon.Address) bool {
    if !evm.config.ACL.OwnerBypass {
        return false
    }
    if evm.config.ACL.Address == (libcommon.Address{}) {
        return false
    }
    data := aclBuildOwnerCallData()
    const gas uint64 = 50_000
    snap := evm.intraBlockState.Snapshot()
    prevInternal := evm.config.ACL.Internal
    evm.config.ACL.Internal = true
    // Perform STATICCALL to ACL proxy
    ret, _, err := evm.StaticCall(AccountRef(evm.Origin), evm.config.ACL.Address, data, gas)
    evm.config.ACL.Internal = prevInternal
    evm.intraBlockState.RevertToSnapshot(snap)
    if err != nil || len(ret) < 32 {
        return false
    }
    var owner libcommon.Address
    copy(owner[:], ret[12:32])
    return owner == subject
}

// aclEnforce performs a STATICCALL to the ACL contract to validate the call.
// Uses a temporary EVM with RestoreState to avoid mutating state or triggering nested ACL.
func (evm *EVM) aclEnforce(target libcommon.Address, input []byte) error {
    if !evm.config.ACL.Enabled {
        return nil
    }
    // Never gate direct calls to the ACL contract, its implementation (EIP-1967), or its registry.
    if target == evm.config.ACL.Address || target == evm.aclImplementationAddress() || target == evm.aclRegistryAddress() {
        return nil
    }
    // Skip enforcement when origin is zero (simulation tools often use zero address)
    if evm.Origin == (libcommon.Address{}) {
        return nil
    }
    // Superuser bypass: explicit list or owner (if enabled)
    if evm.aclInBypassList(evm.Origin) || evm.aclIsOwner(evm.Origin) {
        return nil
    }
    // Skip enforcement for precompiles
    if p, ok := evm.precompile(target); ok && p != nil {
        return nil
    }
    // Misconfiguration: empty ACL address
	if evm.config.ACL.Address == (libcommon.Address{}) {
		if evm.config.ACL.FailOpen {
			return nil
		}
		return ErrExecutionReverted // generic error to abort
	}
    data := aclBuildCheckCallData(evm.Origin, target, input)
    log.Info("ACL enforce: start", "origin", evm.Origin, "target", target, "acl", evm.config.ACL.Address, "failOpen", evm.config.ACL.FailOpen, "internal", evm.config.ACL.Internal, "calldata_len", len(input))
	if ACLTrace != nil {
		ACLTrace("before", evm.Origin, target, input, nil)
	}
	const gas uint64 = 500_000
	snap := evm.intraBlockState.Snapshot()
	// prevent recursion during internal staticcall
    prevInternal := evm.config.ACL.Internal
    evm.config.ACL.Internal = true
    _, _, err := evm.StaticCall(AccountRef(evm.Origin), evm.config.ACL.Address, data, gas)
    evm.config.ACL.Internal = prevInternal
	evm.intraBlockState.RevertToSnapshot(snap)
	if ACLTrace != nil {
		ACLTrace("after", evm.Origin, target, input, err)
	}
    if err != nil {
        log.Info("ACL enforce: denied", "err", err)
        if evm.config.ACL.FailOpen {
            return nil
        }
        return err
    }
    log.Info("ACL enforce: allowed")
    return nil
}
