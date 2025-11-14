# ACL Deployment

This document describes how to deploy the RBAC-based ACL stack using OpenZeppelin proxies. The node continues to call `checkPermittedOrRevert(subject,target,data)` exactly as before, so the client configuration stays the same once you point `--acl.address` at the proxy.

## Components

- `AccessControlRBACRegistry` stores organisations, contracts, roles, groups and policies.
- `AccessControlRBACChecker` is the read-only facade that the node `STATICCALL`s via `checkPermittedOrRevert`.
- Both contracts are deployed behind `ERC1967Proxy` instances and implement the UUPS pattern, so upgrades are authorized directly by the on-chain owner (no `ProxyAdmin` indirection).

## Deployment Steps (Forge)

1. Install dependencies: from `zk/tests/acl` run `forge install` if you haven’t already. That pulls in OpenZeppelin contracts under `lib/openzeppelin-contracts` (the build configuration already references them via remappings).
2. Set environment variables for your deployer private key (e.g., `export PK=0xabc...`).
3. Run the Forge deployment script:

```sh
cd core/state/contracts/acl
forge script script/deploy_prod.s.sol:DeployACL --broadcast --private-key $PK --rpc-url $RPC_URL
```

The script:

1. Deploys the registry implementation, wraps it in an `ERC1967Proxy`, and calls `initialize(owner)` so that `owner` becomes the administrator.
2. Deploys the checker implementation, wraps it in an `ERC1967Proxy`, and initializes it with the registry proxy address.
3. Emits a JSON file with the deployed addresses at `out/acl.addresses.json` for automation (`proxy` = checker proxy, `registry` = registry proxy, `logic`/`registryLogic` = implementations).

> **Tip:** set `DEPLOY_SAMPLE_TARGETS=1` before running the script if you need the sample `ATarget`/`BTarget` contracts for local testing. In production you can leave it unset so no sample targets are deployed.

## Configuring the node

Once deployed:

1. Pass `--acl.enable` and `--acl.address=<proxy address>` to the node (the proxy address is `acl.address` from the JSON output).
2. Set `--acl.failopen=false` (recommended fail-closed). Optionally configure `--acl.bypass` and `--acl.owner-bypass` for emergency accounts.

## Registering protected contracts

Once the ACL stack is live you still need to bind the contracts you want to protect:

1. Deploy or identify the target contract(s) in your normal workflow.
2. Choose an organisation (`orgId = keccak256(bytes(name))`) and decide which role (`POLICY_READER`, `POLICY_WRITER`, `POLICY_ADMIN`) gate access to that contract.
3. Use the registry to bind and policy-control each contract. Example `cast` commands:

   ```sh
   cast send --rpc-url $RPC --private-key $PK <registry> \
     "bindContractToOrg(address,bytes32)" <contract-address> <orgId>
   cast send --rpc-url $RPC --private-key $PK <registry> \
     "setContractDefaultPolicy(address,uint8)" <contract-address> 2
   cast send --rpc-url $RPC --private-key $PK <registry> \
     "grantRole(bytes32,address,uint256)" <orgId> <user-address> <role-bit>
   ```

4. Optionally call `setCreatePermission(user, true)` for accounts that must deploy new contracts (CREATE/CREATE2 is also ACL’d).

After these steps the transparent proxy will reject any call that violates the policy, and the node-side ACL enforcement (in `core/vm`) stays unchanged.

## Upgrades and ownership

- Upgrade the checker logic by deploying a new `AccessControlRBACChecker` and invoking `upgradeTo` (or `upgradeToAndCall`) via the proxy. `_authorizeUpgrade` restricts upgrades to the contract owner.
- Transfer ownership of the registry or checker via their `transferOwnership` entry points (from OZ `OwnableUpgradeable` where applicable).

## Testing

Run `forge test --profile default` from `zk/tests/acl` to exercise the ACL harness with the deployment script.

For full-node coverage, the workflow `.github/workflows/acl-erigon-e2e.yml` builds `cdk-erigon`, deploys the RBAC stack via `core/state/contracts/acl/script/deploy_prod.s.sol`, configures org/policies, reruns the node with `--acl.enable --acl.address=<proxy>`, and sends writer/stranger transactions to verify enforcement.
