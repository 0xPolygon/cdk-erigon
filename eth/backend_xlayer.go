package eth

import (
	"context"
	"slices"

	"github.com/ledgerwatch/erigon/cmd/utils"
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
			if slices.Contains(ethCfg.XLayer.ApolloChanged, utils.SequencerBatchSealTime.Name) {
				cfg.Zk.SequencerBatchSealTime = ethCfg.Zk.SequencerBatchSealTime
			}
			if slices.Contains(ethCfg.XLayer.ApolloChanged, utils.SequencerBlockSealTime.Name) {
				cfg.Zk.SequencerBlockSealTime = ethCfg.Zk.SequencerBlockSealTime
			}
		case <-ctx.Done():
			return
		}
	}
}
