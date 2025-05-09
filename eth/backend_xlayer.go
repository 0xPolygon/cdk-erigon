package eth

import (
	"context"
	"fmt"
	"slices"

	"github.com/ledgerwatch/erigon/cmd/utils"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/smt/pkg/blockinfo"
	"github.com/ledgerwatch/erigon/zk/apollo"
	"github.com/ledgerwatch/erigon/zkevm/log"
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
			if slices.Contains(ethCfg.XLayer.ApolloChanged, utils.BlockInfoConcurrent.Name) {
				blockinfo.SetUseBlockInfoTree(ethCfg.Zk.XLayer.BlockInfoConcurrent)
			}
			if slices.Contains(ethCfg.XLayer.ApolloChanged, utils.SequencerBatchCounterPercentage.Name) {
				vm.SetBatchCounterLimitPercentage(ethCfg.Zk.XLayer.SequencerBatchCounterPercentage)
			}
			if slices.Contains(ethCfg.XLayer.ApolloChanged, utils.SequencerMaxBlockSealTime.Name) {
				if cfg.Zk.SequencerBlockSealTime > ethCfg.XLayer.SequencerMaxBlockSealTime || cfg.Zk.SequencerBatchSealTime > ethCfg.XLayer.SequencerMaxBlockSealTime {
					log.Warn(fmt.Sprintf("Got error: sequencer-block-seal-time: %s, sequencer-max-block-seal-time: %s, sequencer-batch-seal-time: %s"), cfg.Zk.SequencerBlockSealTime, ethCfg.XLayer.SequencerMaxBlockSealTime, cfg.Zk.SequencerBatchSealTime)
				} else {
					cfg.Zk.XLayer.SequencerMaxBlockSealTime = ethCfg.XLayer.SequencerMaxBlockSealTime
				}
			}

		case <-ctx.Done():
			return
		}
	}
}
