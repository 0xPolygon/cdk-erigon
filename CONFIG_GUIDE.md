# CDK-Erigon Config Toolset: Quick Start Guide

The `cdk-config` tool is a standalone utility for managing, auditing, and migrating CDK-Erigon configurations.

## 🛠 Installation

Build the binary from the root of the `cdk-erigon` repository:
```bash
go build -o cdk-config ./cmd/cdk-config
```

## 📋 Commands Overview

### 1. `check`: Syntax & Readiness
Validates that the YAML/TOML file is readable and contains all mandatory fields for its detected network type.
```bash
./cdk-config check --config my-config.yaml
```

### 2. `migrate`: Automated Upgrades
Renames deprecated flags to their modern equivalents and creates a timestamped backup of your original file.
```bash
./cdk-config migrate --config my-config.yaml
```
**Apply Profile:** Force a specific network mode (e.g., Type-1):
```bash
./cdk-config migrate --config my-config.yaml --to Type-1
```

### 3. `doctor`: Deep Diagnostics
The "Doctor" cross-references your configuration with the actual state of your database. It catches mismatched fork heights and migration risks.
```bash
./cdk-config doctor --config my-config.yaml --datadir /path/to/data
```

### 4. `verify-evm`: Integrity Auditor
Verifies that the existing data in your `--datadir` is compliant with the Ethereum/ZK forks.
```bash
./cdk-config verify-evm --datadir /path/to/data
```

## 🌟 The "Gold Standard": `zkevm.mode`

The recommended way to configure `cdk-erigon` is using the `zkevm.mode` flag. This applies tested defaults for specific network types.

**Available Modes**: `FEP` (Full Execution Proof), `PP` (Pessimistic Prover), `Sovereign`, `Type-1`.

---

## 🏗 Pre-flight Check for Upgrades

Before restarting your node after a version upgrade, run:
1. `cdk-config migrate` (Modernize flags)
2. `cdk-config doctor` (Check DB alignment)

This prevents "Impossible Migrations" and ensures your node starts with a healthy state.
