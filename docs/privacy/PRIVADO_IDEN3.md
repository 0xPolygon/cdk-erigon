# Enterprise Privacy with Privado / Iden3 (Scaffold)

This repository includes an initial scaffold to integrate decentralized identity and selective-disclosure credentials into the node- and contract-layer using the Iden3/PolygonID stack.

What's included:

- Smart contracts (Solidity)
  - Audited Iden3 `State` (upgradeable), via wrapper `zk/privacy/contracts/Iden3StateWrapper.sol`.
  - Audited Iden3 `CredentialAtomicQueryMTPV2Validator` (via wrapper in `zk/privacy/contracts/validators/...`).
  - PolygonID example `ERC20Verifier` copied into `zk/privacy/contracts/examples/ERC20Verifier.sol` as a dev verifier.

- Foundry scripts
  - `zk/privacy/script/DeployAuditedPrivacy.s.sol` — deploys audited Iden3 `State`, the MTP v2 validator, and a dev `ERC20Verifier` using mocks for required verifiers.

- Docker Compose (issuer stack)
  - `docker/privacy/docker-compose.yml` — scaffold for a PolygonID Issuer Node + Postgres + Redis + an optional web UI.

This is a minimal, production-friendly starting point — audited contracts are referenced via Foundry remappings. You can plug in real circuit verifiers later and configure request schemas.

## Contracts overview

1) Iden3 State

- Maintains identity state and GIST roots with ZK state‑transition verification. We deploy it as a standalone upgradeable contract (initializer), with mocks for the transition verifier and cross‑chain validator in dev.

2) Verifier

- Provide your generated circuit verifier(s) and set them on the MTP v2 validator (Groth16) and on application verifiers. The scaffold includes a mock Groth16 verifier for development.

3) CredentialAtomicQueryMTPV2Validator (audited)

- Audited Iden3 validator for MTP v2 circuits. It embeds query and pubSignals checks, uses Iden3 State for GIST/state checks, and delegates Groth16 verification to a circuit verifier address.

## Deploy (Foundry)

From repo root, install dependencies (once per checkout):

```
cd zk/privacy
forge install iden3/contracts OpenZeppelin/openzeppelin-contracts-upgradeable
```

Then build/deploy:

```
cd zk/privacy
forge build
PK=<deployer_private_key_hex> RPC_URL=http://localhost:8545 \
  forge script script/DeployAuditedPrivacy.s.sol:DeployAuditedPrivacy \
  --rpc-url $RPC_URL --broadcast --legacy -vvvv
```

The script prints deployed addresses for: `AnchorRegistry`, `DummyVerifier` (dev), and `CredentialAtomicQueryMTPValidator`.

## Issuer stack (Docker Compose)

The compose file provides a scaffold for the PolygonID issuer node and its dependencies. You need to provide configuration secrets via environment (.env) and mount volumes for issuer data.

```
cd docker/privacy
cp .env.example .env   # fill required values
docker compose up -d
```

Images and exact environment keys may evolve with upstream releases — refer to the official PolygonID/Iden3 documentation for the current variables:

- https://0xpolygonid.github.io/tutorials/issuer-node

## Wiring suggestions

- Application contracts (or an ACL hook) can call the validator’s `validateAtomicQueryMTP(...)` method to authorize actions based on off-chain credentials.
- For node-level ACLs, add an “identity hook” that translates an EOA to an identity (DID), verifies a proof via the validator, and gates permissions accordingly.

## Production notes

- Replace mock verifiers with your circuit-specific verifiers (`IGroth16Verifier`, `IStateTransitionVerifier`, and a real cross-chain validator if needed).
- Consider anchoring with roll-forward semantics and historical windows; add access control for who can update anchors.
- Add event logs for attestation/auditing.
- Use a proxy pattern and a multisig for admin.
