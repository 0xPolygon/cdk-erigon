package eth

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ledgerwatch/erigon/cmd/utils"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/smt/pkg/blockinfo"
	"github.com/ledgerwatch/erigon/zk/apollo"
	"github.com/ledgerwatch/erigon/zkevm/log"
)

func (s *Ethereum) listenApollo(ctx context.Context, cfg *ethconfig.Config) {
	stream := apollo.GetEthConfigStream()
	ch, remove := stream.Sub()
	defer remove()

	for {
		select {
		case ethCfg := <-ch:
			var l1SyncerConfigChanged = false
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
					log.Warn(fmt.Sprintf("Got error: sequencer-block-seal-time: %s, sequencer-max-block-seal-time: %s, sequencer-batch-seal-time: %s", cfg.Zk.SequencerBlockSealTime, ethCfg.XLayer.SequencerMaxBlockSealTime, cfg.Zk.SequencerBatchSealTime))
				} else {
					cfg.Zk.XLayer.SequencerMaxBlockSealTime = ethCfg.XLayer.SequencerMaxBlockSealTime
				}
			}
			if slices.Contains(ethCfg.XLayer.ApolloChanged, utils.GetLogsTimeout.Name) {
				cfg.Zk.XLayer.GetLogsTimeout = ethCfg.XLayer.GetLogsTimeout
				l1SyncerConfigChanged = true
			}
			if slices.Contains(ethCfg.XLayer.ApolloChanged, utils.GetLogsRetries.Name) {
				cfg.Zk.XLayer.GetLogsRetries = ethCfg.XLayer.GetLogsRetries
				l1SyncerConfigChanged = true
			}
			if l1SyncerConfigChanged {
				s.updateAllL1Syncer(cfg.Zk.XLayer.GetLogsTimeout, cfg.Zk.XLayer.GetLogsRetries)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *Ethereum) updateAllL1Syncer(getLogsTimeout time.Duration, getLogsRetries int) {
	if s.seqVerSyncer != nil {
		s.seqVerSyncer.UpdateConfig(getLogsTimeout, getLogsRetries)
	}
	if s.l1Syncer != nil {
		s.l1Syncer.UpdateConfig(getLogsTimeout, getLogsRetries)
	}
	if s.l1InfoTreeSyncer != nil {
		s.l1InfoTreeSyncer.UpdateConfig(getLogsTimeout, getLogsRetries)
	}
	if s.l1BlockSyncer != nil {
		s.l1BlockSyncer.UpdateConfig(getLogsTimeout, getLogsRetries)
	}
}
