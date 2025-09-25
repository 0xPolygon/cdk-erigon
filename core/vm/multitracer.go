package vm

import (
	libcommon "github.com/erigontech/erigon-lib/common"
	"github.com/holiman/uint256"

	"github.com/erigontech/erigon/core/types"
)

// multiTracer fans out every tracer callback to all configured loggers.
type multiTracer struct {
	loggers []EVMLogger
}

// NewMultiTracer composes any number of tracers into a single vm.EVMLogger.
// Nil tracers are skipped; if the result would be empty, nil is returned.
func NewMultiTracer(loggers ...EVMLogger) EVMLogger {
	var filtered []EVMLogger
	for _, l := range loggers {
		if l != nil {
			filtered = append(filtered, l)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return &multiTracer{loggers: filtered}
}

func (t *multiTracer) CaptureTxStart(gasLimit uint64) {
	for _, l := range t.loggers {
		l.CaptureTxStart(gasLimit)
	}
}

func (t *multiTracer) CaptureTxEnd(restGas uint64) {
	for _, l := range t.loggers {
		l.CaptureTxEnd(restGas)
	}
}

func (t *multiTracer) CaptureStart(env *EVM, from libcommon.Address, to libcommon.Address, precompile bool, create bool, input []byte, gas uint64, value *uint256.Int, code []byte) {
	for _, l := range t.loggers {
		l.CaptureStart(env, from, to, precompile, create, input, gas, value, code)
	}
}

func (t *multiTracer) CaptureEnd(output []byte, usedGas uint64, err error) {
	for _, l := range t.loggers {
		l.CaptureEnd(output, usedGas, err)
	}
}

func (t *multiTracer) CaptureEnter(typ OpCode, from libcommon.Address, to libcommon.Address, precompile bool, create bool, input []byte, gas uint64, value *uint256.Int, code []byte) {
	for _, l := range t.loggers {
		l.CaptureEnter(typ, from, to, precompile, create, input, gas, value, code)
	}
}

func (t *multiTracer) CaptureExit(output []byte, usedGas uint64, err error) {
	for _, l := range t.loggers {
		l.CaptureExit(output, usedGas, err)
	}
}

func (t *multiTracer) CaptureState(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error) {
	for _, l := range t.loggers {
		l.CaptureState(pc, op, gas, cost, scope, rData, depth, err)
	}
}

func (t *multiTracer) CaptureFault(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, depth int, err error) {
	for _, l := range t.loggers {
		l.CaptureFault(pc, op, gas, cost, scope, depth, err)
	}
}

func (t *multiTracer) Flush(tx types.Transaction) {
	for _, l := range t.loggers {
		if f, ok := l.(FlushableTracer); ok {
			f.Flush(tx)
		}
	}
}
