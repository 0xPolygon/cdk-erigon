# Token Manager Scripts Usage Guide

## 📋 Overview

This directory contains deployment and testing scripts for the Token Manager system, providing complete contract deployment, configuration, and functionality verification processes.

## 🚀 Quick Start

```bash
# 1. Transfer from Genesis deploy account to 0xDE282DC882bbB5100b8A24E30D38a2D5B3080c15
cast send 0xDE282DC882bbB5100b8A24E30D38a2D5B3080c15 --value 10ether --private-key 0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2 --rpc-url http://127.0.0.1:8123 --legacy

# 2. Deploy contracts
# Where
#PROXY_ADMIN="0xDE282DC882bbB5100b8A24E30D38a2D5B3080c15"   # Proxy admin, private key is 0x9935c242a0b0ee41edcbd2d963f5bc7f142fdc803eb24f0df396a6fdb16c6af9
#OWNER_ADDRESS="0xDE282DC882bbB5100b8A24E30D38a2D5B3080c15"  # Contract Owner, same private key
#ADMIN_ADDRESS="0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534"  # Business Admin, private key is 0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2
./deploy_tokenmanager.sh

# 3. Local testing
./test_tokenmanager.sh

# This enables complete testing.
```

### Prerequisites

1. **Erigon Node Running**: Ensure X Layer Erigon node is running
2. **RPC Access**: Ensure access to node RPC endpoint (default: `http://localhost:8123`)
3. **Private Key Preparation**: Prepare deployment account private key
4. **Cast Tool**: Ensure Foundry's `cast` command line tool is installed

```bash
# Install Foundry (if not already installed)
curl -L https://foundry.paradigm.xyz | bash
foundryup
```

---

## 🛠️ Deployment Scripts

### `deploy_tokenmanager.sh`

Complete Token Manager system deployment script, including implementation contract and proxy contract.

#### Basic Usage

```bash
cd scripts
./deploy_tokenmanager.sh
```

#### Environment Variable Configuration

```bash
# Basic configuration
export PRIVATE_KEY="0x9935c242a0b0ee41edcbd2d963f5bc7f142fdc803eb24f0df396a6fdb16c6af9"
export RPC_URL="http://localhost:8123"
export GAS_PRICE="1000000000"
export GAS_LIMIT="5000000"

# Permission separation configuration
export PROXY_ADMIN="0xDE282DC882bbB5100b8A24E30D38a2D5B3080c15"   # Proxy admin
export OWNER_ADDRESS="0xDE282DC882bbB5100b8A24E30D38a2D5B3080c15"  # Contract Owner
export ADMIN_ADDRESS="0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534"  # Business Admin

# Activation configuration
export ACTIVATION_BLOCK="0"  # Activate immediately

# Run deployment
./deploy_tokenmanager.sh
```

#### Deployment Process

1. **Compile Contracts** - Compile TokenManagerV1 and TokenManagerProxy
2. **Deploy Implementation Contract** - Deploy TokenManagerV1 implementation
3. **Deploy Proxy Contract** - Deploy TransparentUpgradeableProxy
4. **Initialize Contract** - Set Owner, Admin, and activation block
5. **Verify Deployment** - Check contract status and permissions

#### Output Information

```
🎉 Token Manager System Deployed Successfully!
======================================

📊 Deployment Summary:
  Implementation: 0x1234...5678
  Proxy Address:  0x1FdC273F90e3Eba11D2b20561F233B11424Fcfab
  Owner:         0xDE282DC882bbB5100b8A24E30D38a2D5B3080c15
  Admin:         0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534
  Status:        Active (Block: 0)
```

---

## 🧪 Testing Scripts

### `test_tokenmanager.sh`

Comprehensive Token Manager functionality testing script, covering all interfaces and boundary conditions.

#### Basic Usage

```bash
cd scripts
./test_tokenmanager.sh
```

#### Test Configuration

The script uses hardcoded configuration for testing:

```bash
# Network configuration
RPC_URL="http://localhost:8123"
PROXY_ADDRESS="0x1FdC273F90e3Eba11D2b20561F233B11424Fcfab"  # Proxy contract address
TARGET_ADDRESS="0x4B24266C13AFEf2bb60e2C69A4C08A482d81e3CA"  # Cleanup target address

# Test account private keys
OWNER_PRIVATE_KEY="0x9935c242a0b0ee41edcbd2d963f5bc7f142fdc803eb24f0df396a6fdb16c6af9"
ADMIN_PRIVATE_KEY="0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2"
OPERATOR_PRIVATE_KEY="0x3c9229289a6125f7fdf1885a77bb12c37a8d3b4962d936f7e3084dece32a3ca1"

# Test amounts
BRIDGE_AMOUNT="1000000000000000000"    # 1 ETH
TRANSFER_AMOUNT="100000000000000000"  # 0.1 ETH (for test account initialization)
```

#### Test Coverage

| Test Category | Specific Tests |
|----------|----------|
| **Operator Management** | setOperator (supports zero address removal) |
| **Admin Management** | setAdmin |
| **Role Queries** | operator(), admin() (auto-generated getters) |
| **Core Functions** | bridgeFrom (cross-chain to operator), cleanup (clean target address) |
| **Permission Control** | Unauthorized operations correctly rejected, permission transfer verification |
| **Pause Control** | pause, unpause, operations rejected in paused state |
| **Owner Management** | transferOwnership, renounceOwnership (disabled) |
| **Boundary Tests** | Repeated cleanup operations, balance change verification, zero address removal |
| **System Queries** | VERSION(), isActive(), paused(), activationBlock() |

#### Test Process

1. **Operator Management Test** - Set, replace, remove (zero address) operator
2. **BridgeFrom Operation Test** - Test token cross-chain to operator account
3. **Cleanup Operation Test** - Test target address cleanup functionality, verify 1 wei retention mechanism
4. **Pause/Resume Functionality Test** - Test pause/unpause mechanism
5. **Query Functionality Test** - Test all auto-generated getter interfaces
6. **Admin Transfer Test** - Test admin permission transfer
7. **Owner Transfer Test** - Test owner permission transfer

#### Core Interface Changes

**Removed Redundant Interfaces**:
- ❌ `getAdmin()` → ✅ Use `admin()` (auto-generated)
- ❌ `getCurrentOperator()` → ✅ Use `operator()` (auto-generated)
- ❌ `removeOperator()` → ✅ Use `setOperator(address(0))`
- ❌ `hasAdmin()` / `hasOperator()` → ✅ Directly check if address is zero

**Retained Core Interfaces**:
- ✅ `setAdmin(address)` - Set new admin
- ✅ `setOperator(address)` - Set operator (supports zero address removal)
- ✅ `bridgeFrom(uint256)` - Cross-chain tokens to operator
- ✅ `cleanup()` - Clean target address to 1 wei
- ✅ `pause()` / `unpause()` - System control
- ✅ `transferOwnership(address)` - Ownership transfer

#### Successful Output Example

```
🎉 All tests completed!
  ✅ Operator management normal
  ✅ BridgeFrom operations normal
  ✅ Cleanup operations normal
  ✅ Pause/Resume functionality normal
  ✅ Query functionality normal
  ✅ Admin transfer functionality normal
  ✅ Owner transfer functionality normal
```

---

## 🏗️ Contract Architecture

### Address Control Mode

TokenManager V1 adopts a simplified address control mode, replacing complex role control:

```solidity
// State variables (auto-generated getters)
address public admin;     // admin() - Business administrator
address public operator;  // operator() - Operator

// Management functions
function setAdmin(address newAdmin) external onlyAdmin;
function setOperator(address newOperator) external onlyAdmin;  // Supports address(0) removal
```

### Permission Hierarchy

```
Owner (System-level permissions)
├── setActivationBlock()    - Set activation block
├── pause() / unpause()     - Pause/Resume system
└── transferOwnership()     - Transfer ownership

Admin (Business-level permissions)
├── setAdmin()             - Set new admin
└── setOperator()          - Set/Remove operator

Operator (Operation-level permissions)
├── bridgeFrom()           - Cross-chain tokens
└── cleanup()              - Clean target address
```

### Security Features

1. **Anti-Frontrunning Protection**: Constructor calls `_disableInitializers()`
2. **Reentrancy Protection**: All state-changing functions use `nonReentrant`
3. **Pause Mechanism**: Supports emergency pause of all business operations
4. **Permission Separation**: Owner/Admin/Operator three-level permissions independent
5. **Single Address Control**: Each role can physically only have one address

---

## 🚨 System Limitations

When using the scripts, please note the following system limitations:

| Limitation | Value | Description |
|--------|------|------|
| **Single Address Design** | Each role can only have 1 address | Admin/Operator adopts single address mode, physically cannot set multiple |
| **Cleanup Balance Protection** | Must retain ≥1 wei | Prevents target address balance from being completely cleared |
| **Permission Design** | Owner/Admin completely independent | Two permission systems separated, but allow same address to hold both roles |
| **Interface Simplification** | Removed redundant functions | Use Solidity standard getters to replace custom query functions |

---

## 🛡️ Security Considerations

1. **Private Key Protection**: 
   - Do not expose private keys in environment variables or command line in production environments
   - Recommend using hardware wallets or secure key management systems

2. **Network Configuration**:
   - Ensure RPC_URL points to the correct network
   - Verify contract addresses are correct

3. **Permission Separation**:
   - Owner controls system-level operations (pause/unpause/transferOwnership)
   - Admin controls business-level operations (setAdmin/setOperator)
   - Operator executes business operations (bridgeFrom/cleanup)
   - Ensure permission allocation follows security principles

4. **Test Environment**:
   - Verify on testnet before running tests in production environment
   - Test scripts automatically provide funds for test accounts and execute actual transactions
   - Basic state verification moved to deployment script, test script focuses on functionality testing

5. **Interface Calls**:
   - Use `admin()` and `operator()` to replace removed getter functions
   - Use `setOperator(address(0))` to remove operator
   - Note that Owner and Admin permissions are completely independent

---

## 📝 Troubleshooting

### Common Issues

1. **"Connection refused"**
   ```bash
   # Check if Erigon node is running
   curl -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        http://localhost:8123
   ```

2. **"insufficient funds"**
   ```bash
   # Check deployment account balance
   cast balance $DEPLOYER_ADDRESS --rpc-url $RPC_URL
   ```

3. **"execution reverted"**
   ```bash
   # Check if contract is properly deployed
   cast code $PROXY_ADDRESS --rpc-url $RPC_URL
   ```

4. **"nonce too low/high"**
   ```bash
   # Reset account nonce (if needed)
   cast nonce $ACCOUNT_ADDRESS --rpc-url $RPC_URL
   ```

5. **"function not found"**
   ```bash
   # Check if removed functions are being used
   # Use admin() instead of getAdmin()
   # Use operator() instead of getCurrentOperator()
   # Use setOperator(address(0)) instead of removeOperator()
   ```

### Debug Mode

Add debug options at the beginning of scripts:

```bash
# Enable verbose output
set -x

# Stop on errors
set -e
```

---

## 📚 Related Documentation

- [Contract Source Code](../contracts/TokenManagerV1.sol)

---

## 💬 Support

If you encounter issues, please check:
1. Erigon node logs
2. Script output error messages
3. Troubleshooting section in related documentation
4. Ensure correct interface names are used (admin/operator getters)