package legacy_executor_verifier

import (
	"context"
	"strconv"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/core/rawdb"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/erigon/zk/smt"
)

func minUint64(a, b uint64) uint64 {
	if a <= b {
		return a
	}
	return b
}

func (v *LegacyExecutorVerifier) SetSmtCache(cache *smt.SmtCache) {
	v.cache = cache
}

func (v *LegacyExecutorVerifier) VerifyWithMockExecutor(request *VerifierRequest) *Promise[*VerifierBundle] {
	// eager promise will do the work as soon as called in a goroutine, then we can retrieve the result later
	// ProcessResultsSequentiallyUnsafe relies on the fact that this function returns ALWAYS non-verifierBundle and error. The only exception is the case when verifications has been canceled. Only then the verifierBundle can be nil
	return NewPromise[*VerifierBundle](func() (*VerifierBundle, error) {
		verifierBundle := NewVerifierBundle(request, nil, false)
		blockNumbers := verifierBundle.Request.BlockNumbers

		var err error
		ctx := context.Background()
		// mapmutation has some issue with us not having a quit channel on the context call to `Done` so
		// here we're creating a cancelable context and just deferring the cancel
		innerCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		tx, err := v.db.BeginRo(innerCtx)
		if err != nil {
			return verifierBundle, err
		}
		defer tx.Rollback()

		// For X Layer, split db and ac
		var txsmt kv.Tx = nil
		if v.dbsmt != nil {
			txsmt, err = v.dbsmt.BeginRo(innerCtx)
			if err != nil {
				return verifierBundle, err
			}
			defer txsmt.Rollback()
		}

		hermezDb := hermez_db.NewHermezDbReader(tx)

		l1InfoTreeMinTimestamps := make(map[uint64]uint64)
		streamBytes, err := v.GetWholeBatchStreamBytes(request.BatchNumber, tx, blockNumbers, hermezDb, l1InfoTreeMinTimestamps, nil)
		if err != nil {
			return verifierBundle, err
		}

		// now we need to figure out the timestamp limit for this payload.  It must be:
		// timestampLimit >= currentTimestamp (from batch pre-state) + deltaTimestamp
		// so to ensure we have a good value we can take the timestamp of the last block in the batch
		// and just add 5 minutes
		lastBlock, err := rawdb.ReadBlockByNumber(tx, blockNumbers[len(blockNumbers)-1])
		if err != nil {
			return verifierBundle, err
		}

		// executor is perfectly happy with just an empty hash here
		oldAccInputHash := common.HexToHash("0x0")
		timestampLimit := lastBlock.Time()
		_ = &Payload{
			Witness:                 nil,
			DataStream:              streamBytes,
			Coinbase:                v.cfg.AddressSequencer.String(),
			OldAccInputHash:         oldAccInputHash.Bytes(),
			L1InfoRoot:              nil,
			TimestampLimit:          timestampLimit,
			ForcedBlockhashL1:       []byte{0},
			ContextId:               strconv.FormatUint(request.BatchNumber, 10),
			L1InfoTreeMinTimestamps: l1InfoTreeMinTimestamps,
		}

		_, err = rawdb.ReadBlockByNumber(tx, blockNumbers[0]-1)
		if err != nil {
			return verifierBundle, err
		}

		verifierBundle.markAsreadyForSendingRequest()
		verifierBundle.Response = &VerifierResponse{
			Valid:            true,
			OriginalCounters: request.Counters,
			Witness:          nil,
			ExecutorResponse: nil,
			Error:            nil,
		}
		return verifierBundle, nil
	})
}
