package eth

import (
	"context"

	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/zk/apollo"
)

func listenApollo(ctx context.Context, cfg *ethconfig.Config) {
	stream := apollo.GetEthConfigStream()
	ch, remove := stream.Sub()
	defer remove()

	for {
		select {
		case ethCfg := <-ch:
			if ethCfg == nil {
				continue
			}
			if cfg.Zk.SequencerBatchSealTime != ethCfg.Zk.SequencerBatchSealTime {
				cfg.Zk.SequencerBatchSealTime = ethCfg.Zk.SequencerBatchSealTime
			}
			if cfg.Zk.SequencerBlockSealTime != ethCfg.Zk.SequencerBlockSealTime {
				cfg.Zk.SequencerBlockSealTime = ethCfg.Zk.SequencerBlockSealTime
			}
		case <-ctx.Done():
			return
		}
	}
}
