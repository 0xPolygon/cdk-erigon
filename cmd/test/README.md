# Test

Test is a command line tool designed to test the node. This can be used in CI pipelines.

### Usage

```bash
test [global flags] <command> [command flags]
```

| Flag              | Description            |
| ----------------- | ---------------------- |
| `-h`, `--help`    | Show help message      |


### Commands

- `selfdestruct`

Test the SELFDESTRUCT opcode. This should never destroy the contract code; instead it runs SENDALL to the recipient’s address.

| Flag         | Description                   |
|--------------|-------------------------------|
| `--rpc-url`  | The RPC url                   |
| `--priv-key` | The private key of the sender |
| `--address`  | The address for the recipient |
