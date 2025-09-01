package operations

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/rpc"
	types "github.com/ledgerwatch/erigon/zk/rpcdaemon"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
	"github.com/ledgerwatch/erigon/zkevm/hex"
	"github.com/ledgerwatch/erigon/zkevm/jsonrpc/client"
)

func GetBlockNumber() (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_blockNumber")
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

func GetBatchNumber() (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_batchNumber")
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

func GetEthSyncing(url string) (bool, error) {
	response, err := client.JSONRPCCall(url, "eth_syncing")
	if err != nil {
		return false, err
	}
	if response.Error != nil {
		return false, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	result := false
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return false, err
	}

	return result, nil
}

func GetNetVersion(url string) (uint64, error) {
	response, err := client.JSONRPCCall(url, "net_version")
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
	num, err := strconv.ParseUint(result, 10, 64)
	if err != nil {
		return 0, err
	}
	return num, nil
}

func GetBatchNumberByBlockNumber(l2Block *big.Int) (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_batchNumberByBlockNumber", hex.EncodeBig(l2Block))
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}
	return transHexToUint64(response.Result)
}

func GetBatchSealTime(batchNum *big.Int) (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_getBatchSealTime", hex.EncodeBig(batchNum))
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}
	return transHexToUint64(response.Result)
}

func GetBatchByNumber(batchNum *big.Int) (*types.Batch, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "zkevm_getBatchByNumber", hex.EncodeBig(batchNum))
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}
	result := types.Batch{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func GetBlockByHash(hash common.Hash) (*types.Block, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getBlockByHash", hash, true)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}
	result := types.Block{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func toBlockNumArg(number *big.Int) string {
	if number == nil {
		return "latest"
	}
	if number.Sign() >= 0 {
		return hex.EncodeBig(number)
	}
	// It's negative.
	if number.IsInt64() {
		return rpc.BlockNumber(number.Int64()).String()
	}
	// It's negative and large, which is invalid.
	panic(fmt.Sprintf("<invalid block number %d>", number))
}

// GetBlockByNumber retrieves a block by its number
func GetBlockByNumber(blockNumber *big.Int) (*types.Block, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getBlockByNumber", toBlockNumArg(blockNumber), true)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}
	result := types.Block{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func GetBlockHashByNumber(block uint64) (string, error) {
	blockData, err := GetBlockByNumber(big.NewInt(int64(block)))
	if err != nil {
		return "", err
	}
	return blockData.Hash.String(), nil
}

func GetInternalTransactions(hash common.Hash) ([]zktypes.InnerTx, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getInternalTransactions", hash)
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

func GetBlockInternalTransactions(block *big.Int) (map[common.Hash][]*zktypes.InnerTx, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getBlockInternalTransactions", hex.EncodeBig(block))
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}
	result := map[common.Hash][]*zktypes.InnerTx{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func GetTransactionByHash(hash common.Hash) (*types.Transaction, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getTransactionByHash", hash)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}
	result := types.Transaction{}
	err = json.Unmarshal(response.Result, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func GetGasPrice() (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_gasPrice")
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

func transHexToUint64(hex json.RawMessage) (uint64, error) {
	var result string
	err := json.Unmarshal(hex, &result)
	if err != nil {
		return 0, err
	}

	if len(result) > 1 && (result[:2] == "0x" || result[:2] == "0X") {
		result = result[2:]
	}

	result1, err := strconv.ParseUint(result, 16, 64)
	if err != nil {
		return 0, err
	}

	return result1, nil
}
func GetMinGasPrice() (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_minGasPrice")
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}

func GetMetricsPrometheus() (string, error) {
	client := http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(DefaultL2MetricsPrometheusURL)
	if err != nil {
		fmt.Println("Error:", err)
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func GetMetrics() (string, error) {
	client := http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(DefaultL2MetricsURL)
	if err != nil {
		fmt.Println("Error:", err)
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func GetBlockGasLimit() (uint64, error) {
	response, err := client.JSONRPCCall(DefaultL2NetworkURL, "eth_getBlockGasLimit")
	if err != nil {
		return 0, err
	}
	if response.Error != nil {
		return 0, fmt.Errorf("%d - %s", response.Error.Code, response.Error.Message)
	}

	return transHexToUint64(response.Result)
}
