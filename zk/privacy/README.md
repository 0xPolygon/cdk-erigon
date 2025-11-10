## Offchain Verification Demo (Privado/PolygonID)

This doc describes POC how to gate `eth_call` with an offchain proof using the Privado/PolygonID wallet, and how to run the supporting components locally.

### 1) Run the Sequencer with Offchain Gating

Enable free txs in your chain config (needed for updating state in contracts or fund verifier):

```text
zkevm.allow-free-transactions: true
```

Use your own values where appropriate.

```bash
# OFFCHAIN_VERIFIER_URL     – URL of the offchain verifier (this repo’s tool)
# OFFCHAIN_VERIFIER_BYPASS_TO – Comma-separated list of contract addresses that
#                                are allowed to be called without verification
#                                (e.g., your State/Anchor contract used by the resolver).
# OFFCHAIN_LOG_AUTH         – When set to 1/true, logs Authorization JWT and decodes header/payload (debug only).

OFFCHAIN_VERIFIER_URL=http://192.168.1.134:8789 \
OFFCHAIN_LOG_AUTH=1 \
OFFCHAIN_VERIFIER_BYPASS_TO=0x69EfD7416E071E528e2951e92C3B30d564cA58e9 \
CDK_ERIGON_SEQUENCER=1 \
./build/bin/cdk-erigon --config=../../gateway/integration8/dynamic-integration8-type-1.yaml
```


### 2) Deploy the State (Anchor) Proxy (optional)

If you don’t already have a State proxy, deploy audited contracts and use the proxy address printed by the script.

```bash
forge script script/DeployAuditedPrivacy.s.sol:DeployAuditedPrivacy \
  --rpc-url "$RPC_URL" --private-key "$PK" --broadcast -vvvv
```

### 3) Run the Offchain Verifier

Configure and run the verifier contained in this repo.

```bash
./run_verifier.sh
```

Verifier must know:
- `PUBLIC_BASE_URL` (e.g., http://192.168.1.134:8789)
- `JWT_SECRET`
- `STATE_RESOLVER_NETWORK`, `STATE_RESOLVER_URL`, `STATE_RESOLVER_CONTRACT`
- Optional: `IPFS_GATEWAY`, `VERIFIER_DID`, `OFFCHAIN_EMAIL`, `OFFCHAIN_ORG`

### 4) Configure and Run Issuer Node

Use the issuer-node repo: `git@github.com:0xPolygonID/issuer-node.git`.

Example `resolvers-settings.yaml` (CDK chain presented as `polygon:cardona` because I could not make it work with custom chain):

```yaml
# Configure Privado Identity Chain – required by the Privado Wallet
privado:
  main:
    contractAddress: 0x3C9acB2205Aa72A05F6D77d708b5Cf85FCa3a896
    networkURL: https://rpc-mainnet.privado.id
    defaultGasLimit: 600000
    confirmationTimeout: 10s
    confirmationBlockCount: 5
    receiptTimeout: 600s
    minGasPrice: 0
    maxGasPrice: 1000000
    rpcResponseTimeout: 5s
    waitReceiptCycleTime: 30s
    waitBlockCycleTime: 30s
    gasLess: false
    rhsSettings:
      mode: None
      contractAddress: 0x7dF78ED37d0B39Ffb6d4D527Bb1865Bf85B60f81
      rhsUrl: https://rhs-staging.polygonid.me
      chainID: 21000
      publishingKey: pbkey

polygon:
  cardona:
    contractAddress: "0x69efd7416e071e528e2951e92c3b30d564ca58e9"   # State (proxy)
    networkURL: "http://192.168.1.134:62644"                         # your L2 RPC
    chainID: 779
    defaultGasLimit: 600000
    confirmationTimeout: "10s"
    confirmationBlockCount: 5
    receiptTimeout: "600s"
    minGasPrice: 0
    maxGasPrice: 1000000
    rpcResponseTimeout: "5s"
    waitReceiptCycleTime: "30s"
    waitBlockCycleTime: "30s"
    gasLess: false
    rhsSettings:
      mode: None
      contractAddress: ""
      rhsUrl: ""
      chainID: 779
      publishingKey: ""
```

Issuer creds (optional, for UI):

```bash
export ISSUER_PASS=password-issuer
export ISSUER_USER=user-issuer
export ISSUER_URL=http://192.168.1.134:3001
```

Run the issuer:

```bash
make run-all
```

### 5) Issue a Credential

Use the helper script from this repo to issue an OrganizationMembership VC.

```bash
export HOLDER_DID=did:iden3:privado:main:2ShDQXZAaptdgWgpp8RqwfTrqKYbuf1DTVF6CcMeG5
./creds_flow_membership.sh
```

### 6) Deploy an ERC‑20 for Demo Calls

```bash
forge create ./lib/contracts/contracts/test-helpers/ERC20Token.sol:ERC20Token \
  --broadcast --rpc-url $RPC_URL \
  --private-key $WALLET_PRIVATE_KEY \
  --constructor-args 1000
```

### 7) Trigger a Gated `eth_call` and Complete the Proof

The helper script makes a call, receives a -32051 “verification required”, prints a universal link/QR, waits for the proof, then replays the call with the JWT.

```bash
./zk_build_verifier_link.sh \
  --to 0xe0eF45aafA1B27B7664bb35a950DCf863dE54a57 \
  --data 0x70a082310000000000000000000000000000000000000000000000000000000000000000 \
  --block latest \
  --wait \
  --timeout 180
```

Flow:
- Script calls `eth_call` → node returns -32051 with `challengeId` and `url`.
- Scan the QR in the Privado Wallet and generate the proof.
- Script polls the verifier, prints the JWT, decodes header/payload, and replays `eth_call` with `Authorization: Bearer <jwt>`.
- Final result is printed from the sequencer.

### Notes & Tips

- If you see resolver calls to your State contract reverting with “State does not exist”, it means the issuer state hasn’t been anchored yet. Proofs may still succeed (genesis) but anchoring removes the noise.
- Keep `OFFCHAIN_VERIFIER_BYPASS_TO` set to your State proxy address so the resolver’s `eth_call`s are not themselves gated.
- For debugging JWT on the node side, set `OFFCHAIN_LOG_AUTH=1` to log and decode the token header/payload in `eth_call`.
- No JWT verification for now in cdk-erigon, it just happily accepts any Authorization header
