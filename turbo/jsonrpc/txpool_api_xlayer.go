package jsonrpc

import (
	"context"
	"errors"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/zk/sequencer"
)

// RemoveTransaction remove the specified transaction from the sequencer
func (api *TxPoolAPIImpl) RemoveTransaction(ctx context.Context, hash common.Hash) error {
	if !sequencer.IsSequencer() {
		return errors.New("Only can remove transactions from sequencer!")
	}
	if api.rawPool == nil {
		return errors.New("The txpool module has not been initialized!")
	}
	// only the sequencer operators can call this api
	return api.rawPool.RemoveTx(hash)
}
