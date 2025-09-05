package operations

import (
	"encoding/json"
	"fmt"
	"github.com/ledgerwatch/erigon/zkevm/log"
	"math/big"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/zkevm/jsonrpc/client"
)

// DebugTraceBlockByHash traces all transactions in the block given by block hash
func DebugTraceBlockByHash(blockHash common.Hash) (interface{}, error) {
	params := []interface{}{blockHash, map[string]interface{}{}}

	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "debug_traceBlockByHash", params...)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// DebugTraceBlockByNumber traces a block with specified options given by block number
func DebugTraceBlockByNumber(blockNumber uint64) (interface{}, error) {
	blockNumberHex := fmt.Sprintf("0x%x", blockNumber)
	params := []interface{}{blockNumberHex, map[string]interface{}{}}

	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "debug_traceBlockByNumber", params...)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// DebugTraceBatchByNumber traces all transactions in a batch given by batch number
func DebugTraceBatchByNumber(batchNumber uint64) (interface{}, error) {
	batchNumberHex := fmt.Sprintf("0x%x", batchNumber)
	params := []interface{}{batchNumberHex, map[string]interface{}{}}

	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "debug_traceBatchByNumber", params...)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// DebugTraceTransaction traces a transaction with specified options
func DebugTraceTransaction(txHash common.Hash) (interface{}, error) {
	params := []interface{}{txHash, map[string]interface{}{}}

	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "debug_traceTransaction", params...)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ZKEVMGetExitRootTable returns the exit root table
func ZKEVMGetExitRootTable() (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_getExitRootTable")
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// EthChainID returns the chain ID of the current network
func EthChainID() (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_chainId")
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// EthEstimateGas estimates the gas required for a transaction
func EthEstimateGas(from, to common.Address, gas string, gasPrice string, value string, data string) (uint64, error) {
	txParams := map[string]interface{}{
		"from":     from,
		"to":       to,
		"gas":      gas,
		"gasPrice": gasPrice,
		"value":    value,
		"data":     data,
	}

	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_estimateGas", txParams)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// EthGetBalance returns the balance of an account
func EthGetBalance(address common.Address, block string) (*big.Int, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getBalance", address, block)
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

// EthGetBlockByHash returns information about a block by hash
func EthGetBlockByHash(blockHash common.Hash, fullTx bool) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getBlockByHash", blockHash, fullTx)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// EthGetBlockByNumber returns information about a block by number
func EthGetBlockByNumber(blockNumber string, fullTx bool) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getBlockByNumber", blockNumber, fullTx)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// EthGetBlockTransactionCountByHash returns the number of transactions in a block by hash
func EthGetBlockTransactionCountByHash(blockHash common.Hash) (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getBlockTransactionCountByHash", blockHash)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// EthGetBlockTransactionCountByNumber returns the number of transactions in a block by number
func EthGetBlockTransactionCountByNumber(blockNumber string) (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getBlockTransactionCountByNumber", blockNumber)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// EthGetCode returns the code at a given address
func EthGetCode(address common.Address, block string) (string, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getCode", address, block)
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

// EthGetTransactionCount returns the number of transactions sent from an address
func EthGetTransactionCount(address common.Address, block string) (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getTransactionCount", address, block)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// EthSyncing returns an object with data about the sync status
func EthSyncing() (bool, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_syncing")
	if err != nil {
		return false, err
	}
	if response.Error != nil {
		return false, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result bool
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		// If the result is not a boolean, it might be a sync object
		// For simplicity, we return true if it's not false
		return true, nil
	}

	return result, nil
}

// TxPoolContent returns the transactions that are in the transaction pool
func TxPoolContent() (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "txpool_content")
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// TxPoolStatus returns the number of transactions in the pool
func TxPoolStatus() (map[string]any, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "txpool_status")
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result map[string]any
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func RemoveTransaction(networkUrl string, txHash common.Hash) error {
	response, err := client.JSONRPCCall(networkUrl, "txpool_removeTransaction", txHash)
	if err != nil {
		return err
	}
	log.Info("Removed transaction result: ", response.Result)
	return nil
}

// EthBlockNumber returns the number of the most recent block
func EthBlockNumber() (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_blockNumber")
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// EthCall executes a new message call immediately without creating a transaction
func EthCall(from, to common.Address, gas string, gasPrice string, value string, data string, block string) (string, error) {
	txParams := map[string]interface{}{
		"from":     from,
		"to":       to,
		"gas":      gas,
		"gasPrice": gasPrice,
		"value":    value,
		"data":     data,
	}

	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_call", txParams, block)
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

// EthGasPrice returns the current price per gas in wei
func EthGasPrice() (*big.Int, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_gasPrice")
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToBigInt(response.Result)
}

// EthGetLogs returns logs matching the given filter
func EthGetLogs(fromBlock, toBlock string, address common.Address) (interface{}, error) {
	filter := map[string]interface{}{
		"fromBlock": fromBlock,
		"toBlock":   toBlock,
		"address":   address,
	}

	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getLogs", filter)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// EthGetStorageAt returns the value from a storage position at a given address
func EthGetStorageAt(address common.Address, position string, block string) (string, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getStorageAt", address, position, block)
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

// EthGetTransactionByBlockHashAndIndex returns information about a transaction by block hash and transaction index position
func EthGetTransactionByBlockHashAndIndex(blockHash common.Hash, index string) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getTransactionByBlockHashAndIndex", blockHash, index)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// EthGetTransactionByBlockNumberAndIndex returns information about a transaction by block number and transaction index position
func EthGetTransactionByBlockNumberAndIndex(blockNumber string, index string) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getTransactionByBlockNumberAndIndex", blockNumber, index)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// EthGetTransactionByHash returns the information about a transaction requested by transaction hash
func EthGetTransactionByHash(txHash common.Hash) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getTransactionByHash", txHash)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// EthGetInternalTransactions returns the internal transactions for a given transaction hash
func EthGetInternalTransactions(txHash common.Hash) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getInternalTransactions", txHash)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// EthGetBlockInternalTransactions returns the internal transactions for a given block number
func EthGetBlockInternalTransactions(blockNumber string) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getBlockInternalTransactions", blockNumber)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// EthGetTransactionReceipt returns the receipt of a transaction by transaction hash
func EthGetTransactionReceipt(txHash common.Hash) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getTransactionReceipt", txHash)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// TxPoolLimbo returns the transactions that are in the limbo state
func TxPoolLimbo() (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "txpool_limbo")
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ZKEVMBatchNumber returns the current batch number
func ZKEVMBatchNumber() (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_batchNumber")
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// ZKEVMGetLatestDataStreamBlock returns the latest data stream block
func ZKEVMGetLatestDataStreamBlock() (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_getLatestDataStreamBlock")
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ZKEVMEstimateCounters estimates the counters for a given transaction
func ZKEVMEstimateCounters(from, to common.Address, gas, gasPrice, value, data string) (interface{}, error) {
	txParams := map[string]interface{}{
		"from":     from,
		"to":       to,
		"gas":      gas,
		"gasPrice": gasPrice,
		"value":    value,
		"input":    data,
	}

	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_estimateCounters", txParams)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// SyncGetOffChainData returns off-chain data for a given hash
func SyncGetOffChainData(hash common.Hash) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "sync_getOffChainData", hash)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ZKEVMBatchNumberByBlockNumber returns the batch number for a given block number
func ZKEVMBatchNumberByBlockNumber(blockNumber string) (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_batchNumberByBlockNumber", blockNumber)
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

// ZKEVMGetBatchByNumber returns the batch information for a given batch number
func ZKEVMGetBatchByNumber(batchNumber uint64) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_getBatchByNumber", batchNumber)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ZKEVMGetFullBlockByHash returns the full block information for a given block hash
func ZKEVMGetFullBlockByHash(blockHash common.Hash, fullTx bool) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_getFullBlockByHash", blockHash, fullTx)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ZKEVMGetFullBlockByNumber returns the full block information for a given block number
func ZKEVMGetFullBlockByNumber(blockNumber uint64, fullTx bool) (interface{}, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_getFullBlockByNumber", blockNumber, fullTx)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	var result interface{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// transHexToBigInt converts a hex string to a big.Int
func transHexToBigInt(hexStr json.RawMessage) (*big.Int, error) {
	var hexString string
	err := json.Unmarshal(hexStr, &hexString)
	if err != nil {
		return nil, err
	}

	if len(hexString) > 2 && (hexString[:2] == "0x" || hexString[:2] == "0X") {
		hexString = hexString[2:]
	}

	value := new(big.Int)
	value, ok := value.SetString(hexString, 16)
	if !ok {
		return nil, fmt.Errorf("failed to convert hex to big.Int: %s", hexString)
	}

	return value, nil
}
