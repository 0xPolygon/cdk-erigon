# EVM Enforcement Plan

This document outlines how to harden node-level ACL enforcement to close delegatecall and nested-call bypasses.

## Goals

- Enforce ACL for all external interactions triggered by a top-level transaction, not just the entry call.
- Prevent bypasses via wrapper contracts using DELEGATECALL / CALLCODE.
- Support CREATE/CREATE2 gating.

## High-Level Approach

1. Track the top-level subject (EOA) in the EVM execution context.
   - Add a field (e.g., `Subject common.Address`) to the EVM or CallContext set from `tx.From` at transaction start.

2. On every external call opcode (CALL, CALLCODE, DELEGATECALL, STATICCALL):
   - Determine the callee `to` and the call data `input`.
   - Compute `(subject, target, data)` as:
     - For CALL/STATICCALL: `target = to`, `data = input`.
     - For DELEGATECALL/CALLCODE: `target = to` (delegate target), `data = input`.
   - Do a read-only check via the ACL contract: `checkPermittedOrRevert(subject, target, data)` using a `STATICCALL`.
   - If not permitted, revert the frame with a well-formed error (bubble up).

3. For CREATE/CREATE2:
   - Before creating code, call `checkPermittedOrRevert(subject, address(0), initCode)`.
   - Optionally add allowlisted templates (init code mask rules) in the ACL for finer control.

## Implementation Pointers (Erigon/CDK)

- Hook points:
  - `core/state_transition.go`: capture `tx.From` and pass to EVM as subject.
  - `core/evm.go` (or equivalent opcode handlers): insert checks in handlers for CALL-family opcodes and CREATE-family opcodes.
  - If a staticcall is too costly per opcode, add a precompile address (e.g., `0x000...ACL`) for a cheaper in-client state read to the ACL contract storage. The MVP path uses the proxy with `STATICCALL` for simplicity.

- Caching:
  - Cache positive `(subject, target, selector)` results for the lifetime of a transaction context (or per block), invalidated on reorg.
  - Respect constraints: cache entries must include the selector and whether a constraint exists; don’t cache constrained selectors without checking the actual calldata mask value.

- Config flags (see `eth/ethconfig/acl.go`):
  - `Enabled` and `ContractAddress` must be set to activate checks.
  - `FailOpen` for controlled environments; default should be fail-closed.

## Edge Cases

- Value-only CALL (no input): selector = `0x00000000`.
- Precompiles: optionally exempt precompile addresses or explicitly grant them.
- Re-entrancy: checks apply per outgoing call, regardless of depth.
- Delegatecall into libraries: treat as a call to the library target; enforce ACL accordingly.

## Testing Strategy

- Unit tests for selector bitmaps and constraints in Solidity.
- Integration tests using local Erigon instance with ACL enabled:
  - Allowed and denied top-level calls.
  - Wrapper bypass attempts via DELEGATECALL and CALL.
  - CREATE/CREATE2 with and without permission.
  - Performance baseline with caching.

