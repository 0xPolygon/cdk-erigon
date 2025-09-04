package operations

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holiman/uint256"
	ethereum "github.com/ledgerwatch/erigon"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/accounts/abi"
	"github.com/ledgerwatch/erigon/accounts/abi/bind"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/crypto"
	"github.com/ledgerwatch/erigon/ethclient"
	"github.com/ledgerwatch/erigon/turbo/jsonrpc/constants"
)

const (
	TmpSenderPrivateKey = "363ea277eec54278af051fb574931aec751258450a286edce9e1f64401f3b9c8"
)

// Global variables to store deployed contract addresses
var (
	ContractAAddr     libcommon.Address
	ContractBAddr     libcommon.Address
	FactoryAddr       libcommon.Address
	DeploymentAddress libcommon.Address
	ContractsDeployed bool
)

// TransTokenWithFrom transfers tokens from a specific private key to an address
func TransTokenWithFrom(t *testing.T, ctx context.Context, client *ethclient.Client, fromPrivateKey string, amount *uint256.Int, toAddress string) string {
	chainID, err := client.ChainID(ctx)
	require.NoError(t, err)
	auth, err := GetAuth(fromPrivateKey, chainID.Uint64())
	require.NoError(t, err)
	nonce, err := client.PendingNonceAt(ctx, auth.From)
	require.NoError(t, err)
	gasPrice, err := client.SuggestGasPrice(ctx)
	require.NoError(t, err)

	to := libcommon.HexToAddress(toAddress)
	gas, err := client.EstimateGas(ctx, ethereum.CallMsg{From: auth.From, To: &to, Value: amount})
	require.NoError(t, err)

	tx := &types.LegacyTx{
		CommonTx: types.CommonTx{Nonce: nonce, To: &to, Gas: gas, Value: amount},
		GasPrice: uint256.MustFromBig(gasPrice),
	}

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(fromPrivateKey, "0x"))
	require.NoError(t, err)

	signer := types.MakeSigner(GetTestChainConfig(DefaultL2ChainID), 1, 0)
	signedTx, err := types.SignTx(tx, *signer, privateKey)
	require.NoError(t, err)

	err = client.SendTransaction(ctx, signedTx)
	require.NoError(t, err)

	err = WaitTxToBeMined(ctx, client, signedTx, DefaultTimeoutTxToBeMined)
	require.NoError(t, err)

	return signedTx.Hash().String()
}

// TransToken transfers tokens using the default admin private key
func TransToken(t *testing.T, ctx context.Context, client *ethclient.Client, amount *uint256.Int, toAddress string) string {
	return TransTokenWithFrom(t, ctx, client, DefaultL2AdminPrivateKey, amount, toAddress)
}

// DeployContract deploys a contract using the provided parameters
func DeployContract(t *testing.T, ctx context.Context, client *ethclient.Client, privateKey *ecdsa.PrivateKey, contractName, abiJson, bytecodeStr string, constructorArgs ...interface{}) libcommon.Address {
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	require.NoError(t, err)
	gasPrice, err := client.SuggestGasPrice(ctx)
	require.NoError(t, err)

	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, GetTestChainConfig(DefaultL2ChainID).ChainID)
	require.NoError(t, err)
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0)
	auth.GasLimit = uint64(3000000)
	auth.GasPrice = gasPrice

	contractABI, err := abi.JSON(strings.NewReader(abiJson))
	require.NoError(t, err)
	contractBytecode, err := hex.DecodeString(bytecodeStr)
	require.NoError(t, err)

	contractAddr, tx, _, err := bind.DeployContract(auth, contractABI, contractBytecode, client, constructorArgs...)
	require.NoError(t, err)

	bind.WaitDeployed(ctx, client, tx)
	return contractAddr
}

// EnsureContractsDeployed ensures that test contracts are deployed for e2e tests
func EnsureContractsDeployed(t *testing.T) {
	if ContractsDeployed {
		return
	}

	ctx := context.Background()
	client, err := ethclient.Dial(DefaultL2NetworkURL)
	require.NoError(t, err)

	privateKey, err := crypto.HexToECDSA(TmpSenderPrivateKey)
	require.NoError(t, err)
	DeploymentAddress = crypto.PubkeyToAddress(privateKey.PublicKey)

	// Fund deployment address
	fundingAmount := uint256.NewInt(5000000000000000000) // 5 ETH
	TransToken(t, ctx, client, fundingAmount, DeploymentAddress.String())

	// Deploy contracts
	ContractBAddr = DeployContract(t, ctx, client, privateKey, "ContractB", constants.ContractBABIJson, constants.ContractBBytecodeStr)
	ContractAAddr = DeployContract(t, ctx, client, privateKey, "ContractA", constants.ContractAABIJson, constants.ContractABytecodeStr, ContractBAddr)
	FactoryAddr = DeployContract(t, ctx, client, privateKey, "ContractFactory", constants.ContractFactoryABIJson, constants.ContractFactoryBytecodeStr)

	ContractsDeployed = true
}

// EncodeTransferCall encodes an ERC20 transfer function call
func EncodeTransferCall(to string, amount uint64) []byte {
	// ERC20 transfer function selector: 0xa9059cbb
	selector := []byte{0xa9, 0x05, 0x9c, 0xbb}

	// Convert address string to bytes
	toAddr := libcommon.HexToAddress(to)

	// Create the data payload
	data := make([]byte, 68) // 4 bytes selector + 32 bytes address + 32 bytes amount
	copy(data[:4], selector)
	copy(data[4+12:36], toAddr.Bytes()) // Address goes in the last 20 bytes of the 32-byte slot

	// Amount as big.Int
	amountBig := big.NewInt(int64(amount))
	amountBig.FillBytes(data[36:68])

	return data
}

// EncodeComplexCall encodes a complex contract call
func EncodeComplexCall(target libcommon.Address) []byte {
	// Function selector for complex call
	selector := []byte{0xe6, 0x09, 0x05, 0x5e}

	// Create the data payload
	data := make([]byte, 36) // 4 bytes selector + 32 bytes address
	copy(data[:4], selector)
	copy(data[4+12:36], target.Bytes()) // Address goes in the last 20 bytes

	return data
}
