package mdbx

import (
	"fmt"

	"github.com/ledgerwatch/log/v3"
)

func (opts MdbxOpts) debug() {
	log.Info(fmt.Sprintf(
		"MdbxOpts: Path=%s, SyncPeriod=%s, MapSize=%s, GrowthStep=%s, ShrinkThreshold=%d, Flags=%d, PageSize=%d, DirtySpace=%d, MergeThreshold=%d, Verbosity=%v, Label=%v, InMem=%v",
		opts.path, opts.syncPeriod, opts.mapSize.String(), opts.growthStep.String(),
		opts.shrinkThreshold, opts.flags, opts.pageSize, opts.dirtySpace, opts.mergeThreshold,
		opts.verbosity, opts.label, opts.inMem,
	))
}
