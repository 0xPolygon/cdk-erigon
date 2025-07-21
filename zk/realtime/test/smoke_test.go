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
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/mdbx"
	"github.com/ledgerwatch/erigon/accounts/abi"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/core/types/accounts"
	"github.com/ledgerwatch/erigon/crypto"
	"github.com/ledgerwatch/erigon/ethclient"
	"github.com/ledgerwatch/erigon/test/operations"
	"github.com/ledgerwatch/erigon/zkevm/encoding"
	"github.com/ledgerwatch/erigon/zkevm/log"
	logger "github.com/ledgerwatch/log/v3"
	"github.com/stretchr/testify/require"
)

func TestRealtimeRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	blockNumber := setupRealtimeTestEnvironment(t)

	// Preapre to deploy a ERC20 contract
	ctx := context.Background()
	client, err := ethclient.Dial(DefaultL2NetworkURL)
	require.NoError(t, err)

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(DefaultL2AdminPrivateKey, "0x"))
	require.NoError(t, err)
	fromAddress := common.HexToAddress(DefaultL2AdminAddress)
	log.Infof("Sender: %s", fromAddress)

	// Default test address for tests that require an address
	testAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Used to check whether the result returned by the interface call is correct
	time.Sleep(1 * time.Second)
	originNonce, err := RealtimeGetTransactionCount(fromAddress)
	require.NoError(t, err)
	originBalance, err := RealtimeGetBalance(testAddress)
	require.NoError(t, err)

	// Transfer native token
	txHash := transToken(t, context.Background(), client, uint256.NewInt(encoding.Gwei), testAddress.String())

	// Deploy the contract
	erc20Address := deployERC20Contract(t, ctx, privateKey, client)

	t.Run("RealtimeGetBlockTransactionCountByNumber", func(t *testing.T) {
		transactionCount, err := RealtimeGetBlockTransactionCountByNumber(blockNumber)
		require.NoError(t, err)
		log.Infof("RealtimeGetBlockTransactionCountByNumber result: %d", transactionCount)
	})

	t.Run("RealtimeGetTransactionByHash", func(t *testing.T) {
		includeExtraInfo := true
		result, err := RealtimeGetTransactionByHash(common.HexToHash(txHash), &includeExtraInfo)
		require.NoError(t, err)
		log.Infof("RealtimeGetTransactionByHash result type: %T", result)
	})

	t.Run("RealtimeGetRawTransactionByHash", func(t *testing.T) {
		result, err := RealtimeGetRawTransactionByHash(common.HexToHash(txHash))
		require.NoError(t, err)
		log.Infof("RealtimeGetRawTransactionByHash result type: %T", result)
	})

	t.Run("RealtimeGetTransactionReceipt", func(t *testing.T) {
		receipt, err := RealtimeGetTransactionReceipt(common.HexToHash(txHash))
		require.NoError(t, err)
		require.NotNil(t, receipt)
		log.Infof("RealtimeGetTransactionReceipt result type: %T", receipt)
	})

	t.Run("RealtimeGetInternalTransactions", func(t *testing.T) {
		tx, err := RealtimeGetInternalTransactions(common.HexToHash(txHash))
		require.NoError(t, err)
		log.Infof("RealtimeGetInternalTransactions result type: %T", tx)
	})

	t.Run("RealtimeGetBalance", func(t *testing.T) {
		balance, err := RealtimeGetBalance(testAddress)
		require.NoError(t, err)
		require.Equal(t, originBalance.Add(originBalance, big.NewInt(encoding.Gwei)).String(), balance.String(), "Balance should increase by 1 Gwei")
		log.Infof("RealtimeGetBalance result for test address: %s", balance.String())
	})

	t.Run("RealtimeGetTransactionCount", func(t *testing.T) {
		nonce, err := RealtimeGetTransactionCount(fromAddress)
		require.NoError(t, err)
		require.Equal(t, originNonce+2, nonce)
		log.Infof("RealtimeGetTransactionCount result for sender address: %d", nonce)
	})

	t.Run("RealtimeGetCode", func(t *testing.T) {
		code, err := RealtimeGetCode(erc20Address)
		require.NoError(t, err)
		require.NotEmpty(t, code, "Contract code should not be empty")
		log.Infof("RealtimeGetCode result for erc20 contract %s: %s", erc20Address, code)
	})

	t.Run("RealtimeGetStorageAt", func(t *testing.T) {
		// 0x2 is refered to _totalSupply field
		value, err := RealtimeGetStorageAt(erc20Address, "0x2")
		require.NoError(t, err)
		require.Equal(t, "0x00000000000000000000000000000000000000000052b7d2dcc80cd2e4000000", value, "Storage at index 0x2 should be equal to 1000000000000000000000")
		log.Infof("RealtimeGetStorageAt result for erc20 contract %s at index %s: %s", erc20Address, "0x2", value)
	})

	t.Run("RealtimeCall", func(t *testing.T) {
		data, err := erc20ABI.Pack("balanceOf", fromAddress)
		require.NoError(t, err)
		value, err := RealtimeCall(testAddress, erc20Address, "0x100000", "0x1", "0x0", fmt.Sprintf("0x%x", data))
		require.NoError(t, err)
		require.Equal(t, "0x00000000000000000000000000000000000000000052b7d2dcc80cd2e4000000", value, fmt.Sprintf("Balance of %s should be equal to 1000000000000000000000", fromAddress))
		log.Infof("RealtimeCall result for erc20 contract %s calling method balanceOf %s: %s", erc20Address, fromAddress, value)
	})
}

func TestRealtimeStateIsConsistent(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	ctx := context.Background()
	client, err := ethclient.Dial(DefaultL2NetworkURL)
	require.NoError(t, err)

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(DefaultL2AdminPrivateKey, "0x"))
	require.NoError(t, err)
	signer := types.MakeSigner(operations.GetTestChainConfig(DefaultL2ChainID), 1, 0)

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	require.True(t, ok)
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	log.Infof("Sender: %s", fromAddress)

	erc20ABI, err := abi.JSON(strings.NewReader(erc20ABIJson))
	require.NoError(t, err)

	// Deploy the contract
	erc20Address := deployERC20Contract(t, ctx, privateKey, client)

	// Get the sender's nonce
	nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
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
		receipt, err := RealtimeGetTransactionReceipt(signedTx.Hash())
		require.NoError(t, err)
		require.NotNil(t, receipt)
		log.Infof("receipt: %+v", receipt)
	}

	// Dump state cache for further checking
	err = RealtimeDumpCache()
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
	db, err := mdbx.NewMDBX(logger.New()).Path(copiedDbDir).Open(ctx)
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
