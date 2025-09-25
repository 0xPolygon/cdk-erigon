# ACL Firewall (MVP)

Goal: Perimeter security for enterprise privacy. Only authorized subjects (senders) can send transactions, constrained by target contract, method (selector), and optionally by parameters. Implemented as an on-chain ACL that the node consults to admit or reject transactions.

## Components

- `core/state/contracts/acl/AccessControlFirewall.sol` — logic contract with bitmap-based selector allowlist plus optional calldata prefix mask/value constraints.
- `core/state/contracts/acl/AdminUpgradeableProxy.sol` — minimal transparent upgradeable proxy (EIP‑1967 slots).
- `core/state/contracts/acl/ProxyAdmin.sol` — admin controller for proxies (recommend multisig ownership).
- `core/state/contracts/acl/IAccessControlFirewall.sol` — interface for integration.
- `core/state/contracts/acl/CalldataMask.sol` — library for `(calldata & mask) == value` checks.

## Permission Model

- Keyed by `(subject, target, selector)`.
- Selector storage uses sparse bitmaps: `bucket = selector >> 8` (uint24), `bitIndex = selector & 0xFF`.
- Optional parameter constraint per `(subject, target, selector)`: mask/value applied to the first `mask.length` bytes of calldata; permitted if `(calldata & mask) == value`.
- Special cases:
  - Value-only transfers (`calldata.length < 4`): treated as selector `0x00000000`.
  - Contract creation: `target == address(0)` requires `grantContractCreation(subject)`.
  - `grantAnySelector(subject, target)` flags all selectors to a target as allowed for the subject.

## Admin API (selected)

- `grantSelector(subject, target, selector)` / `revokeSelector(...)`
- `grantAnySelector(subject, target)` / `revokeAnySelector(...)`
- `setParamConstraint(subject, target, selector, mask, value)` — set or clear (`mask.length == 0`).
- `grantContractCreation(subject)` / `revokeContractCreation(subject)`

## Read/Check API

- `isPermitted(subject, target, data) -> bool`
- `checkPermittedOrRevert(subject, target, data) -> bool` (reverts `ACL: denied` if not allowed)

## Deployment

1. Deploy `AccessControlFirewall` logic contract.
2. Deploy `ProxyAdmin` with organization owner (ideally Gnosis Safe).
3. Deploy `AdminUpgradeableProxy` with logic, `ProxyAdmin` as admin, and initializer call data: `AccessControlFirewall.initialize(owner)`.
4. Interact with the proxy address (not the logic) to manage permissions.

Example (pseudo):

```
// grant subject S to call target T:foo(bytes32) with selector 0x12345678
acl.grantSelector(S, T, 0x12345678);

// add a parameter constraint on first 36 bytes (4-byte selector + 32-byte arg) matching a masked prefix
bytes mask = hex"fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff0";
bytes value = hex"12345678....................................................a0"; // example
acl.setParamConstraint(S, T, 0x12345678, mask, value);
```

## Node Integration (Erigon/CDK)

MVP path (fail-closed recommended):

- Config: add `ACL` feature flag and `ACLContractAddress` (see `eth/ethconfig/acl.go`).
- Admission control: when processing a top-level transaction, do a read-only `STATICCALL` to the ACL proxy:
  - `checkPermittedOrRevert(tx.from, tx.to, tx.data)`
  - For contract creation, `to == address(0)` and pass `tx.data` as init code; ACL checks `_canCreate[from]`.
  - If call reverts with `ACL: denied`, reject the transaction before execution.

Future hardening (nested calls):

- EVM instrumentation in CALL/DELEGATECALL/STATICCALL/CALLCODE opcodes to enforce ACL on every external interaction:
  - Track the original subject (EOA sender) in EVM context.
  - Before each external call, query ACL with `(subject, callee, calldata)`.
  - For DELEGATECALL, enforce the policy against the delegate target as if it were the external target (prevents wrapper bypasses).
  - For CREATE/CREATE2, check `subject` has `canCreate` permission and optionally constrain init code (using mask rules if desired).

Performance notes:

- ACL queries are `STATICCALL` to a local proxy; cache `(subject,target,selector)` positives per block to amortize reads.
- Use `grantAnySelector` for high-churn targets where per-selector checks aren’t necessary.

## Identity & ZK Considerations (Privado / Iden3)

This MVP is address-based, but designed to extend to identity proofs:

- Subject abstraction: add an optional identity hook contract (`IIdentityHook`) the ACL can consult to validate that `msg.sender` (or an asserted identity) satisfies a policy (group membership, KYC level, etc.).
- Iden3: integrate an on-chain verifier (Iden3/PolygonID) to validate zero-knowledge credentials for group/attribute membership; successful verification can set or time-bound a permission for an address.
- Privado: use off-chain private policy evaluation with an on-chain attestation/allowlist update to reflect the outcome; ACL remains the enforcement point.

These can be introduced without changing the bitmap core: permissions still map to `(subject,target,selector)`, but subject derivation can be identity-aware.

## Security Guidance

- Run ACL through an upgradeable proxy managed by a multisig (`ProxyAdmin`).
- Fail-closed at the node admission layer.
- Use events to audit policy changes.
- Keep constraints as short masks on calldata prefixes for gas efficiency and simplicity.

