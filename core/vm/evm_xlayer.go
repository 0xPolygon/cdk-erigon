package vm

import (
	"math/big"
	"strconv"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

const (
	CALL_TYP         = "call"
	CALLCODE_TYP     = "callcode"
	DELEGATECALL_TYP = "delegatecall"
	STATICCAL_TYP    = "staticcall"
	CREATE_TYP       = "create"
	CREATE2_TYP      = "create2"
	SUICIDE_TYP      = "suicide"
)

type InnerTxMeta struct {
	index     int
	lastDepth int
	indexMap  map[int]int
	InnerTxs  []*zktypes.InnerTx
}

func (evm *EVM) GetInnerTxMeta() *InnerTxMeta {
	return evm.innerTxMeta
}

func (evm *EVM) AddInnerTx(innerTx *zktypes.InnerTx) {
	evm.innerTxMeta.InnerTxs = append(evm.innerTxMeta.InnerTxs, innerTx)
}

func beforeOp(
	interpreter *EVMInterpreter,
	callTyp string,
	fromAddr libcommon.Address,
	toAddr *libcommon.Address,
	codeAddr *libcommon.Address,
	input []byte,
	gas uint64,
	value *big.Int) (*zktypes.InnerTx, int) {
	innerTx := &zktypes.InnerTx{
		CallType:     callTyp,
		From:         fromAddr.String(),
		ValueWei:     value.String(),
		CallValueWei: hexutil.EncodeBig(value),
		Gas:          gas,
		IsError:      false,
	}

	if toAddr != nil {
		innerTx.To = toAddr.String()
	}
	if codeAddr != nil {
		innerTx.CodeAddress = codeAddr.String()
	}

	if input != nil {
		innerTx.Input = hexutility.Encode(input)
	}

	innerTxMeta := interpreter.evm.GetInnerTxMeta()
	if innerTxMeta == nil {
		// TODO
	}
	if interpreter.Depth() == innerTxMeta.lastDepth {
		innerTxMeta.index++
		innerTxMeta.indexMap[interpreter.Depth()] = innerTxMeta.index
	} else if interpreter.Depth() < innerTxMeta.lastDepth {
		innerTxMeta.index = innerTxMeta.indexMap[interpreter.Depth()] + 1
		innerTxMeta.indexMap[interpreter.Depth()] = innerTxMeta.index
		innerTxMeta.lastDepth = interpreter.Depth()
	} else if interpreter.Depth() > innerTxMeta.lastDepth {
		innerTxMeta.index = 0
		innerTxMeta.indexMap[interpreter.Depth()] = 0
		innerTxMeta.lastDepth = interpreter.Depth()
	}
	for i := 1; i <= innerTxMeta.lastDepth; i++ {
		innerTx.Name = innerTx.Name + "_" + strconv.Itoa(innerTxMeta.indexMap[i])
	}
	innerTx.Name = innerTx.CallType + innerTx.Name
	innerTx.Dept = *big.NewInt(int64(interpreter.Depth()))
	innerTx.InternalIndex = *big.NewInt(int64(innerTxMeta.index))

	interpreter.evm.AddInnerTx(innerTx)

	newIndex := len(interpreter.evm.GetInnerTxMeta().InnerTxs) - 1
	if newIndex < 0 {
		newIndex = 0
	}

	return innerTx, newIndex
}

func afterOp(interpreter *EVMInterpreter, opType string, gas_used uint64, newIndex int, innerTx *zktypes.InnerTx, addr *libcommon.Address, err error, ret []byte) {
	innerTx.GasUsed = gas_used
	innerTx.Output = hexutility.Encode(ret[:])

	if err != nil {
		innerTxMeta := interpreter.evm.GetInnerTxMeta()
		for _, itx := range innerTxMeta.InnerTxs[newIndex:] {
			itx.IsError = true
		}
		innerTx.Error = err.Error()
	}

	switch opType {
	case CREATE_TYP, CREATE2_TYP:
		innerTx.To = addr.String()
	}
}
