# Ethereum Hardfork Mode

By default CDK-Erigon runs in `hermez` mode which uses specific ZK features such as ZK counters and modifications to the EVM. You can enable `ethereumHardfork` mode to turn these features off.

To enabled `ethereumHardfork` mode you must enable it through the `chainspec` configuration like this:

_dynamic-{network}-chainspec.json_
```json
{
  // other configuration
  "ethereumHardforkBlock": 0
}
```

### ZK Counters

When in `ethereumHardfork` mode, the ZK counters are disabled. This means that the ZK counters will not be used in the ZK proof generation and verification process. This is important for compatibility with Ethereum, as Ethereum does not use ZK counters.

### EVM Modifications

When in `ethereumHardfork` mode, the EVM modifications are disabled. This means that the EVM will behave like the Ethereum EVM, and will not use any of the ZK specific modifications. This is important for compatibility with Ethereum, as Ethereum does not use these modifications.

In `ethereumHardfork` mode. The opcode `SELFDESTRUCT` is replaced with `SENDALL` on all forks. This means that the contract code will still be available after the contract is destroyed.

***

## Configurable Flags

There are flags that can be configured to enable specific features for `ethereumHardfork` mode. These flags are:

**Name:**

`zkevm.commitment`

**Type:**

`string`

**Options:**

- `smt`
- `pmt`

**Default:**

- `smt`

**Description:**

The flag `zkevm.commitment` is used to specify the type merkle tree to use in the interhashing stage. The `smt` will use the Sparse Merkle Tree whioch is the default option. The `pmt` will use the Patricia Merkle Tree which is the Ethereum default. The `pmt` option can be used in combination with `ethereumHardfork` mode to give the node the same behavior as Ethereum. This is important for compatibility with Ethereum, as Ethereum uses the Patricia Merkle Tree.

***