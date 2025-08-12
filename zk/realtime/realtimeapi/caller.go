package realtimeapi

import (
	"context"
	"fmt"
	"time"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/chain"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/consensus"
	"github.com/ledgerwatch/erigon/core"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/rpc"
	ethapi2 "github.com/ledgerwatch/erigon/turbo/adapter/ethapi"
	"github.com/ledgerwatch/erigon/turbo/services"
	"github.com/ledgerwatch/erigon/turbo/transactions"
)

type RealtimeReusableCaller struct {
	evm         *vm.EVM
	gasCap      uint64
	baseFee     *uint256.Int
	stateReader state.StateReader
	callTimeout time.Duration
	message     *types.Message
}

func NewRealtimeReusableCaller(
	engine consensus.EngineReader,
	stateReader state.StateReader,
	overrides *ethapi2.StateOverrides,
	header *types.Header,
	initialArgs ethapi2.CallArgs,
	gasCap uint64,
	blockNrOrHash rpc.BlockNumberOrHash,
	tx kv.Tx,
	headerReader services.HeaderReader,
	chainConfig *chain.Config,
	callTimeout time.Duration,
) (*RealtimeReusableCaller, error) {
	ibs := state.New(stateReader)

	if overrides != nil {
		if err := overrides.Override(ibs); err != nil {
			return nil, err
		}
	}

	var baseFee *uint256.Int
	if header != nil && header.BaseFee != nil {
		var overflow bool
		baseFee, overflow = uint256.FromBig(header.BaseFee)
		if overflow {
			return nil, fmt.Errorf("header.BaseFee uint256 overflow")
		}
	}

	msg, err := initialArgs.ToMessage(gasCap, baseFee)
	if err != nil {
		return nil, err
	}

	blockCtx := transactions.NewEVMBlockContext(engine, header, blockNrOrHash.RequireCanonical, tx, headerReader)
	txCtx := core.NewEVMTxContext(msg)

	to := msg.To()
	if to == nil {
		to = &libcommon.Address{}
	}

	evm := vm.NewEVM(blockCtx, txCtx, ibs, chainConfig, vm.Config{NoBaseFee: true})
	return &RealtimeReusableCaller{
		evm:         evm,
		gasCap:      gasCap,
		baseFee:     baseFee,
		stateReader: stateReader,
		callTimeout: callTimeout,
		message:     &msg,
	}, nil
}

func (r *RealtimeReusableCaller) DoCallWithNewGas(
	ctx context.Context,
	newGas uint64,
) (*core.ExecutionResult, error) {
	var cancel context.CancelFunc
	if r.callTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, r.callTimeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	// Make sure the context is cancelled when the call has completed
	// this makes sure resources are cleaned up.
	defer cancel()

	r.message.ChangeGas(r.gasCap, newGas)

	// reset the EVM so that we can continue to use it with the new context
	txCtx := core.NewEVMTxContext(r.message)
	ibs := state.New(r.stateReader)
	r.evm.Reset(txCtx, ibs)

	timedOut := false
	go func() {
		<-ctx.Done()
		timedOut = true
	}()

	gp := new(core.GasPool).AddGas(r.message.Gas()).AddBlobGas(r.message.BlobGas())

	result, err := core.ApplyMessage(r.evm, r.message, gp, true /* refunds */, false /* gasBailout */)
	if err != nil {
		return nil, err
	}

	// If the timer caused an abort, return an appropriate error message
	if timedOut {
		return nil, fmt.Errorf("execution aborted (timeout = %v)", r.callTimeout)
	}

	return result, nil
}
