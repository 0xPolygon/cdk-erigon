//go:build !skip_smoke_realtime
// +build !skip_smoke_realtime

package test

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/mdbx"
	"github.com/ledgerwatch/erigon/accounts/abi"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/core/types/accounts"
	"github.com/ledgerwatch/erigon/crypto"
	"github.com/ledgerwatch/erigon/ethclient"
	"github.com/ledgerwatch/erigon/test/operations"
	"github.com/ledgerwatch/erigon/zk/realtime/rtclient"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
	"github.com/ledgerwatch/erigon/zkevm/encoding"
	"github.com/ledgerwatch/log/v3"
	"github.com/stretchr/testify/require"
)

func TestRealtimeRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// Preapre to deploy a ERC20 contract
	ctx := context.Background()
	ec, err := ethclient.Dial(DefaultL2NetworkRealtimeURL)
	require.NoError(t, err)
	client := rtclient.NewRealtimeClient(ec, DefaultL2NetworkRealtimeURL)
	blockNumber := setupRealtimeTestEnvironment(t, client)

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(DefaultL2AdminPrivateKey, "0x"))
	require.NoError(t, err)
	fromAddress := common.HexToAddress(DefaultL2AdminAddress)
	log.Info(fmt.Sprintf("Sender: %s", fromAddress))

	// Default test address for tests that require an address
	testAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Used to check whether the result returned by the interface call is correct
	time.Sleep(1 * time.Second)
	originNonce, err := client.RealtimeGetTransactionCount(fromAddress)
	require.NoError(t, err)
	originBalance, err := client.RealtimeGetBalance(testAddress)
	require.NoError(t, err)

	// Transfer native token
	txHash := transToken(t, context.Background(), client, uint256.NewInt(encoding.Gwei), testAddress.String())

	// Deploy the contract
	erc20Address := deployERC20Contract(t, ctx, privateKey, client)

	t.Run("RealtimeBlockNumber", func(t *testing.T) {
		blockNumber, err := client.RealtimeBlockNumber()
		require.NoError(t, err)
		log.Info(fmt.Sprintf("RealtimeBlockNumber result: %d", blockNumber))
	})

	t.Run("RealtimePendingBlockNumber", func(t *testing.T) {
		blockNumber, err := client.RealtimePendingBlockNumber()
		require.NoError(t, err)
		log.Info(fmt.Sprintf("RealtimePendingBlockNumber result: %d", blockNumber))
	})

	t.Run("RealtimeGetBlockTransactionCountByNumber", func(t *testing.T) {
		transactionCount, err := client.RealtimeGetBlockTransactionCountByNumber(blockNumber)
		require.NoError(t, err)
		log.Info(fmt.Sprintf("RealtimeGetBlockTransactionCountByNumber result: %d", transactionCount))
	})

	t.Run("RealtimeGetLatestBlockTransactionCount", func(t *testing.T) {
		transactionCount, err := client.RealtimeGetLatestBlockTransactionCount()
		require.NoError(t, err)
		log.Info(fmt.Sprintf("RealtimeGetLatestBlockTransactionCount result: %d", transactionCount))
	})

	t.Run("RealtimeGetPendingBlockTransactionCount", func(t *testing.T) {
		transactionCount, err := client.RealtimeGetPendingBlockTransactionCount()
		require.NoError(t, err)
		log.Info(fmt.Sprintf("RealtimeGetPendingBlockTransactionCount result: %d", transactionCount))
	})

	t.Run("RealtimeGetTransactionByHash", func(t *testing.T) {
		includeExtraInfo := true
		result, err := client.RealtimeGetTransactionByHash(common.HexToHash(txHash), &includeExtraInfo)
		require.NoError(t, err)

		receipt, err := client.RealtimeGetTransactionReceipt(common.HexToHash(txHash))
		require.NoError(t, err)
		require.NotNil(t, receipt, "GetTransactionReceipt should return receipt")

		txHashIndex := int(*result.TransactionIndex)
		receiptIndex := int(receipt.TransactionIndex)
		require.Equal(t, receiptIndex, txHashIndex)
		log.Info(fmt.Sprintf("RealtimeGetTransactionByHash result type: %T", result))
	})

	t.Run("RealtimeGetRawTransactionByHash", func(t *testing.T) {
		result, err := client.RealtimeGetRawTransactionByHash(common.HexToHash(txHash))
		require.NoError(t, err)
		log.Info(fmt.Sprintf("RealtimeGetRawTransactionByHash result type: %T", result))
	})

	t.Run("RealtimeGetTransactionReceipt", func(t *testing.T) {
		receipt, err := client.RealtimeGetTransactionReceipt(common.HexToHash(txHash))
		require.NoError(t, err)
		require.NotNil(t, receipt)
		log.Info(fmt.Sprintf("RealtimeGetTransactionReceipt result type: %T", receipt))
	})

	t.Run("RealtimeGetInternalTransactions", func(t *testing.T) {
		tx, err := client.RealtimeGetInternalTransactions(common.HexToHash(txHash))
		require.NoError(t, err)
		log.Info(fmt.Sprintf("RealtimeGetInternalTransactions result type: %T", tx))
	})

	t.Run("RealtimeGetBalance", func(t *testing.T) {
		balance, err := client.RealtimeGetBalance(testAddress)
		require.NoError(t, err)
		require.Equal(t, originBalance.Add(originBalance, big.NewInt(encoding.Gwei)).String(), balance.String(), "Balance should increase by 1 Gwei")
		log.Info(fmt.Sprintf("RealtimeGetBalance result for test address: %s", balance.String()))
	})

	t.Run("RealtimeGetTransactionCount", func(t *testing.T) {
		nonce, err := client.RealtimeGetTransactionCount(fromAddress)
		require.NoError(t, err)
		require.Equal(t, originNonce+2, nonce)
		log.Info(fmt.Sprintf("RealtimeGetTransactionCount result for sender address: %d", nonce))
	})

	t.Run("RealtimeGetCode", func(t *testing.T) {
		code, err := client.RealtimeGetCode(erc20Address)
		require.NoError(t, err)
		require.NotEmpty(t, code, "Contract code should not be empty")
		log.Info(fmt.Sprintf("RealtimeGetCode result for erc20 contract %s: %s", erc20Address, code))
	})

	t.Run("RealtimeGetStorageAt", func(t *testing.T) {
		// 0x2 is refered to _totalSupply field
		value, err := client.RealtimeGetStorageAt(erc20Address, "0x2", "pending")
		require.NoError(t, err)
		require.Equal(t, "0x00000000000000000000000000000000000000000052b7d2dcc80cd2e4000000", value, "Storage at index 0x2 should be equal to 1000000000000000000000")
		log.Info(fmt.Sprintf("RealtimeGetStorageAt result for erc20 contract %s at index %s: %s", erc20Address, "0x2", value))
	})

	t.Run("RealtimeCall", func(t *testing.T) {
		data, err := erc20ABI.Pack("balanceOf", fromAddress)
		require.NoError(t, err)
		value, err := client.RealtimeCall(testAddress, erc20Address, "0x100000", "0x1", "0x0", fmt.Sprintf("0x%x", data))
		require.NoError(t, err)
		require.Equal(t, "0x00000000000000000000000000000000000000000052b7d2dcc80cd2e4000000", value, fmt.Sprintf("Balance of %s should be equal to 1000000000000000000000", fromAddress))
		log.Info(fmt.Sprintf("RealtimeCall result for erc20 contract %s calling method balanceOf %s: %s", erc20Address, fromAddress, value))
	})

	t.Run("RealtimeEstimateGas", func(t *testing.T) {
		// Test standard eth_estimateGas with a simple transfer
		transferArgs := map[string]interface{}{
			"from":  fromAddress,
			"to":    testAddress,
			"value": (*hexutil.Big)(big.NewInt(1)),
		}

		gasEstimate, err := client.RealtimeEstimateGas(transferArgs)
		require.NoError(t, err)
		require.Equal(t, gasEstimate, uint64(21_000), "Mative transfer txs gas should be 21_000")

		// Test gas estimation for a contract call (ERC20 transfer)
		transferData, err := erc20ABI.Pack("transfer", erc20Address, big.NewInt(1))
		require.NoError(t, err)

		contractCallArgs := map[string]interface{}{
			"from": fromAddress,
			"to":   erc20Address,
			"data": (*hexutil.Bytes)(&transferData),
		}

		gasEstimateCall, err := client.RealtimeEstimateGas(contractCallArgs)
		require.NoError(t, err)
		require.Greater(t, gasEstimateCall, gasEstimate, "Contract call should require more gas than simple transfer")
	})

	// Test call for block height specific
	t.Run("RealtimeCallWithHeight", func(t *testing.T) {
		data, err := erc20ABI.Pack("balanceOf", fromAddress)
		require.NoError(t, err)

		startValue, err := client.RealtimeCall(testAddress, erc20Address, "0x100000", "0x1", "0x0", fmt.Sprintf("0x%x", data))
		require.NoError(t, err)
		require.Equal(t, "0x00000000000000000000000000000000000000000052b7d2dcc80cd2e4000000", startValue, fmt.Sprintf("Balance of %s should be equal to 1e+26", fromAddress))

		// Send balance transfer
		transferAmount := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18)) // Adjust for token decimals (18 in this case)
		nonce, err := client.RealtimeGetTransactionCount(fromAddress)
		require.NoError(t, err)
		signedTx := erc20TransferTx(t, ctx, privateKey, client, transferAmount, testAddress, erc20Address, nonce)
		err = WaitTxToBeMined(ctx, client, signedTx, DefaultTimeoutTxToBeMined)
		require.NoError(t, err)

		// Get tx block number
		receipt, err := client.RealtimeGetTransactionReceipt(signedTx.Hash())
		require.NoError(t, err)
		require.NotNil(t, receipt)
		targetBlockNumber := receipt.BlockNumber.Uint64()

		correctValue, err := client.RealtimeCall(testAddress, erc20Address, "0x100000", "0x1", "0x0", fmt.Sprintf("0x%x", data))
		require.NoError(t, err)
		require.Equal(t, "0x00000000000000000000000000000000000000000052b7d2cee7561f3c9c0000", correctValue, fmt.Sprintf("Balance of %s should be equal to 9.9999999e+25 after transfer", fromAddress))
		require.NotEqual(t, startValue, correctValue)

		// Send balance transfer
		signedTx = erc20TransferTx(t, ctx, privateKey, client, transferAmount, testAddress, erc20Address, nonce+1)
		err = WaitTxToBeMined(ctx, client, signedTx, DefaultTimeoutTxToBeMined)
		require.NoError(t, err)

		endValue, err := client.RealtimeCall(testAddress, erc20Address, "0x100000", "0x1", "0x0", fmt.Sprintf("0x%x", data))
		require.NoError(t, err)
		require.NotEqual(t, endValue, correctValue)
		require.Equal(t, "0x00000000000000000000000000000000000000000052b7d2c1069f6b95380000", endValue, fmt.Sprintf("Balance of %s should be equal to 9.9999998e+25 after transfer", fromAddress))

		// Get block height specific state
		testValue, err := client.EthGetTokenBalance(ctx, testAddress, erc20Address, new(big.Int).SetUint64(targetBlockNumber))
		require.NoError(t, err)
		require.NotEqual(t, testValue, correctValue)
	})

	t.Run("RealtimeGetBlockByNumber", func(t *testing.T) {
		latestBlockNumber, err := client.RealtimeBlockNumber()
		if err != nil {
			log.Error(fmt.Sprintf("RealtimeGetBlockNumber error: %v", err))
		}
		block, err := client.RealtimeGetBlockByNumber(latestBlockNumber)
		require.NoError(t, err)
		require.NotNil(t, block, "Block should not be nil")
		require.NotNil(t, block["hash"], "Block hash should not be nil")

		log.Info(fmt.Sprintf("RealtimeGetBlockByNumber result block number: %v, hash: %v, txCount: %v", block["number"], block["hash"], len(block["transactions"].([]interface{}))))
	})

	t.Run("RealtimeGetBlockByHash", func(t *testing.T) {
		latestBlockNumber, err := client.RealtimeBlockNumber()
		require.NoError(t, err)
		require.Greater(t, latestBlockNumber, uint64(0), "Latest block number should be greater than 0")

		// Get the block by number
		log.Info(fmt.Sprintf("Getting finalized block by number: %v", latestBlockNumber))
		blockByNumber, err := client.RealtimeGetBlockByNumber(latestBlockNumber)
		require.NoError(t, err)
		require.NotNil(t, blockByNumber, "Block by number should not be nil")

		// Extract the block hash
		blockHashStr, ok := blockByNumber["hash"].(string)
		require.True(t, ok, "Block hash should be a string")
		require.NotEmpty(t, blockHashStr, "Block hash should not be empty")

		// Test getting the same block by hash
		blockByHash, err := client.RealtimeGetBlockByHash(common.HexToHash(blockHashStr), true)
		require.NoError(t, err)
		require.NotNil(t, blockByHash, "Block should not be nil")
		require.NotNil(t, blockByHash["hash"], "Block hash should not be nil")

		// Verify that both methods return the same block
		require.Equal(t, blockByNumber["hash"], blockByHash["hash"], "Block hashes should match")
		require.Equal(t, blockByNumber["number"], blockByHash["number"], "Block numbers should match")

		log.Info(fmt.Sprintf("RealtimeGetBlockByHash result - finalized block number: %v, hash: %v, txCount: %v", blockByHash["number"], blockByHash["hash"], len(blockByHash["transactions"].([]interface{}))))
	})

	t.Run("RealtimeGetBlockTransactionCountByHash", func(t *testing.T) {
		numberOfTransactions := 10

		// Create the specified number of transactions and wait for them to be mined
		txHashes := transTokenBatch(t, context.Background(), client, uint256.NewInt(encoding.Gwei), testAddress.String(), numberOfTransactions)
		lastTxHash := txHashes[len(txHashes)-1]

		// Get the block information from the last transaction's receipt
		receipt, err := client.RealtimeGetTransactionReceipt(common.HexToHash(lastTxHash))
		require.NoError(t, err)
		require.NotNil(t, receipt, "Transaction receipt should not be nil")

		targetBlockNumber := receipt.BlockNumber.Uint64()
		targetBlockHash := receipt.BlockHash

		// Get the actual transaction count for this block by number
		actualTxCount, err := client.RealtimeGetBlockTransactionCountByNumber(targetBlockNumber)
		require.NoError(t, err)

		// Test getting transaction count by hash
		transactionCount, err := client.RealtimeGetBlockTransactionCountByHash(targetBlockHash)
		require.NoError(t, err)

		require.Equal(t, actualTxCount, transactionCount, fmt.Sprintf("Transaction count by hash should match count by number (%d)", actualTxCount))

		log.Info(fmt.Sprintf("RealtimeGetBlockTransactionCountByHash result: %d (verified against block content) ✓", transactionCount))

	})

	t.Run("RealtimeGetBlockInternalTransactions", func(t *testing.T) {
		txHash := transToken(t, ctx, client, uint256.NewInt(encoding.Gwei), testAddress.String())

		var targetBlockNumber uint64

		// Get the block number directly from the transaction receipt
		receipt, err := client.RealtimeGetTransactionReceipt(common.HexToHash(txHash))
		require.NoError(t, err)

		targetBlockNumber = receipt.BlockNumber.Uint64()

		// Test getting internal transactions for the block
		internalTxs, err := client.RealtimeGetBlockInternalTransactions(targetBlockNumber)
		require.NoError(t, err)
		require.NotNil(t, internalTxs, "Internal transactions map should not be nil")

		// Count total internal transactions
		totalInternalTxs := 0
		for _, innerTxs := range internalTxs {
			totalInternalTxs += len(innerTxs)
		}

		require.IsType(t, map[common.Hash][]*zktypes.InnerTx{}, internalTxs, "Should return correct type")
		log.Info(fmt.Sprintf("RealtimeGetBlockInternalTransactions successfully returned data for block %d", targetBlockNumber))
	})

	t.Run("RealtimeGetBlockReceipts", func(t *testing.T) {
		numberOfTransactions := 10

		// Create the specified number of transactions and wait for them to be mined
		txHashes := transTokenBatch(t, context.Background(), client, uint256.NewInt(encoding.Gwei), testAddress.String(), numberOfTransactions)
		lastTxHash := txHashes[len(txHashes)-1]

		// Get the block information from the last transaction's receipt
		receipt, err := client.RealtimeGetTransactionReceipt(common.HexToHash(lastTxHash))
		require.NoError(t, err)
		require.NotNil(t, receipt, "Transaction receipt should not be nil")

		receiptsByNumber, err := client.RealtimeGetBlockReceiptsByNumber(receipt.BlockNumber.Uint64())
		require.NoError(t, err)
		require.NotNil(t, receiptsByNumber, "Transaction receipts by number should not be nil")
		for _, receipt := range receiptsByNumber {
			require.NoError(t, err)
			require.NotNil(t, receipt)
			log.Info(fmt.Sprintf("RealtimeGetBlockReceiptsByNumber result type: %T", receipt))
		}

		receiptsByHash, err := client.RealtimeGetBlockReceiptsByHash(receipt.BlockHash)
		require.NoError(t, err)
		require.NotNil(t, receiptsByHash, "Transaction receipts by hash should not be nil")
		for _, receipt := range receiptsByHash {
			require.NoError(t, err)
			require.NotNil(t, receipt)
			log.Info(fmt.Sprintf("RealtimeGetBlockReceiptsByHash result type: %T", receipt))
		}
	})

	t.Run("RealtimeEnabled", func(t *testing.T) {
		// Test with valid "pending" tag
		isEnabled, err := client.RealtimeEnabled()
		require.NoError(t, err)
		require.IsType(t, bool(false), isEnabled, "RealtimeEnabled should return bool")

		if isEnabled {
			log.Info("RealtimeEnabled: Realtime feature is enabled and cache is ready")
		} else {
			log.Info("RealtimeEnabled: Realtime feature is disabled or cache is not ready")
		}
	})

	t.Run("RealtimeLatest", func(t *testing.T) {
		latestBlockNum, err := client.RealtimeBlockNumber()
		require.NoError(t, err)
		require.Greater(t, latestBlockNum, uint64(0), "Latest block number should be greater than 0")

		// Change chain-state
		fromAddress := common.HexToAddress(DefaultL2AdminAddress)
		testAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")
		transferAmount := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18))
		nonce, err := client.RealtimeGetTransactionCount(fromAddress)
		require.NoError(t, err)
		signedTx := erc20TransferTx(t, ctx, privateKey, client, transferAmount, testAddress, erc20Address, nonce)
		err = WaitTxToBeMined(ctx, client, signedTx, DefaultTimeoutTxToBeMined)
		require.NoError(t, err)

		// Test state APIs
		balance, err := client.RealtimeGetBalance(fromAddress)
		require.NoError(t, err)
		require.Greater(t, balance.Uint64(), uint64(0), "Balance should be greater than 0")

		tokenBalance, err := client.RealtimeGetTokenBalance(fromAddress, testAddress, erc20Address)
		require.NoError(t, err)
		require.Greater(t, tokenBalance.Uint64(), uint64(0), "Token balance should be greater than 0")

		// Test stateless APIs
		block, err := client.RealtimeGetBlockByNumber(latestBlockNum)
		require.NoError(t, err)
		require.NotNil(t, block, "Block should not be nil")
		require.NotNil(t, block["hash"], "Block hash should not be nil")

		blockByHash, err := client.RealtimeGetBlockByHash(common.HexToHash(block["hash"].(string)), true)
		require.NoError(t, err)
		require.NotNil(t, blockByHash, "Block should not be nil")
		require.NotNil(t, blockByHash["hash"], "Block hash should not be nil")

		require.Equal(t, block["hash"], blockByHash["hash"], "Block hashes should match")
		require.Equal(t, block["number"], blockByHash["number"], "Block numbers should match")
	})
}

func TestRealtimeStateIsConsistent(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	ctx := context.Background()
	ec, err := ethclient.Dial(DefaultL2NetworkRealtimeURL)
	require.NoError(t, err)
	client := rtclient.NewRealtimeClient(ec, DefaultL2NetworkRealtimeURL)

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(DefaultL2AdminPrivateKey, "0x"))
	require.NoError(t, err)
	signer := types.MakeSigner(operations.GetTestChainConfig(DefaultL2ChainID), 1, 0)

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	require.True(t, ok)
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	log.Info(fmt.Sprintf("Sender: %s", fromAddress))

	erc20ABI, err := abi.JSON(strings.NewReader(erc20ABIJson))
	require.NoError(t, err)

	// Deploy the contract
	erc20Address := deployERC20Contract(t, ctx, privateKey, client)

	// Get the sender's nonce
	nonce, err := client.RealtimeGetTransactionCount(fromAddress)
	require.NoError(t, err)

	for i := int64(0); i < 10; i++ {
		// Transfer erc20 tokens amount
		amount := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18)) // Adjust for token decimals (18 in this case)
		// Prepare transfer data
		recevier := common.HexToAddress(fmt.Sprintf("0x000000000000000000000000000000000010%04x", i))
		data, err := erc20ABI.Pack("transfer", recevier, amount)
		require.NoError(t, err)

		gasPrice, err := client.SuggestGasPrice(ctx)
		require.NoError(t, err)
		transferERC20TokenTx := &types.LegacyTx{
			CommonTx: types.CommonTx{
				Nonce: nonce + uint64(i),
				To:    &erc20Address,
				Gas:   60000,
				Value: uint256.NewInt(0),
				Data:  data,
			},
			GasPrice: uint256.MustFromBig(gasPrice),
		}

		signedTx, err := types.SignTx(transferERC20TokenTx, *signer, privateKey)
		require.NoError(t, err)
		err = client.SendTransaction(ctx, signedTx)
		require.NoError(t, err)
		err = WaitTxToBeMined(ctx, client, signedTx, DefaultTimeoutTxToBeMined)
		require.NoError(t, err)
		receipt, err := client.RealtimeGetTransactionReceipt(signedTx.Hash())
		require.NoError(t, err)
		require.NotNil(t, receipt)
		log.Info(fmt.Sprintf("receipt: %+v", receipt))
	}

	// Dump state cache for further checking
	err = client.RealtimeDumpCache()
	require.NoError(t, err)

	compareCacheWithSequenceDB(t, DefaultSequncerDBPath, DefaultStateCachePath)
}

func compareCacheWithSequenceDB(t *testing.T, dbDir, cacheDir string) {
	// Cache Files list
	cacheFiles := map[string]string{
		"account_cache.json":     "",
		"storage_cache.json":     "",
		"code_cache.json":        "",
		"incarnation_cache.json": "",
	}

	for fileName := range cacheFiles {
		filePath := filepath.Join(cacheDir, fileName)
		_, err := os.Stat(filePath)
		require.NoError(t, err)
		data, err := ioutil.ReadFile(filePath)
		require.NoError(t, err)

		cacheFiles[fileName] = string(data)
	}

	tempDbDir, err := ioutil.TempDir("", "mdbx_copy")
	require.NoError(t, err, "Failed to create temp db dir")
	defer os.RemoveAll(tempDbDir)

	cmd := exec.Command("cp", "-r", dbDir, tempDbDir)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to copy db dir with cp -r: %s, output: %s", dbDir, string(output))
	copiedDbDir := filepath.Join(tempDbDir, filepath.Base(dbDir))

	ctx := context.Background()
	db, err := mdbx.NewMDBX(log.New()).Path(copiedDbDir).Open(ctx)
	require.NoError(t, err)
	defer db.Close()

	// Compare account data
	if cacheFiles["account_cache.json"] != "" {
		var accountCache map[string]string
		err := json.Unmarshal([]byte(cacheFiles["account_cache.json"]), &accountCache)
		require.NoError(t, err)

		db.View(ctx, func(txn kv.Tx) error {
			for k, v := range accountCache {
				key, err := hex.DecodeString(k)
				require.NoError(t, err)
				value, err := txn.GetOne(kv.PlainState, key)
				require.NoError(t, err)

				vBytes, _ := hex.DecodeString(v)
				var dbAccount accounts.Account
				err = dbAccount.DecodeForStorage(value)
				require.NoError(t, err)

				var cacheAccount accounts.Account
				err = cacheAccount.DecodeForStorage(vBytes)
				require.NoError(t, err)

				require.Equal(t, cacheAccount.Initialised, dbAccount.Initialised, "Initialised mismatch for account %s, from cache: %t, from db: %t", k, cacheAccount.Initialised, dbAccount.Initialised)
				require.Equal(t, cacheAccount.Nonce, dbAccount.Nonce, "Nonce mismatch for account %s, from cache: %d, from db: %d", k, cacheAccount.Nonce, dbAccount.Nonce)
				require.Equal(t, cacheAccount.Balance, dbAccount.Balance, "Balance mismatch for account %s, from cache: %s, from db: %s", k, cacheAccount.Balance.String(), dbAccount.Balance.String())
				require.Equal(t, cacheAccount.Root, dbAccount.Root, "Root mismatch for account %s, from cache: %s, from db: %s", k, cacheAccount.Root.Hex(), dbAccount.Root.Hex())
				require.Equal(t, cacheAccount.CodeHash, dbAccount.CodeHash, "CodeHash mismatch for account %s, from cache: %s, from db: %s", k, cacheAccount.CodeHash.Hex(), dbAccount.CodeHash.Hex())
				require.Equal(t, cacheAccount.Incarnation, dbAccount.Incarnation, "Incarnation mismatch for account %s, from cache: %d, from db: %d", k, cacheAccount.Incarnation, dbAccount.Incarnation)
				require.Equal(t, cacheAccount.PrevIncarnation, dbAccount.PrevIncarnation, "PrevIncarnation mismatch for account %s, from cache: %d, from db: %d", k, cacheAccount.PrevIncarnation, dbAccount.PrevIncarnation)
			}
			return nil
		})
	}

	// Compare storage data
	if cacheFiles["storage_cache.json"] != "" {
		var storageCache map[string]string
		err := json.Unmarshal([]byte(cacheFiles["storage_cache.json"]), &storageCache)
		require.NoError(t, err)

		db.View(ctx, func(txn kv.Tx) error {
			for k, v := range storageCache {
				key, err := hex.DecodeString(k)
				require.NoError(t, err)
				value, err := txn.GetOne(kv.PlainState, key)
				require.NoError(t, err)

				require.Equal(t, v, hex.EncodeToString(value), "Storage mismatch for key %s, from cache: %s, from db: %s", k, v, hex.EncodeToString(value))
			}
			return nil
		})
	}

	// Compare code data
	if cacheFiles["code_cache.json"] != "" {
		var codeCache map[string]string
		err := json.Unmarshal([]byte(cacheFiles["code_cache.json"]), &codeCache)
		require.NoError(t, err)

		db.View(ctx, func(txn kv.Tx) error {
			for k, v := range codeCache {
				key, err := hex.DecodeString(k)
				require.NoError(t, err)
				value, err := txn.GetOne(kv.Code, key)
				require.NoError(t, err)

				require.Equal(t, v, hex.EncodeToString(value), "Code mismatch for key %s, from cache: %s, from db: %s", k, v, hex.EncodeToString(value))
			}
			return nil
		})
	}

	// Compare incarnation data
	if cacheFiles["incarnation_cache.json"] != "" {
		var incarnationCache map[string]string
		err := json.Unmarshal([]byte(cacheFiles["incarnation_cache.json"]), &incarnationCache)
		require.NoError(t, err)

		db.View(ctx, func(txn kv.Tx) error {
			for k, v := range incarnationCache {
				key, err := hex.DecodeString(k)
				require.NoError(t, err)
				value, err := txn.GetOne(kv.Code, key)
				require.NoError(t, err)

				require.Equal(t, v, hex.EncodeToString(value), "Incarnation mismatch for key %s, from cache: %s, from db: %s", k, v, hex.EncodeToString(value))
			}
			return nil
		})
	}
}
