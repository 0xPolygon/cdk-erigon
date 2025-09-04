package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	"github.com/ledgerwatch/log/v3"

	"github.com/ledgerwatch/erigon/accounts/abi"
	"github.com/ledgerwatch/erigon/core"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types"

	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/eth/tracers"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/turbo/adapter/ethapi"
	"github.com/ledgerwatch/erigon/turbo/rpchelper"
)

const (
	UnKnownErrCode             = 1000
	InsufficientBalanceErrCode = 1001
	RevertedErrCode            = 1002
	CheckPreArgsErrCode        = 1003
)

// PreExecInnerTx defines the structure for inner transactions returned by TransactionPreExec RPC
// This is specifically designed for the eth_transaction_preexec endpoint
type PreExecInnerTx struct {
	Dept          big.Int `json:"dept"`
	InternalIndex big.Int `json:"internal_index"`
	CallType      string  `json:"call_type"`
	Name          string  `json:"name"`
	TraceAddress  string  `json:"trace_address"`
	CodeAddress   string  `json:"code_address"`
	From          string  `json:"from"`
	To            string  `json:"to"`
	Input         string  `json:"input"`
	Output        string  `json:"output"`
	IsError       bool    `json:"is_error"`
	GasUsed       uint64  `json:"gas_used"`
	Value         string  `json:"value"`
	ValueWei      string  `json:"value_wei"`
	Error         string  `json:"error"`
	ReturnGas     uint64  `json:"return_gas"`
}

// PreArgs represents the arguments for transaction pre-execution
type PreArgs struct {
	ChainId              *big.Int          `json:"chainId,omitempty"`
	From                 *common.Address   `json:"from"`
	To                   *common.Address   `json:"to"`
	Gas                  *hexutil.Uint64   `json:"gas"`
	GasPrice             *hexutil.Big      `json:"gasPrice"`
	MaxFeePerGas         *hexutil.Big      `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *hexutil.Big      `json:"maxPriorityFeePerGas"`
	Value                *hexutil.Big      `json:"value"`
	Nonce                *hexutil.Uint64   `json:"nonce"`
	Data                 *hexutility.Bytes `json:"data"`
	Input                *hexutility.Bytes `json:"input"`
	AuthorizationList    []interface{}     `json:"authorizationList,omitempty"`
}

func (args PreArgs) ToLogString() string {
	argsBytes, _ := json.Marshal(args)
	return string(argsBytes)
}

// PreError represents an error in pre-execution
type PreError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// StateAccount represents an account state for prestate tracer
type StateAccount struct {
	Balance string            `json:"balance"`
	Code    string            `json:"code"`
	Nonce   uint64            `json:"nonce"`
	Storage map[string]string `json:"storage"`
}

// CallTracerResult represents the result from callTracer
type CallTracerResult struct {
	Calls        []CallTracerResult `json:"calls"`
	From         string             `json:"from"`
	Gas          string             `json:"gas"`
	GasUsed      string             `json:"gasUsed"`
	Input        string             `json:"input"`
	Output       string             `json:"output,omitempty"`
	To           string             `json:"to"`
	Type         string             `json:"type"`
	Value        string             `json:"value"`
	Error        string             `json:"error,omitempty"`
	RevertReason string             `json:"revertReason,omitempty"`
}

func toPreError(err error, result *core.ExecutionResult) PreError {
	preErr := PreError{
		Code: UnKnownErrCode,
	}
	if err != nil {
		preErr.Msg = err.Error()
	}
	if result != nil && result.Err != nil {
		preErr.Msg = result.Err.Error()
	}
	if strings.HasPrefix(preErr.Msg, "execution reverted") {
		preErr.Code = RevertedErrCode
		if result != nil {
			preErr.Msg, _ = abi.UnpackRevert(result.Revert())
		}
	}
	if strings.HasPrefix(preErr.Msg, "out of gas") {
		preErr.Code = RevertedErrCode
	}
	if strings.HasPrefix(preErr.Msg, "insufficient funds for transfer") {
		preErr.Code = InsufficientBalanceErrCode
	}
	if strings.HasPrefix(preErr.Msg, "insufficient balance for transfer") {
		preErr.Code = InsufficientBalanceErrCode
	}
	if strings.HasPrefix(preErr.Msg, "insufficient funds for gas * price") {
		preErr.Code = InsufficientBalanceErrCode
	}
	return preErr
}

// PreResult represents the result of transaction pre-execution
type PreResult struct {
	InnerTxs    interface{} `json:"innerTxs"`
	Logs        interface{} `json:"logs"`
	StateDiff   interface{} `json:"stateDiff"`
	Error       PreError    `json:"error"`
	GasUsed     uint64      `json:"gasUsed"`
	BlockNumber *big.Int    `json:"blockNumber"`
}

func (res PreResult) ToLogString() string {
	// Simplified logging - just marshal basic result
	resBytes, _ := json.Marshal(res)
	if len(resBytes) > 500 {
		return string(resBytes[:500]) + "..."
	}
	return string(resBytes)
}

func toPreResult(innerTxs []*PreExecInnerTx, logs []*types.Log, stateDiff map[string]interface{},
	preError PreError, gasUsed uint64, number *big.Int) PreResult {
	preResult := PreResult{
		Error:       preError,
		GasUsed:     gasUsed,
		BlockNumber: number,
	}
	if len(innerTxs) > 0 {
		preResult.InnerTxs = innerTxs
	} else {
		preResult.InnerTxs = make([]*PreExecInnerTx, 0)
	}
	if len(logs) > 0 {
		preResult.Logs = logs
	} else {
		preResult.Logs = make([]*types.Log, 0)
	}
	if len(stateDiff) > 0 {
		preResult.StateDiff = stateDiff
	} else {
		preResult.StateDiff = make(map[string]interface{})
	}

	return preResult
}

// TransactionPreExec executes multiple transactions in sequence and returns their execution results
func (api *APIImpl) TransactionPreExec(ctx context.Context, origins []PreArgs, blockNrOrHash *rpc.BlockNumberOrHash, stateOverrides *ethapi.FlexibleStateOverrides) ([]PreResult, error) {
	start := time.Now()
	requestID := uuid.NewString()
	defer func(s time.Time, id string) {
		log.Info("Executing TransactionPreExec call finished", "requestID", id, "runtime", time.Since(s))
	}(start, requestID)

	preResList := make([]PreResult, 0)

	// Get database transaction
	tx, err := api.db.BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Use specified block or default to latest (consistent with other RPC methods)
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}

	blockNumber, hash, _, err := rpchelper.GetCanonicalBlockNumber_zkevm(bNrOrHash, tx, api.filters)
	if err != nil {
		return nil, err
	}

	// Create state reader for the specified block
	stateReader, err := rpchelper.CreateStateReader(ctx, tx, bNrOrHash, 0, api.filters, api.stateCache, api.historyV3(tx), "")
	if err != nil {
		return nil, err
	}

	// Get header - prefer by hash if available, otherwise by number
	var header *types.Header
	if hash != (common.Hash{}) {
		header, err = api._blockReader.HeaderByHash(ctx, tx, hash)
	} else {
		header, err = api._blockReader.HeaderByNumber(ctx, tx, blockNumber)
	}
	if err != nil {
		return nil, err
	}
	if header == nil {
		return nil, fmt.Errorf("block header not found for block %d (hash: %s)", blockNumber, hash.Hex())
	}

	// Create state
	ibs := state.New(stateReader)

	// Apply state overrides if provided
	if stateOverrides != nil {
		log.Info("TransactionPreExec: applying flexible state overrides", "requestID", requestID, "overrides", len(*stateOverrides))
		adapter := &ethapi.IntraBlockStateAdapter{IntraBlockState: ibs}
		err = stateOverrides.Override(adapter)
		if err != nil {
			return nil, err
		}
		// Log the state after overrides
		for addr, override := range *stateOverrides {
			if override.Balance != nil {
				log.Info("TransactionPreExec: state override applied", "requestID", requestID, "address", addr.Hex())
			}
		}
	}

	blockBigNumber := new(big.Int).Set(header.Number)

	// Get chain config once outside the loop for better performance
	chainConfig, err := api.chainConfig(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain config: %v", err)
	}

	// Set default chainId from config (can be overridden per transaction)
	defaultChainId := chainConfig.ChainID

	// Setup context with timeout
	timeout := 5 * time.Second
	if len(origins) > 0 {
		timeout = time.Duration(len(origins)) * timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Get current block gas limit
	currentBlockGasLimit, err := api.GetBlockGasLimit(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block gas limit: %v", err)
	}
	blockGasLimit := currentBlockGasLimit.ToInt().Uint64()

	// Track cumulative gas usage across all transactions in the batch
	var cumulativeGasUsed uint64

	// Process each transaction
	for i, origin := range origins {
		var gasUsed uint64
		log.Info("TransactionPreExec", "requestID", requestID, "input index", i, "input args", origin.ToLogString())

		if err := preArgsCheck(ibs, origin); err != nil {
			preError := PreError{
				Code: CheckPreArgsErrCode,
				Msg:  err.Error(),
			}
			preResult := toPreResult(nil, nil, nil, preError, gasUsed, blockBigNumber)
			preResList = append(preResList, preResult)
			continue
		}

		// Check whether sender's nonce decreases in batch transactions
		if i > 0 && *origin.From == *origins[i-1].From && origin.Nonce != nil && origins[i-1].Nonce != nil && uint64(*origin.Nonce) <= uint64(*origins[i-1].Nonce) {
			preError := PreError{
				Code: CheckPreArgsErrCode,
				Msg:  fmt.Sprintf("%v nonce decreases, tx index %d has nonce %d, tx index %d has nonce %d", origin.From.Hex(), i-1, uint64(*origins[i-1].Nonce), i, uint64(*origin.Nonce)),
			}
			preResult := toPreResult(nil, nil, nil, preError, gasUsed, blockBigNumber)
			preResList = append(preResList, preResult)
			continue
		}

		// Calculate remaining gas limit for this transaction
		remainingGasLimit := blockGasLimit - cumulativeGasUsed

		// Check if there's any gas left for this transaction
		if remainingGasLimit == 0 {
			preError := PreError{
				Code: RevertedErrCode,
				Msg:  fmt.Sprintf("block gas limit exceeded: no gas remaining for transaction %d", i),
			}
			preResult := toPreResult(nil, nil, nil, preError, gasUsed, blockBigNumber)
			preResList = append(preResList, preResult)
			continue
		}

		// Set default gas if not provided, but cap it at remaining gas limit
		if origin.Gas == nil {
			gas := remainingGasLimit
			origin.Gas = (*hexutil.Uint64)(&gas)
		} else if uint64(*origin.Gas) > remainingGasLimit {
			// If requested gas exceeds remaining limit, cap it at remaining limit
			gas := remainingGasLimit
			origin.Gas = (*hexutil.Uint64)(&gas)
			log.Warn("TransactionPreExec: gas limit capped", "requestID", requestID, "txIndex", i,
				"requestedGas", uint64(*origin.Gas), "cappedGas", gas, "remainingGas", remainingGasLimit)
		}

		// Use default chainId or override if specified in transaction
		chainId := defaultChainId
		if origin.ChainId != nil {
			chainId = origin.ChainId
		}

		// Convert to transaction args
		txArgs := ethapi.CallArgs{
			ChainID:              (*hexutil.Big)(chainId),
			From:                 origin.From,
			To:                   origin.To,
			Gas:                  origin.Gas,
			GasPrice:             origin.GasPrice,
			MaxFeePerGas:         origin.MaxFeePerGas,
			MaxPriorityFeePerGas: origin.MaxPriorityFeePerGas,
			Value:                origin.Value,
			Data:                 origin.Data,
			Input:                origin.Input,
		}

		// Convert to message
		var baseFee *uint256.Int
		if header.BaseFee != nil {
			var overflow bool
			baseFee, overflow = uint256.FromBig(header.BaseFee)
			if overflow {
				log.Error("TransactionPreExec: header.BaseFee uint256 overflow", "requestID", requestID)
				preError := PreError{
					Code: UnKnownErrCode,
					Msg:  "header.BaseFee uint256 overflow",
				}
				preResult := toPreResult(nil, nil, nil, preError, gasUsed, blockBigNumber)
				preResList = append(preResList, preResult)
				continue
			}
		}
		msg, err := txArgs.ToMessage(api.GasCap, baseFee)
		if err != nil {
			log.Error("TransactionPreExec: tx args to message failed", "requestID", requestID, "error", err.Error())
			preError := PreError{
				Code: UnKnownErrCode,
				Msg:  err.Error(),
			}
			preResult := toPreResult(nil, nil, nil, preError, gasUsed, blockBigNumber)
			preResList = append(preResList, preResult)
			continue
		}

		// Create tracer for state diffs
		txHash := common.BigToHash(big.NewInt(int64(i)))
		traceConfig := []byte(`{
			"prestateTracer": {
				"diffMode": true
			},
			"callTracer": {}
		}`)

		tracer, err := tracers.New("muxTracer", &tracers.Context{
			BlockHash: header.Hash(),
			TxIndex:   0,
			TxHash:    txHash,
		}, traceConfig)
		if err != nil {
			log.Error("TransactionPreExec: generate muxTracer failed", "requestID", requestID, "input args", origin.ToLogString(), "error", err.Error())
			// Continue without tracer instead of returning error
			tracer = nil
			log.Warn("TransactionPreExec: proceeding without tracer", "requestID", requestID)
		}

		// Create EVM
		vmconfig := vm.Config{
			Debug:      tracer != nil, // Only enable debug mode when tracer is available
			NoBaseFee:  true,
			NoInnerTxs: false, // Enable inner transactions tracking
		}
		if tracer != nil {
			vmconfig.Tracer = tracer
		}

		getHashFunc := func(n uint64) common.Hash {
			h, _ := api._blockReader.HeaderByNumber(ctx, tx, n)
			if h != nil {
				return h.Hash()
			}
			return common.Hash{}
		}

		blockCtx := core.NewEVMBlockContext(header, getHashFunc, api.engine(), nil)
		txCtx := core.NewEVMTxContext(msg)

		// Create ZK-EVM instance for proper InnerTx tracking
		evm := vm.NewZkEVM(blockCtx, txCtx, ibs, chainConfig, vm.ZkConfig{Config: vmconfig})

		// Cancel the evm when context is done
		go func() {
			<-ctx.Done()
			evm.Cancel()
		}()

		// Execute the message with remaining gas limit
		gp := new(core.GasPool).AddGas(remainingGasLimit)
		ibs.SetTxContext(txHash, header.Hash(), i)

		result, err := core.ApplyMessage(evm, core.Message(msg), gp, true, false)
		if result != nil {
			gasUsed = result.UsedGas
		}

		if err != nil {
			log.Error("TransactionPreExec: core apply message failed", "requestID", requestID, "input args", origin.ToLogString(), "error", err.Error())
			preError := toPreError(err, result)
			preResult := toPreResult(nil, nil, nil, preError, gasUsed, blockBigNumber)
			preResList = append(preResList, preResult)
			continue
		}

		// Check if EVM was cancelled due to timeout
		if evm.Cancelled() {
			log.Error("TransactionPreExec: evm execution aborted timeout", "requestID", requestID, "input args", origin.ToLogString())
			preError := PreError{
				Code: UnKnownErrCode,
				Msg:  fmt.Sprintf("execution aborted (timeout = %v)", timeout),
			}
			preResult := toPreResult(nil, nil, nil, preError, gasUsed, blockBigNumber)
			preResList = append(preResList, preResult)
			continue
		}

		var stateDiff map[string]interface{}
		var innerTxs []*PreExecInnerTx

		// Get trace results from tracer if available
		if tracer != nil {
			rawRes, err := tracer.GetResult()
			if err != nil {
				log.Error("TransactionPreExec: tracer get result failed", "requestID", requestID, "input args", origin.ToLogString(), "error", err.Error())
				preError := toPreError(err, result)
				preResult := toPreResult(nil, nil, nil, preError, gasUsed, blockBigNumber)
				preResList = append(preResList, preResult)
				continue
			}

			// muxTracer returns format: {"prestateTracer": {...}, "callTracer": ...}
			var muxResult map[string]json.RawMessage
			if err := json.Unmarshal(rawRes, &muxResult); err != nil {
				log.Error("TransactionPreExec: failed to unmarshal muxTracer result", "requestID", requestID, "input args", origin.ToLogString(), "error", err.Error())
				preError := toPreError(err, result)
				preResult := toPreResult(nil, nil, nil, preError, gasUsed, blockBigNumber)
				preResList = append(preResList, preResult)
				continue
			} else {
				// Extract prestateTracer result
				if prestateRaw, exists := muxResult["prestateTracer"]; exists {
					var prestateResult interface{}
					if err := json.Unmarshal(prestateRaw, &prestateResult); err == nil {
						stateDiff = convertPrestateToStateDiff(prestateResult)
					} else {
						log.Warn("TransactionPreExec: failed to unmarshal prestateTracer result", "requestID", requestID, "error", err)
					}
				}

				// Extract callTracer result and convert to innerTxs
				if callTracerRaw, exists := muxResult["callTracer"]; exists {
					var callTracerResult interface{}
					if err := json.Unmarshal(callTracerRaw, &callTracerResult); err == nil {
						if convertedInnerTxs, err := convertCallTracerResultToInnerTxs(callTracerResult); err == nil {
							// Check if we should return innerTxs based on depth analysis and error status
							// Return innerTxs if:
							// 1. There are calls with depth > 0, OR
							// 2. There are failed calls (even if only depth 0)
							hasDeepCalls := false
							hasFailedCalls := false
							for _, innerTx := range convertedInnerTxs {
								if innerTx.Dept.Int64() > 0 {
									hasDeepCalls = true
								}
								if innerTx.IsError || innerTx.Error != "" {
									hasFailedCalls = true
								}
							}

							// Return innerTxs if there are deep calls OR failed calls
							if hasDeepCalls || hasFailedCalls {
								innerTxs = convertedInnerTxs
								if hasFailedCalls {
									log.Debug("TransactionPreExec: returning innerTxs due to failed calls", "requestID", requestID, "totalInnerTxs", len(innerTxs))
								} else {
									log.Debug("TransactionPreExec: returning innerTxs with deep calls", "requestID", requestID, "totalInnerTxs", len(innerTxs))
								}
							} else {
								log.Debug("TransactionPreExec: only successful depth 0 calls found, returning empty innerTxs", "requestID", requestID)
							}
						} else {
							log.Error("TransactionPreExec: failed to convert callTracer result to innerTxs", "requestID", requestID, "input args", origin.ToLogString(), "error", err.Error())
							preError := toPreError(err, result)
							preResult := toPreResult(nil, nil, nil, preError, gasUsed, blockBigNumber)
							preResList = append(preResList, preResult)
							continue
						}
					} else {
						log.Error("TransactionPreExec: failed to unmarshal callTracer result", "requestID", requestID, "input args", origin.ToLogString(), "error", err.Error())
						preError := toPreError(err, result)
						preResult := toPreResult(nil, nil, nil, preError, gasUsed, blockBigNumber)
						preResList = append(preResList, preResult)
						continue
					}
				}
			}
		}

		preRes := toPreResult(innerTxs, ibs.GetLogs(txHash), stateDiff, PreError{}, gasUsed, blockBigNumber)

		// Handle execution result errors
		if result != nil && result.Failed() {
			preRes.Error = toPreError(result.Err, result)
		}

		if preRes.Error.Msg == "" && len(innerTxs) != 0 && innerTxs[0].Error != "" {
			preRes.Error = PreError{
				Code: RevertedErrCode,
				Msg:  innerTxs[0].Error,
			}
		}

		preResList = append(preResList, preRes)

		// Update cumulative gas usage for next transaction
		cumulativeGasUsed += gasUsed

		log.Info("TransactionPreExec execute finished", "requestID", requestID, "index", i,
			"gasUsed", gasUsed, "cumulativeGasUsed", cumulativeGasUsed,
			"remainingGas", blockGasLimit-cumulativeGasUsed)
	}

	return preResList, nil
}

func convertPrestateToStateDiff(traceResult interface{}) map[string]interface{} {
	if traceResult == nil {
		return make(map[string]interface{})
	}

	result := make(map[string]interface{})

	// First, try to convert to JSON and parse the structure
	stateDiffResultStr, err := json.Marshal(traceResult)
	if err != nil {
		log.Warn("Failed to marshal prestate tracer result", "error", err)
		return result
	}

	// Parse the prestateTracer result which has the format: {"pre": {...}, "post": {...}}
	var prestateResult struct {
		Pre  map[string]*StateAccount `json:"pre"`
		Post map[string]*StateAccount `json:"post"`
	}

	if err := json.Unmarshal(stateDiffResultStr, &prestateResult); err != nil {
		log.Warn("Failed to unmarshal prestate tracer result", "error", err)
		return result
	}

	if len(prestateResult.Pre) == 0 || len(prestateResult.Post) == 0 {
		return result
	}

	// Process each address that has changes
	for addr, postState := range prestateResult.Post {
		if preState, exist := prestateResult.Pre[addr]; exist {
			addrMap := make(map[string]interface{})
			preStateBalance, postStateBalance := new(big.Int), new(big.Int)

			// Parse pre-state balance
			if preState.Balance != "" {
				if strings.HasPrefix(preState.Balance, "0x") {
					preStateBalance, _ = big.NewInt(0).SetString(preState.Balance[2:], 16)
				} else {
					preStateBalance, _ = big.NewInt(0).SetString(preState.Balance, 10)
				}
			}

			// Parse post-state balance
			if postState.Balance != "" {
				if strings.HasPrefix(postState.Balance, "0x") {
					postStateBalance, _ = big.NewInt(0).SetString(postState.Balance[2:], 16)
				} else {
					postStateBalance, _ = big.NewInt(0).SetString(postState.Balance, 10)
				}
			} else {
				// If post balance is empty, use pre balance
				postStateBalance = preStateBalance
			}

			// Add balance changes
			balance := struct {
				Before string `json:"before"`
				After  string `json:"after"`
			}{
				Before: preStateBalance.String(),
				After:  postStateBalance.String(),
			}
			addrMap["balance"] = balance

			// Add nonce changes
			/*
				if preState.Nonce != postState.Nonce {
					nonce := struct {
						Before uint64 `json:"before"`
						After  uint64 `json:"after"`
					}{
						Before: preState.Nonce,
						After:  postState.Nonce,
					}
					addrMap["nonce"] = nonce
				}

				// Add code changes
				if preState.Code != postState.Code {
					code := struct {
						Before string `json:"before"`
						After  string `json:"after"`
					}{
						Before: preState.Code,
						After:  postState.Code,
					}
					addrMap["code"] = code
				}

				// Add storage changes
				storage := make(map[string]interface{})
				// Compare storage from pre and post states
				allKeys := make(map[string]bool)
				if preState.Storage != nil {
					for key := range preState.Storage {
						allKeys[key] = true
					}
				}
				if postState.Storage != nil {
					for key := range postState.Storage {
						allKeys[key] = true
					}
				}

				for key := range allKeys {
					var preval, postval string
					if preState.Storage != nil {
						preval = preState.Storage[key]
					}
					if postState.Storage != nil {
						postval = postState.Storage[key]
					}

					if preval != postval {
						storageChange := struct {
							Before string `json:"before"`
							After  string `json:"after"`
						}{
							Before: preval,
							After:  postval,
						}
						storage[key] = storageChange
					}
				}

				if len(storage) > 0 {
					addrMap["storage"] = storage
				}
			*/

			// Convert address to EIP-55 checksummed format
			checksummedAddr := common.HexToAddress(addr).Hex()
			result[checksummedAddr] = addrMap
		}
	}

	return result
}

// convertCallTracerResultToInnerTxs converts callTracer result to InnerTx array
func convertCallTracerResultToInnerTxs(traceResult interface{}) (result []*PreExecInnerTx, err error) {
	if traceResult == nil {
		return nil, fmt.Errorf("call tracer result is nil")
	}
	traceResultStr, err := json.Marshal(traceResult)
	if err != nil {
		return nil, err
	}
	callTx := CallTracerResult{}
	if err := json.Unmarshal(traceResultStr, &callTx); err != nil {
		return nil, err
	}

	result = make([]*PreExecInnerTx, 0)
	isError := false
	var errorMsg string
	if callTx.Error != "" {
		isError = true
		errorMsg = callTx.Error
	}
	if callTx.Error != "" && callTx.RevertReason != "" {
		isError = true
		errorMsg = fmt.Sprintf("%s,%s", callTx.Error, callTx.RevertReason)
	}
	gasUsed := new(big.Int)
	if len(callTx.GasUsed) > 2 && strings.HasPrefix(callTx.GasUsed, "0x") {
		gasUsed, _ = gasUsed.SetString(callTx.GasUsed[2:], 16)
	}

	valueWei := ""
	if len(callTx.Value) > 2 && strings.HasPrefix(callTx.Value, "0x") {
		valueWeiInt := new(big.Int)
		valueWeiInt, _ = valueWeiInt.SetString(callTx.Value[2:], 16)
		valueWei = valueWeiInt.String()
	}

	gas := new(big.Int)
	if len(callTx.Gas) > 2 && strings.HasPrefix(callTx.Gas, "0x") {
		gas, _ = gas.SetString(callTx.Gas[2:], 16)
	}

	// Calculate ReturnGas = Gas - GasUsed
	gasUint64 := gas.Uint64()
	gasUsedUint64 := gasUsed.Uint64()
	returnGas := uint64(0)
	if gasUint64 > gasUsedUint64 {
		returnGas = gasUint64 - gasUsedUint64
	}

	// Handle empty output - ensure it's "0x" instead of ""
	output := callTx.Output
	if output == "" {
		output = "0x"
	}

	innerTx := &PreExecInnerTx{
		Dept:          *big.NewInt(0),
		InternalIndex: *big.NewInt(int64(0)),
		CallType:      strings.ToLower(callTx.Type),
		Name:          strings.ToLower(callTx.Type),
		TraceAddress:  "",
		CodeAddress:   "",
		From:          common.HexToAddress(callTx.From).Hex(), // Convert to checksummed address
		To:            common.HexToAddress(callTx.To).Hex(),   // Convert to checksummed address
		Input:         callTx.Input,
		Output:        output, // Use processed output
		IsError:       isError,
		// GasUsed:       gasUsedUint64, // For historical reason, we use gasUint64 here
		GasUsed:   gasUint64,
		Value:     valueWei,
		ValueWei:  valueWei,
		Error:     errorMsg,
		ReturnGas: returnGas,
	}
	result = append(result, innerTx)
	if len(callTx.Calls) > 0 {
		// convert calls to innerTxs
		callInnerTxs := convertCallsToInnerTxs(callTx.Calls, 0, "", isError)
		result = append(result, callInnerTxs...)
	}

	return
}

// convertCallsToInnerTxs converts nested calls to InnerTx array
func convertCallsToInnerTxs(calls []CallTracerResult, lastDepth int64, lastDepthIndexRoot string, isError bool) (result []*PreExecInnerTx) {
	result = make([]*PreExecInnerTx, 0)
	depth := lastDepth + 1
	for index, callTx := range calls {
		var errorMsg string
		if callTx.Error != "" {
			isError = true
			errorMsg = callTx.Error
		}
		if callTx.Error != "" && callTx.RevertReason != "" {
			isError = true
			errorMsg = fmt.Sprintf("%s,%s", callTx.Error, callTx.RevertReason)
		}
		gasUsed := new(big.Int)
		if len(callTx.GasUsed) > 2 && strings.HasPrefix(callTx.GasUsed, "0x") {
			gasUsed, _ = gasUsed.SetString(callTx.GasUsed[2:], 16)
		}

		gas := new(big.Int)
		if len(callTx.Gas) > 2 && strings.HasPrefix(callTx.Gas, "0x") {
			gas, _ = gas.SetString(callTx.Gas[2:], 16)
		}

		valueWei := ""
		if len(callTx.Value) > 2 && strings.HasPrefix(callTx.Value, "0x") {
			valueWeiInt := new(big.Int)
			valueWeiInt, _ = valueWeiInt.SetString(callTx.Value[2:], 16)
			valueWei = valueWeiInt.String()
		}

		// Calculate ReturnGas = Gas - GasUsed
		gasUint64 := gas.Uint64()
		gasUsedUint64 := gasUsed.Uint64()
		returnGas := uint64(0)
		if gasUint64 > gasUsedUint64 {
			returnGas = gasUint64 - gasUsedUint64
		}

		// Handle empty output - ensure it's "0x" instead of ""
		output := callTx.Output
		if output == "" {
			output = "0x"
		}

		innerTx := &PreExecInnerTx{
			Dept:          *big.NewInt(depth),
			InternalIndex: *big.NewInt(int64(index)),
			CallType:      strings.ToLower(callTx.Type),
			Name:          "",
			TraceAddress:  "",
			CodeAddress:   "",
			From:          common.HexToAddress(callTx.From).Hex(), // Convert to checksummed address
			To:            common.HexToAddress(callTx.To).Hex(),   // Convert to checksummed address
			Input:         callTx.Input,
			Output:        output, // Use processed output
			IsError:       isError,
			GasUsed:       gasUsedUint64,
			Value:         valueWei,
			ValueWei:      valueWei,
			Error:         errorMsg,
			ReturnGas:     returnGas,
		}

		// if CallType == "callcode", CodeAddress = callTx.To
		if strings.ToLower(callTx.Type) == "callcode" {
			innerTx.CodeAddress = common.HexToAddress(callTx.To).Hex() // Convert to checksummed address
		}

		// Record depth
		depthIndexRoot := fmt.Sprintf("%s_%d", lastDepthIndexRoot, index)
		// set name
		innerTx.Name = fmt.Sprintf("%s%s", innerTx.CallType, depthIndexRoot)
		// set trace address
		// if lastDepthIndexRoot == "" {
		// 	innerTx.TraceAddress = fmt.Sprintf("%d", index)
		// } else {
		// 	innerTx.TraceAddress = fmt.Sprintf("%s,%d", strings.ReplaceAll(lastDepthIndexRoot, "_", ","), index)
		// }

		result = append(result, innerTx)
		if len(callTx.Calls) > 0 {
			innerTxs := convertCallsToInnerTxs(callTx.Calls, depth, depthIndexRoot, isError)
			result = append(result, innerTxs...)
		}
	}
	return result
}

// preArgsCheck validates transaction arguments
func preArgsCheck(ibs *state.IntraBlockState, arg PreArgs) error {
	if arg.From == nil {
		return fmt.Errorf("from is nil")
	}

	if arg.To == nil {
		return fmt.Errorf("to is nil")
	}

	if arg.Nonce == nil {
		return fmt.Errorf("%s, nonce is nil", arg.From.Hex())
	}

	// Check for EIP-1559 transaction fields - not supported
	if arg.MaxFeePerGas != nil || arg.MaxPriorityFeePerGas != nil {
		return fmt.Errorf("EIP-1559 transactions are not supported: maxFeePerGas and maxPriorityFeePerGas should not be set")
	}

	// Check for EIP-7702 transaction fields - not supported
	if len(arg.AuthorizationList) > 0 {
		return fmt.Errorf("EIP-7702 transactions are not supported: authorizationList should not be set")
	}

	// When obtaining state from the pending block height, the nonce must be validated
	msgFrom := *arg.From
	msgNonce := uint64(*arg.Nonce)
	stNonce := ibs.GetNonce(msgFrom)
	/*if stNonce < msgNonce {
		return fmt.Errorf("%w: address %v, tx: %d state: %d", core.ErrNonceTooHigh,
			msgFrom.Hex(), msgNonce, stNonce)
	}*/
	if stNonce > msgNonce {
		return fmt.Errorf("%w: address %v, tx: %d state: %d", core.ErrNonceTooLow,
			msgFrom.Hex(), msgNonce, stNonce)
	} else if stNonce+1 < stNonce {
		return fmt.Errorf("%w: address %v, nonce: %d", core.ErrNonceMax,
			msgFrom.Hex(), stNonce)
	}

	// skip check account
	// make sure the sender is an EOA
	/*
		codeHash := ibs.GetCodeHash(msgFrom)
		if codeHash != (common.Hash{}) && !accounts.IsEmptyCodeHash(codeHash) {
			return fmt.Errorf("%w: address %v, codehash: %s", core.ErrSenderNoEOA,
				msgFrom.Hex(), codeHash.Hex())
		}
	*/
	return nil
}
