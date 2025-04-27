package stages

import (
	"context"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/eth/stagedsync"
)

// This stages does NOTHING while going forward, because its done during execution
// Even this stage progress is updated in execution stage
func SpawnSequencerInterhashesStage(
	s *stagedsync.StageState,
	u stagedsync.Unwinder,
	tx kv.RwTx,
	ctx context.Context,
	cfg ZkInterHashesCfg,
	quiet bool,
) error {
	return nil
}

// The unwind of interhashes must happen separate from execution’s unwind although execution includes interhashes while going forward.
// This is because interhashes MUST be unwound before history/calltraces while execution MUST be after history/calltraces
func UnwindSequencerInterhashsStage(
	u *stagedsync.UnwindState,
	s *stagedsync.StageState,
	tx kv.RwTx,
	txsmt kv.RwTx,
	ctx context.Context,
	cfg ZkInterHashesCfg,
) error {
	// For X Layer, split db and ac
	return UnwindZkIntermediateHashesStage(u, s, tx, txsmt, cfg, ctx, false)
}

func PruneSequencerInterhashesStage(
	s *stagedsync.PruneState,
	tx kv.RwTx,
	cfg ZkInterHashesCfg,
	ctx context.Context,
) error {
	return nil
}
