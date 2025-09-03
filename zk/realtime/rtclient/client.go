package rtclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	ethereum "github.com/ledgerwatch/erigon"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/ethclient"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
	"github.com/ledgerwatch/erigon/zkevm/jsonrpc/client"
)

const (
	PendingTag = "pending"
	LatestTag  = "latest"
)

type RealtimeClient struct {
	*ethclient.Client
	url string
}

func NewRealtimeClient(ethClient *ethclient.Client, url string) *RealtimeClient {
	return &RealtimeClient{
		Client: ethClient,
		url:    url,
	}
}

type RealtimeDebugResult struct {
	ConfirmHeight   uint64   `json:"confirmHeight"`
	ExecutionHeight uint64   `json:"executionHeight"`
	Mismatches      []string `json:"mismatches"`
}

type BigInt struct {
	*big.Int
}

// UnmarshalJSON implements json.Unmarshaler for BigInt
func (bi *BigInt) UnmarshalJSON(data []byte) error {
	if bi.Int == nil {
		bi.Int = new(big.Int)
	}
	// Remove quotes
	unquotedData, err := strconv.Unquote(string(data))
	if err != nil {
		return err
	}

	if len(unquotedData) > 2 && unquotedData[0] == '0' && unquotedData[1] == 'x' {
		unquotedData = unquotedData[2:]
	}
	_, success := bi.SetString(unquotedData, 16)
	if !success {
		return errors.New("failed to convert string to big.Int")
	}
	return nil
}

type Int int

// UnmarshalJSON implements json.Unmarshaler for Int
func (i *Int) UnmarshalJSON(data []byte) error {
	unquotedData, err := strconv.Unquote(string(data))
	if err != nil {
		return err
	}

	if len(unquotedData) > 2 && unquotedData[0] == '0' && unquotedData[1] == 'x' {
		unquotedData = unquotedData[2:]
	}

	num, err := strconv.ParseInt(unquotedData, 16, 64)
	if err != nil {
		return err
	}

	*i = Int(num)
	return nil
}

// RpcTransaction represents a transaction that will serialize to the RPC representation of a transaction
type RpcTransaction struct {
	BlockNumber      *string         `json:"blockNumber,omitempty"`
	BlockHash        *common.Hash    `json:"blockHash,omitempty"`
	From             *common.Address `json:"from,omitempty"`
	Gas              *BigInt         `json:"gas,omitempty"`
	GasPrice         *BigInt         `json:"gasPrice,omitempty"`
	Hash             *string         `json:"hash,omitempty"`
	Input            *string         `json:"input,omitempty"`
	Nonce            *BigInt         `json:"nonce,omitempty"`
	R                *string         `json:"r,omitempty"`
	S                *string         `json:"s,omitempty"`
	To               *common.Address `json:"to,omitempty"`
	TransactionIndex *Int            `json:"transactionIndex,omitempty"`
	Type             *Int            `json:"type,omitempty"`
	V                *string         `json:"v,omitempty"`
	Value            *BigInt         `json:"value,omitempty"`
}

// RealtimeBlockNumber returns the number of the most recent block in real-time
func (rc *RealtimeClient) RealtimeBlockNumber() (uint64, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_blockNumber", LatestTag)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// RealtimePendingBlockNumber returns the number of the most recent block in real-time
func (rc *RealtimeClient) RealtimePendingBlockNumber() (uint64, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_blockNumber", PendingTag)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// RealtimeCall executes a new message call immediately without creating a transaction in real-time
func (rc *RealtimeClient) RealtimeCall(from, to common.Address, gas string, gasPrice string, value string, data string) (string, error) {
	txParams := map[string]any{
		"from":     from,
		"to":       to,
		"gas":      gas,
		"gasPrice": gasPrice,
		"value":    value,
		"data":     data,
	}

	response, err := client.JSONRPCCall(rc.url, "eth_call", txParams, PendingTag)
	if err != nil {
		return "", err
	}
	if response.Error != nil {
		return "", fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result string
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return "", err
	}

	return result, nil
}

// RealtimeEstimateGas estimates gas for a transaction in real-time
func (rc *RealtimeClient) RealtimeEstimateGas(args map[string]interface{}) (uint64, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_estimateGas", args)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result string
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return 0, err
	}

	// Convert hex string to uint64
	gasEstimate, err := hexutil.DecodeUint64(result)
	if err != nil {
		return 0, err
	}

	return gasEstimate, nil
}

// RealtimeGetBalance returns the balance of an account in real-time
func (rc *RealtimeClient) RealtimeGetBalance(address common.Address) (*big.Int, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getBalance", address, PendingTag)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var hexBalance string
	err = json.Unmarshal(response.Result, &hexBalance)
	if err != nil {
		return nil, err
	}

	if len(hexBalance) > 2 && (hexBalance[:2] == "0x" || hexBalance[:2] == "0X") {
		hexBalance = hexBalance[2:]
	}

	balance := new(big.Int)
	balance, ok := balance.SetString(hexBalance, 16)
	if !ok {
		return nil, fmt.Errorf("failed to convert hex to big.Int: %s", hexBalance)
	}

	return balance, nil
}

// RealtimeGetTransactionCount returns the number of transactions sent from an address in real-time
func (rc *RealtimeClient) RealtimeGetTransactionCount(address common.Address) (uint64, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getTransactionCount", address, PendingTag)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// RealtimeGetCode returns the code at a given address in real-time
func (rc *RealtimeClient) RealtimeGetCode(address common.Address) (string, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getCode", address, PendingTag)
	if err != nil {
		return "", err
	}
	if response.Error != nil {
		return "", fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var code string
	err = json.Unmarshal(response.Result, &code)
	if err != nil {
		return "", err
	}

	return code, nil
}

// RealtimeGetStorageAt returns the value from a storage position at a given address in real-time
func (rc *RealtimeClient) RealtimeGetStorageAt(address common.Address, position string, tag string) (string, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getStorageAt", address, position, tag)
	if err != nil {
		return "", err
	}
	if response.Error != nil {
		return "", fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result string
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return "", err
	}

	return result, nil
}

// RealtimeGetLatestBlockTransactionCount returns the number of transactions in the latest block in real-time
func (rc *RealtimeClient) RealtimeGetLatestBlockTransactionCount() (uint64, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getBlockTransactionCountByNumber", LatestTag)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// RealtimeGetPendingBlockTransactionCount returns the number of transactions in the pending block in real-time
func (rc *RealtimeClient) RealtimeGetPendingBlockTransactionCount() (uint64, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getBlockTransactionCountByNumber", PendingTag)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// RealtimeGetBlockTransactionCountByNumber returns the number of transactions in the block number in real-time
func (rc *RealtimeClient) RealtimeGetBlockTransactionCountByNumber(blockNumber uint64) (uint64, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getBlockTransactionCountByNumber", blockNumber)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// RealtimeGetTransactionByHash returns the information about a transaction requested by transaction hash in real-time
func (rc *RealtimeClient) RealtimeGetTransactionByHash(txHash common.Hash, includeExtraInfo *bool) (RpcTransaction, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getTransactionByHash", txHash, includeExtraInfo)
	if err != nil {
		return RpcTransaction{}, err
	}
	if response.Error != nil {
		return RpcTransaction{}, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	result := RpcTransaction{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return RpcTransaction{}, err
	}

	return result, nil
}

// RealtimeGetTransactionByHash returns raw information about a transaction requested by transaction hash in real-time
func (rc *RealtimeClient) RealtimeGetRawTransactionByHash(txHash common.Hash) ([]byte, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getRawTransactionByHash", txHash)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var hexResult string
	err = json.Unmarshal(response.Result, &hexResult)
	if err != nil {
		return nil, err
	}

	if len(hexResult) > 2 && (hexResult[:2] == "0x" || hexResult[:2] == "0X") {
		hexResult = hexResult[2:]
	}

	return hex.DecodeString(hexResult)
}

// RealtimeGetTransactionReceipt returns the receipt of a transaction by transaction hash in real-time
func (rc *RealtimeClient) RealtimeGetTransactionReceipt(txHash common.Hash) (*types.Receipt, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getTransactionReceipt", txHash)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result types.Receipt
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// RealtimeGetInternalTransactions returns the internal transactions for a given transaction hash in real-time
func (rc *RealtimeClient) RealtimeGetInternalTransactions(txHash common.Hash) ([]zktypes.InnerTx, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getInternalTransactions", txHash)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	result := []zktypes.InnerTx{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (rc *RealtimeClient) RealtimeGetTokenBalance(
	fromAddress common.Address,
	toAddress common.Address,
	erc20Addr common.Address,
) (*big.Int, error) {
	// Pack the balanceOf function call
	data, err := erc20ABI.Pack("balanceOf", toAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to pack balanceOf call: %v", err)
	}

	// Make the realtime eth_call
	result, err := rc.RealtimeCall(fromAddress, erc20Addr, "0x100000", "0x1", "0x0", fmt.Sprintf("0x%x", data))
	if err != nil {
		return nil, fmt.Errorf("failed to call contract: %v", err)
	}

	// Parse the hex result
	if len(result) > 2 && (result[:2] == "0x" || result[:2] == "0X") {
		result = result[2:]
	}

	balance := new(big.Int)
	balance, ok := balance.SetString(result, 16)
	if !ok {
		return nil, fmt.Errorf("failed to convert hex to big.Int: %s", result)
	}

	return balance, nil
}

func (rc *RealtimeClient) EthGetTokenBalance(
	ctx context.Context,
	addr common.Address,
	erc20Addr common.Address,
	height *big.Int,
) (*big.Int, error) {
	// Pack the balanceOf function call
	data, err := erc20ABI.Pack("balanceOf", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to pack balanceOf call: %v", err)
	}

	// Make the eth_call
	result, err := rc.CallContract(ctx, ethereum.CallMsg{
		To:   &erc20Addr,
		Data: data,
	}, height)
	if err != nil {
		return nil, fmt.Errorf("failed to call contract: %v", err)
	}

	// Unpack the result
	var balance *big.Int
	err = erc20ABI.UnpackIntoInterface(&balance, "balanceOf", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack result: %v", err)
	}

	return balance, nil
}

func (rc *RealtimeClient) RealtimeGetBlockByNumber(blockNumber uint64) (map[string]interface{}, error) {
	// Call eth_getBlockByNumber with fullTx=true to get full transaction details
	fullTx := true
	response, err := client.JSONRPCCall(rc.url, "eth_getBlockByNumber", blockNumber, fullTx)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result map[string]interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// RealtimeGetBlockByHash returns the information about a block requested by block hash in real-time
func (rc *RealtimeClient) RealtimeGetBlockByHash(blockHash common.Hash, fullTx bool) (map[string]interface{}, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getBlockByHash", blockHash, fullTx)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result map[string]interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// RealtimeGetBlockTransactionCountByHash returns the number of transactions in a block requested by block hash in real-time
func (rc *RealtimeClient) RealtimeGetBlockTransactionCountByHash(blockHash common.Hash) (uint64, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getBlockTransactionCountByHash", blockHash)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

func (rc *RealtimeClient) RealtimeGetBlockInternalTransactions(blockNumber uint64) (map[common.Hash][]*zktypes.InnerTx, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getBlockInternalTransactions", blockNumber)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result map[common.Hash][]*zktypes.InnerTx
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (rc *RealtimeClient) RealtimeGetBlockReceiptsByNumber(blockNumber uint64) ([]*types.Receipt, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getBlockReceipts", blockNumber)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result []*types.Receipt
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (rc *RealtimeClient) RealtimeGetBlockReceiptsByHash(blockHash common.Hash) ([]*types.Receipt, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_getBlockReceipts", blockHash)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result []*types.Receipt
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (rc *RealtimeClient) RealtimeEnabled() (bool, error) {
	response, err := client.JSONRPCCall(rc.url, "eth_realtimeEnabled")
	if err != nil {
		return false, err
	}
	if response.Error != nil {
		return false, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result bool
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return false, err
	}

	return result, nil
}
