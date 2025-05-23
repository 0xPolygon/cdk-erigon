package helpers

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	ethereum "github.com/erigontech/erigon"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/crypto"
	"github.com/erigontech/erigon/accounts/abi"
	"github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/ethclient"
	"github.com/erigontech/erigon/rpc"
	"github.com/holiman/uint256"
	"os/exec"
	"strings"
	"time"
)

func CompileContract(filename string) ([]byte, error) {
	cmd := exec.Command("solc", "--optimize", "--evm-version", "london", "--bin", filename)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	var bytecode string
	for i, line := range lines {
		if strings.Contains(line, "Binary:") && i+1 < len(lines) {
			bytecode = lines[i+1]
			break
		}
	}

	if bytecode == "" {
		return nil, fmt.Errorf("bytecode not found in solc output")
	}

	bytecodeBytes, err := hex.DecodeString(bytecode)
	if err != nil {
		return nil, err
	}

	return bytecodeBytes, nil
}

func CompileContractInitCode(filename, contractName string, constructorArgs ...interface{}) ([]byte, error) {
	cmd := exec.Command("solc", "--optimize", "--evm-version", "london", "--combined-json", "bin,abi", filename)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("solc compile error: %w", err)
	}

	var meta struct {
		Contracts map[string]struct {
			Bin string          `json:"bin"`
			ABI json.RawMessage `json:"abi"`
		} `json:"contracts"`
	}

	if err = json.Unmarshal(out, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal solc JSON: %w", err)
	}

	key := fmt.Sprintf("%s:%s", filename, contractName)
	info, ok := meta.Contracts[key]
	if !ok {
		return nil, fmt.Errorf("contract %q not found in solc output", key)
	}

	initCode, err := hex.DecodeString(info.Bin)
	if err != nil {
		return nil, fmt.Errorf("decoding init code: %w", err)
	}

	if len(constructorArgs) > 0 {
		parsedABI, err := abi.JSON(bytes.NewReader(info.ABI))
		if err != nil {
			return nil, fmt.Errorf("parsing ABI: %w", err)
		}
		ctorPayload, err := parsedABI.Pack("", constructorArgs...)
		if err != nil {
			return nil, fmt.Errorf("ABI‐packing constructor args: %w", err)
		}
		initCode = append(initCode, ctorPayload...)
	}

	return initCode, nil
}

func DeployContract(ctx context.Context, client *ethclient.Client, privateKey *ecdsa.PrivateKey, bytecode []byte) (*types.Receipt, error) {
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce: %v", err)
	}

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %v", err)
	}

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("error fetching gas price: %w", err)
	}

	msg := ethereum.CallMsg{
		From:     fromAddress,
		To:       nil,
		Value:    uint256.NewInt(0),
		Data:     bytecode,
		GasPrice: uint256.NewInt(gasPrice.Uint64()),
	}

	gasLimit, err := client.EstimateGas(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("error estimating gas: %w", err)
	}

	tx := &types.LegacyTx{
		CommonTx: types.CommonTx{
			To:    nil,
			Nonce: nonce,
			Value: uint256.NewInt(0),
			Gas:   gasLimit,
			Data:  bytecode,
		},
		GasPrice: uint256.NewInt(gasPrice.Uint64()),
	}

	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, *signer, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %v", err)
	}

	if err = client.SendTransaction(ctx, signedTx); err != nil {
		return nil, fmt.Errorf("failed to send transaction: %v", err)
	}

	receipt, err := WaitForReceipt(client, signedTx.Hash())
	if err != nil {
		return nil, err
	}

	if receipt.Status == 0 {
		return nil, fmt.Errorf("contract deployment failed")
	}

	return receipt, nil
}

func WaitForReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err != nil {
			if errors.Is(err, ethereum.NotFound) || errors.Is(err, rpc.ErrNoResult) {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("error fetching transaction receipt: %w", err)
		}

		return receipt, nil
	}
}

func FundContract(ctx context.Context, client *ethclient.Client, privateKey *ecdsa.PrivateKey, contractAddress common.Address, amount *uint256.Int) (*types.Receipt, error) {
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	balance, err := client.BalanceAt(ctx, fromAddress, nil)
	if err != nil {
		return nil, fmt.Errorf("error fetching balance: %w", err)
	}

	if balance.Cmp(amount) < 0 {
		return nil, fmt.Errorf("insufficient balance: %s", balance.String())
	}

	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return nil, fmt.Errorf("error fetching nonce: %w", err)
	}

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("error fetching gas price: %w", err)
	}

	msg := ethereum.CallMsg{
		From:  fromAddress,
		To:    &contractAddress,
		Value: amount,
	}

	gasNeeded, err := client.EstimateGas(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("error estimating gas: %w", err)
	}

	tx := types.NewTransaction(nonce, contractAddress, amount, gasNeeded, uint256.NewInt(gasPrice.Uint64()), nil)

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("error fetching chain ID: %w", err)
	}

	signer := types.LatestSignerForChainID(chainID)

	signedTx, err := types.SignTx(tx, *signer, privateKey)
	if err != nil {
		return nil, fmt.Errorf("error signing transaction: %w", err)
	}

	if err = client.SendTransaction(ctx, signedTx); err != nil {
		return nil, fmt.Errorf("error sending transaction: %w", err)
	}

	r, err := WaitForReceipt(client, signedTx.Hash())
	if err != nil {
		return nil, fmt.Errorf("error waiting for transaction receipt: %w", err)
	}

	if r.Status == 0 {
		return nil, fmt.Errorf("transaction failed")
	}

	return r, nil
}
