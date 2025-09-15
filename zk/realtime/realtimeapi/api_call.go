package realtimeapi

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	"github.com/ledgerwatch/erigon/core"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/params"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/turbo/adapter/ethapi"
	ethapi2 "github.com/ledgerwatch/erigon/turbo/adapter/ethapi"
	"github.com/ledgerwatch/erigon/turbo/transactions"
	"github.com/ledgerwatch/log/v3"
)

// Call implements the realtime eth_call.
// Executes a new message call immediately without creating a transaction on the block chain.
// Note that realtime API only supports execution on the latest block.
func (api *RealtimeAPIImpl) Call(ctx context.Context, args ethapi2.CallArgs, blockNrOrHash rpc.BlockNumberOrHash, overrides *ethapi2.StateOverrides) (hexutility.Bytes, error) {
	if api.cacheDB == nil || !api.cacheDB.ReadyFlag.Load() {
		return api.APIImpl.Call(ctx, args, blockNrOrHash, overrides)
	}

	reader, blockNumber, err := api.createStateReader(blockNrOrHash)
	if err != nil || reader == nil {
		return api.APIImpl.Call(ctx, args, blockNrOrHash, overrides)
	}

	return api.doRealtimeCall(ctx, args, overrides, blockNumber, reader)
}

func (api *RealtimeAPIImpl) doRealtimeCall(ctx context.Context, args ethapi2.CallArgs, overrides *ethapi2.StateOverrides, blockNumber uint64, reader state.StateReader) (hexutility.Bytes, error) {
	tx, err := api.APIImpl.GetDB().BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	chainConfig, err := api.APIImpl.GetChainConfig(ctx, tx)
	if err != nil {
		return nil, err
	}
	engine := api.APIImpl.GetEngine()

	if args.Gas == nil || uint64(*args.Gas) == 0 {
		args.Gas = (*hexutil.Uint64)(&api.APIImpl.GasCap)
	}

	header, _, _, ok := api.cacheDB.Stateless.GetBlockInfo(blockNumber)
	if !ok {
		return nil, fmt.Errorf("header not found for block number %d", blockNumber)
	}

	bn := rpc.BlockNumber(blockNumber)
	rpcBlockNr := rpc.BlockNumberOrHash{BlockNumber: &bn}
	result, err := transactions.DoCall(ctx, engine, args, tx, rpcBlockNr, header, overrides, api.APIImpl.GasCap, chainConfig, reader, api.cacheDB.Stateless, api.APIImpl.GetEvmCallTimeout())
	if err != nil {
		return nil, err
	}

	if len(result.ReturnData) > api.APIImpl.ReturnDataLimit {
		return nil, fmt.Errorf("call returned result on length %d exceeding --rpc.returndata.limit %d", len(result.ReturnData), api.APIImpl.ReturnDataLimit)
	}

	// If the result contains a revert reason, try to unpack and return it.
	if len(result.Revert()) > 0 {
		return nil, ethapi2.NewRevertError(result)
	}

	return result.Return(), result.Err
}

// EstimateGas implements the realtime eth_estimateGas.
// Returns an estimate of how much gas is necessary to allow the transaction to complete.
func (api *RealtimeAPIImpl) EstimateGas(ctx context.Context, argsOrNil *ethapi.CallArgs, blockNrOrHash *rpc.BlockNumberOrHash) (hexutil.Uint64, error) {
	if api.cacheDB == nil || !api.cacheDB.ReadyFlag.Load() {
		return api.APIImpl.EstimateGas(ctx, argsOrNil, blockNrOrHash)
	}

	// Parse arguments
	var args ethapi.CallArgs
	if argsOrNil != nil {
		args = *argsOrNil
	}

	// Binary search the gas requirement, as it may be higher than the amount used
	var (
		lo     = params.TxGas - 1
		hi     uint64
		gasCap uint64
	)
	// Use zero address if sender unspecified
	if args.From == nil {
		args.From = new(libcommon.Address)
	}

	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber)
	if blockNrOrHash != nil {
		bNrOrHash = *blockNrOrHash
	}
	stateReader, blockNumber, err := api.createStateReader(bNrOrHash)
	if err != nil || stateReader == nil {
		return api.APIImpl.EstimateGas(ctx, argsOrNil, blockNrOrHash)
	}
	header, _, _, ok := api.cacheDB.Stateless.GetBlockInfo(blockNumber)
	if !ok {
		return 0, fmt.Errorf("header not found for block number %d", blockNumber)
	}

	// Retrieve from rpc
	gaslimit, err := api.GetBlockGasLimit(ctx)
	if err != nil {
		return 0, err
	}

	// Determine the highest gas limit can be used during the estimation.
	if args.Gas != nil && uint64(*args.Gas) >= params.TxGas {
		hi = uint64(*args.Gas)
		if hi > gaslimit.Uint64() {
			hi = gaslimit.Uint64()
		}
	} else {
		hi = gaslimit.Uint64()
	}

	var feeCap *big.Int
	if args.GasPrice != nil && (args.MaxFeePerGas != nil || args.MaxPriorityFeePerGas != nil) {
		return 0, errors.New("both gasPrice and (maxFeePerGas or maxPriorityFeePerGas) specified")
	} else if args.GasPrice != nil {
		feeCap = args.GasPrice.ToInt()
	} else if args.MaxFeePerGas != nil {
		feeCap = args.MaxFeePerGas.ToInt()
	} else {
		feeCap = libcommon.Big0
	}
	// Recap the highest gas limit with account's available balance.
	if feeCap.Sign() != 0 {
		account, err := stateReader.ReadAccountData(*args.From) // from cannot be nil
		if err != nil || account == nil {
			return 0, err
		}

		balance := &account.Balance
		available := balance.ToBig()
		if args.Value != nil {
			if args.Value.ToInt().Cmp(available) >= 0 {
				return 0, errors.New("insufficient funds for transfer")
			}
			available.Sub(available, args.Value.ToInt())
		}
		allowance := new(big.Int).Div(available, feeCap)

		// If the allowance is larger than maximum uint64, skip checking
		if allowance.IsUint64() && hi > allowance.Uint64() {
			transfer := args.Value
			if transfer == nil {
				transfer = new(hexutil.Big)
			}
			log.Debug("Gas estimation capped by limited funds", "original", hi, "balance", balance,
				"sent", transfer.ToInt(), "maxFeePerGas", feeCap, "fundable", allowance)
			hi = allowance.Uint64()
		}
	}

	// Recap the highest gas allowance with specified gascap.
	if hi > api.GasCap {
		log.Debug("Caller gas above allowance, capping", "requested", hi, "cap", api.GasCap)
		hi = api.GasCap
	}
	gasCap = hi

	tx, err := api.APIImpl.GetDB().BeginRo(ctx)
	if err != nil {
		return api.APIImpl.EstimateGas(ctx, &args, nil)
	}
	defer tx.Rollback()

	chainConfig, err := api.APIImpl.GetChainConfig(ctx, tx)
	if err != nil {
		return api.APIImpl.EstimateGas(ctx, &args, nil)
	}
	engine := api.APIImpl.GetEngine()

	caller, err := NewRealtimeReusableCaller(engine, stateReader, nil, header, args, api.GasCap, bNrOrHash, tx, api.cacheDB.Stateless, chainConfig, api.APIImpl.GetEvmCallTimeout())
	if err != nil {
		return 0, err
	}

	// Create a helper to check if a gas allowance results in an executable transaction
	executable := func(gas uint64) (bool, *core.ExecutionResult, error) {
		result, err := caller.DoCallWithNewGas(ctx, gas)
		if err != nil {
			if errors.Is(err, core.ErrIntrinsicGas) {
				// Special case, raise gas limit
				return true, nil, nil
			}

			// Bail out
			return true, nil, err
		}

		return result.Failed(), result, nil
	}

	// Execute the binary search and hone in on an executable gas limit
	for lo+1 < hi {
		mid := (hi + lo) / 2
		failed, _, err := executable(mid)
		// If the error is not nil(consensus error), it means the provided message
		// call or transaction will never be accepted no matter how much gas it is
		// assigened. Return the error directly, don't struggle any more.
		if err != nil {
			return 0, err
		}
		if failed {
			lo = mid
		} else {
			hi = mid
		}
	}

	// Reject the transaction as invalid if it still fails at the highest allowance
	if hi == gasCap {
		failed, result, err := executable(hi)
		if err != nil {
			return 0, err
		}
		if failed {
			if result != nil && !errors.Is(result.Err, vm.ErrOutOfGas) {
				if len(result.Revert()) > 0 {
					return 0, ethapi2.NewRevertError(result)
				}
				return 0, result.Err
			}
			// Otherwise, the specified gas cap is too low
			return 0, fmt.Errorf("gas required exceeds allowance (%d)", gasCap)
		}
	}
	return hexutil.Uint64(hi), nil
}
