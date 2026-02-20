# CDK-Erigon Upgrade & Configuration Guide (SOP)

This guide outlines the **Standard Operating Procedure (SOP)** for managing node configurations and performing robust version upgrades using the `cdk-config` toolset.

## 🛠 Prerequisites

Build the binary from the root of the `cdk-erigon` repository:
```bash
make cdk-config
```

---

## 🏎 The Ideal Flow: Step-by-Step

### Phase 1: Pre-Upgrade Analysis
Before installing a new binary, analyze your current state to identify the required path.

1.  **Discover Path**: Use `list-migrations` to see what forks are active in your DB and if any upgrades are available.
    ```bash
    ./cdk-config list-migrations --datadir /path/to/data
    ```
    *Why?* Identifies if you are moving from SMT to PMT (Type-1) or enabling Sovereign mode.

2.  **Verify State**: Run `doctor` to ensure your current config matches your current DB.
    ```bash
    ./cdk-config doctor --config config.yaml --datadir /path/to/data
    ```

### Phase 2: Configuration Migration
Modernize your configuration file for the new binary version.

1.  **Migrate Flags**: Automatically rename deprecated flags and apply network profiles.
    ```bash
    ./cdk-config migrate --config config.yaml --to Type-1
    ```
    *Why?* This creates a backup, renames old flags (e.g., `gasless` -> `allow-free-transactions`), and applies the `zkevm.mode` profile.

### Phase 3: Pre-Flight Validation (CRITICAL)
Validate the migrated config against the database **before** starting the node.

1.  **Alignment Audit**: Run `doctor` again.
    ```bash
    ./cdk-config doctor --config config.yaml --datadir /path/to/data
    ```
    *Why?* This catches "Impossible Migrations" where your config file specifies a fork height that contradicts what is already recorded in the database.

### Phase 4: Post-Migration Cleanup
Once the node has started and synced past the activation block, lean out your configuration.

1.  **Cleanup Advice**: Run `doctor` one last time.
    ```bash
    ./cdk-config doctor --config config.yaml --datadir /path/to/data
    ```
    *Why?* Once the migration is healthy and finished, the tool will suggest removing transitional flags (like `zkevm.simultaneous-pmt-and-smt`) to keep your configuration clean and performant.

---

## 📋 Command Reference

| Command | Purpose | When to use |
| :--- | :--- | :--- |
| `check` | Syntax & mandatory fields | Any time you edit the YAML manually. |
| `migrate` | Flag renaming & profiles | During version upgrades. |
| `doctor` | Config vs. DB alignment | **Pre-flight** (prevent failure) & **Post-flight** (cleanup). |
| `list-migrations` | DB fork discovery | To understand which "Path" a network is on. |
| `verify-evm` | Data integrity check | For deep audits of block/header compliance. |

## 🌟 The "Gold Standard": `zkevm.mode`

Always prefer using `zkevm.mode` (e.g., `Type-1`, `FEP`, `PP`) in your config. The `cdk-erigon` node uses this to automatically apply internal defaults, reducing the surface area for manual configuration errors.
