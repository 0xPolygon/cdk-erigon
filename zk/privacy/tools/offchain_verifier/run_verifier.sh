#!/bin/zsh

export PUBLIC_BASE_URL=http://192.168.1.134:8789 # This verifier instance URL
export JWT_SECRET=dummy
export STATE_RESOLVER_CONTRACT=0x69EfD7416E071E528e2951e92C3B30d564cA58e9 # This is state (proxy) address deployed with "forge script script/DeployAuditedPrivacy.s.sol:DeployAuditedPrivacy --rpc-url "$RPC_URL" --private-key "$PK" --broadcast -vvvv" in cdk-erigon folder
export STATE_RESOLVER_NETWORK=polygon:cardona # the one set in issuer node's resolvers_settings.yaml
export STATE_RESOLVER_URL=http://192.168.1.134:62644 # Sequencer/RPC URL

# go run ./main.go
go build
./offchainverifier
