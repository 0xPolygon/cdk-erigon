package stages

import (
	"context"
	"github.com/erigontech/erigon/zk/l1infotree"
	"github.com/iden3/go-iden3-crypto/keccak256"
	"github.com/stretchr/testify/assert"
	"math/big"
	"testing"
	"time"

	ethereum "github.com/erigontech/erigon"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/kv/memdb"
	"github.com/erigontech/erigon/core/rawdb"
	"github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/eth/ethconfig"
	"github.com/erigontech/erigon/eth/stagedsync"
	"github.com/erigontech/erigon/eth/stagedsync/stages"
	"github.com/erigontech/erigon/smt/pkg/db"
	"github.com/erigontech/erigon/zk/contracts"
	"github.com/erigontech/erigon/zk/hermez_db"
	"github.com/erigontech/erigon/zk/syncer"
	"github.com/erigontech/erigon/zk/syncer/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/erigontech/erigon/rpc"
)

func TestSpawnStageL1Syncer(t *testing.T) {
	// Arrange
	ctx, db1 := context.Background(), memdb.NewTestDB(t)
	tx := memdb.BeginRw(t, db1)
	err := hermez_db.CreateHermezBuckets(tx)
	require.NoError(t, err)

	err = db.CreateEriDbBuckets(tx)
	require.NoError(t, err)

	_, db2 := context.Background(), memdb.NewTestDB(t)

	l1FirstBlock := big.NewInt(20)
	l2BlockNumber := uint64(10)
	verifiedBatchNumber := uint64(2)

	hDB := hermez_db.NewHermezDb(tx)
	err = hDB.WriteBlockBatch(0, 0)
	require.NoError(t, err)
	err = hDB.WriteBlockBatch(l2BlockNumber-1, verifiedBatchNumber-1)
	require.NoError(t, err)
	err = hDB.WriteBlockBatch(l2BlockNumber, verifiedBatchNumber)
	require.NoError(t, err)
	err = stages.SaveStageProgress(tx, stages.L1Syncer, 0)
	require.NoError(t, err)
	err = stages.SaveStageProgress(tx, stages.IntermediateHashes, l2BlockNumber-1)
	require.NoError(t, err)

	err = hDB.WriteVerification(l1FirstBlock.Uint64(), verifiedBatchNumber-1, common.HexToHash("0x1"), common.HexToHash("0x99990"))
	require.NoError(t, err)
	err = hDB.WriteVerification(l1FirstBlock.Uint64(), verifiedBatchNumber, common.HexToHash("0x2"), common.HexToHash("0x99999"))
	require.NoError(t, err)

	genesisHeader := &types.Header{
		Number:      big.NewInt(0).SetUint64(l2BlockNumber - 1),
		Time:        0,
		Difficulty:  big.NewInt(1),
		GasLimit:    8000000,
		GasUsed:     0,
		ParentHash:  common.HexToHash("0x1"),
		TxHash:      common.HexToHash("0x2"),
		ReceiptHash: common.HexToHash("0x3"),
		Root:        common.HexToHash("0x99990"),
	}

	txs := []types.Transaction{}
	uncles := []*types.Header{}
	receipts := []*types.Receipt{}
	withdrawals := []*types.Withdrawal{}

	genesisBlock := types.NewBlock(genesisHeader, txs, uncles, receipts, withdrawals)

	err = rawdb.WriteBlock(tx, genesisBlock)
	require.NoError(t, err)
	err = rawdb.WriteCanonicalHash(tx, genesisBlock.Hash(), genesisBlock.NumberU64())
	require.NoError(t, err)

	s := &stagedsync.StageState{ID: stages.L1Syncer, BlockNumber: 0}
	u := &stagedsync.Sync{}

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	EthermanMock := mocks.NewMockIEtherman(mockCtrl)

	l1ContractAddresses := []common.Address{
		common.HexToAddress("0x1"),
		common.HexToAddress("0x2"),
		common.HexToAddress("0x3"),
	}
	l1ContractTopics := [][]common.Hash{
		[]common.Hash{common.HexToHash("0x1")},
		[]common.Hash{common.HexToHash("0x2")},
		[]common.Hash{common.HexToHash("0x3")},
	}

	latestBlockParentHash := common.HexToHash("0x123456789")
	latestBlockTime := uint64(time.Now().Unix())
	latestBlockNumber := big.NewInt(21)
	latestBlockHeader := &types.Header{ParentHash: latestBlockParentHash, Number: latestBlockNumber, Time: latestBlockTime}
	latestBlock := types.NewBlockWithHeader(latestBlockHeader)

	EthermanMock.EXPECT().BlockByNumber(gomock.Any(), nil).Return(latestBlock, nil).AnyTimes()

	filterQuery := ethereum.FilterQuery{
		FromBlock: l1FirstBlock,
		ToBlock:   latestBlockNumber,
		Addresses: l1ContractAddresses,
		Topics:    l1ContractTopics,
	}

	const rollupID = uint64(1)

	type testCase struct {
		name   string
		getLog func(hDB *hermez_db.HermezDb) (types.Log, error)
		assert func(t *testing.T, hDB *hermez_db.HermezDb)
	}

	testCases := []testCase{
		{
			name: "SequencedBatchTopicPreEtrog",
			getLog: func(hDB *hermez_db.HermezDb) (types.Log, error) {
				batchNum := uint64(1)
				batchNumHash := common.BytesToHash(big.NewInt(0).SetUint64(batchNum).Bytes())
				txHash := common.HexToHash("0x1")
				return types.Log{
					BlockNumber: latestBlockNumber.Uint64(),
					Address:     l1ContractAddresses[0],
					Topics:      []common.Hash{contracts.SequenceBatchesTopicPreEtrog, batchNumHash},
					TxHash:      txHash,
					Data:        []byte{},
				}, nil
			},
			assert: func(t *testing.T, hDB *hermez_db.HermezDb) {
				l1BatchInfo, err := hDB.GetSequenceByBatchNo(1)
				require.NoError(t, err)

				require.Equal(t, l1BatchInfo.BatchNo, uint64(1))
				require.Equal(t, l1BatchInfo.L1BlockNo, latestBlockNumber.Uint64())
				require.Equal(t, l1BatchInfo.L1TxHash.String(), common.HexToHash("0x1").String())
				require.Equal(t, l1BatchInfo.StateRoot.String(), common.Hash{}.String())
				require.Equal(t, l1BatchInfo.L1InfoRoot.String(), common.Hash{}.String())
			},
		},
		{
			name: "SequencedBatchTopicEtrog",
			getLog: func(hDB *hermez_db.HermezDb) (types.Log, error) {
				batchNum := uint64(2)
				batchNumHash := common.BytesToHash(big.NewInt(0).SetUint64(batchNum).Bytes())
				txHash := common.HexToHash("0x2")
				l1InfoRoot := common.HexToHash("0x3")
				return types.Log{
					BlockNumber: latestBlockNumber.Uint64(),
					Address:     l1ContractAddresses[0],
					Topics:      []common.Hash{contracts.SequenceBatchesTopicEtrog, batchNumHash},
					Data:        l1InfoRoot.Bytes(),
					TxHash:      txHash,
				}, nil
			},
			assert: func(t *testing.T, hDB *hermez_db.HermezDb) {
				l1BatchInfo, err := hDB.GetSequenceByBatchNo(2)
				require.NoError(t, err)

				require.Equal(t, l1BatchInfo.BatchNo, uint64(2))
				require.Equal(t, l1BatchInfo.L1BlockNo, latestBlockNumber.Uint64())
				require.Equal(t, l1BatchInfo.L1TxHash.String(), common.HexToHash("0x2").String())
				require.Equal(t, l1BatchInfo.StateRoot.String(), common.Hash{}.String())
				require.Equal(t, l1BatchInfo.L1InfoRoot.String(), common.HexToHash("0x3").String())
			},
		},
		{
			name: "VerificationTopicPreEtrog",
			getLog: func(hDB *hermez_db.HermezDb) (types.Log, error) {
				batchNum := uint64(3)
				batchNumHash := common.BytesToHash(big.NewInt(0).SetUint64(batchNum).Bytes())
				txHash := common.HexToHash("0x4")
				stateRoot := common.HexToHash("0x5")
				return types.Log{
					BlockNumber: latestBlockNumber.Uint64(),
					Address:     l1ContractAddresses[0],
					Topics:      []common.Hash{contracts.VerificationTopicPreEtrog, batchNumHash},
					Data:        stateRoot.Bytes(),
					TxHash:      txHash,
				}, nil
			},
			assert: func(t *testing.T, hDB *hermez_db.HermezDb) {
				l1BatchInfo, err := hDB.GetVerificationByBatchNo(3)
				require.NoError(t, err)

				require.Equal(t, l1BatchInfo.BatchNo, uint64(3))
				require.Equal(t, l1BatchInfo.L1BlockNo, latestBlockNumber.Uint64())
				require.Equal(t, l1BatchInfo.L1TxHash.String(), common.HexToHash("0x4").String())
				require.Equal(t, l1BatchInfo.StateRoot.String(), common.HexToHash("0x5").String())
				require.Equal(t, l1BatchInfo.L1InfoRoot.String(), common.Hash{}.String())
			},
		},
		{
			name: "VerificationValidiumTopicEtrog",
			getLog: func(hDB *hermez_db.HermezDb) (types.Log, error) {
				batchNum := uint64(4)
				batchNumHash := common.BytesToHash(big.NewInt(0).SetUint64(batchNum).Bytes())
				txHash := common.HexToHash("0x4")
				stateRoot := common.HexToHash("0x5")
				return types.Log{
					BlockNumber: latestBlockNumber.Uint64(),
					Address:     l1ContractAddresses[0],
					Topics:      []common.Hash{contracts.VerificationValidiumTopicEtrog, batchNumHash},
					Data:        stateRoot.Bytes(),
					TxHash:      txHash,
				}, nil
			},
			assert: func(t *testing.T, hDB *hermez_db.HermezDb) {
				l1BatchInfo, err := hDB.GetVerificationByBatchNo(4)
				require.NoError(t, err)

				require.Equal(t, l1BatchInfo.BatchNo, uint64(4))
				require.Equal(t, l1BatchInfo.L1BlockNo, latestBlockNumber.Uint64())
				require.Equal(t, l1BatchInfo.L1TxHash.String(), common.HexToHash("0x4").String())
				require.Equal(t, l1BatchInfo.StateRoot.String(), common.HexToHash("0x5").String())
				require.Equal(t, l1BatchInfo.L1InfoRoot.String(), common.Hash{}.String())
			},
		},
		{
			name: "VerificationTopicEtrog",
			getLog: func(hDB *hermez_db.HermezDb) (types.Log, error) {
				rollupIDHash := common.BytesToHash(big.NewInt(0).SetUint64(rollupID).Bytes())
				batchNum := uint64(5)
				batchNumHash := common.BytesToHash(big.NewInt(0).SetUint64(batchNum).Bytes())
				txHash := common.HexToHash("0x6")
				stateRoot := common.HexToHash("0x7")
				data := append(batchNumHash.Bytes(), stateRoot.Bytes()...)
				return types.Log{
					BlockNumber: latestBlockNumber.Uint64(),
					Address:     l1ContractAddresses[0],
					Topics:      []common.Hash{contracts.VerificationTopicEtrog, rollupIDHash},
					Data:        data,
					TxHash:      txHash,
				}, nil
			},
			assert: func(t *testing.T, hDB *hermez_db.HermezDb) {
				l1BatchInfo, err := hDB.GetVerificationByBatchNo(5)
				require.NoError(t, err)

				require.Equal(t, l1BatchInfo.BatchNo, uint64(5))
				require.Equal(t, l1BatchInfo.L1BlockNo, latestBlockNumber.Uint64())
				require.Equal(t, l1BatchInfo.L1TxHash.String(), common.HexToHash("0x6").String())
				require.Equal(t, l1BatchInfo.StateRoot.String(), common.HexToHash("0x7").String())
				require.Equal(t, l1BatchInfo.L1InfoRoot.String(), common.Hash{}.String())
			},
		},
		{
			name: "RollbackBatchesTopic",
			getLog: func(hDB *hermez_db.HermezDb) (types.Log, error) {
				blockNum := uint64(10)
				batchNum := uint64(20)
				batchNumHash := common.BytesToHash(big.NewInt(0).SetUint64(batchNum).Bytes())
				txHash := common.HexToHash("0x888")
				stateRoot := common.HexToHash("0x999")
				l1InfoRoot := common.HexToHash("0x101010")

				for i := uint64(15); i <= uint64(25); i++ {
					err := hDB.WriteSequence(blockNum, i, txHash, stateRoot, l1InfoRoot)
					require.NoError(t, err)
				}

				return types.Log{
					BlockNumber: latestBlockNumber.Uint64(),
					Address:     l1ContractAddresses[0],
					Topics:      []common.Hash{contracts.RollbackBatchesTopic, batchNumHash},
					TxHash:      txHash,
				}, nil
			},
			assert: func(t *testing.T, hDB *hermez_db.HermezDb) {
				for i := uint64(15); i <= uint64(20); i++ {
					l1BatchInfo, err := hDB.GetSequenceByBatchNo(i)
					require.NotNil(t, l1BatchInfo)
					require.NoError(t, err)
				}
				for i := uint64(21); i <= uint64(25); i++ {
					l1BatchInfo, err := hDB.GetSequenceByBatchNo(i)
					require.Nil(t, l1BatchInfo)
					require.NoError(t, err)
				}
			},
		},
	}

	filteredLogs := []types.Log{}
	for _, tc := range testCases {
		ll, err := tc.getLog(hDB)
		require.NoError(t, err)
		filteredLogs = append(filteredLogs, ll)
	}

	EthermanMock.EXPECT().FilterLogs(gomock.Any(), filterQuery).Return(filteredLogs, nil).AnyTimes()

	l1Syncer := syncer.NewL1Syncer(ctx, db2, []syncer.IEtherman{EthermanMock}, l1ContractAddresses, l1ContractTopics, 10, 0, "latest")

	updater := l1infotree.NewUpdater(&ethconfig.Zk{}, l1Syncer, l1infotree.NewInfoTreeL2RpcSyncer(ctx, &ethconfig.Zk{}))

	zkCfg := &ethconfig.Zk{
		L1RollupId:   rollupID,
		L1FirstBlock: l1FirstBlock.Uint64(),
	}
	cfg := StageL1CombinedSyncerCfg(db1, l1Syncer, zkCfg, updater)

	// Act
	err = SpawnStageL1CombinedSyncer(s, u, ctx, tx, cfg, false)
	require.NoError(t, err)

	// Assert
	for _, tc := range testCases {
		tc.assert(t, hDB)
	}

	progress, err := stages.GetStageProgress(tx, stages.L1CombinedSyncer)
	assert.Equal(t, latestBlockNumber.Uint64(), progress)
}

func TestSpawnL1SequencerSyncStage(t *testing.T) {
	// arrange
	ctx, db1 := context.Background(), memdb.NewTestDB(t)
	tx := memdb.BeginRw(t, db1)
	err := hermez_db.CreateHermezBuckets(tx)
	require.NoError(t, err)
	err = db.CreateEriDbBuckets(tx)
	require.NoError(t, err)

	_, db2 := context.Background(), memdb.NewTestDB(t)

	hDB := hermez_db.NewHermezDb(tx)
	err = hDB.WriteBlockBatch(0, 0)
	require.NoError(t, err)
	err = stages.SaveStageProgress(tx, stages.L1SequencerSync, 0)
	require.NoError(t, err)

	s := &stagedsync.StageState{ID: stages.L1SequencerSync, BlockNumber: 0}
	u := &stagedsync.Sync{}

	// mocks
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	EthermanMock := mocks.NewMockIEtherman(mockCtrl)

	l1ContractAddresses := []common.Address{
		common.HexToAddress("0x1"),
		common.HexToAddress("0x2"),
		common.HexToAddress("0x3"),
	}
	l1ContractTopics := [][]common.Hash{
		[]common.Hash{common.HexToHash("0x1")},
		[]common.Hash{common.HexToHash("0x2")},
		[]common.Hash{common.HexToHash("0x3")},
	}

	l1FirstBlock := big.NewInt(20)

	finalizedBlockParentHash := common.HexToHash("0x123456789")
	finalizedBlockTime := uint64(time.Now().Unix())
	finalizedBlockNumber := big.NewInt(21)
	finalizedBlockHeader := &types.Header{ParentHash: finalizedBlockParentHash, Number: finalizedBlockNumber, Time: finalizedBlockTime}
	finalizedBlock := types.NewBlockWithHeader(finalizedBlockHeader)

	latestBlockParentHash := finalizedBlock.Hash()
	latestBlockTime := uint64(time.Now().Unix())
	latestBlockNumber := big.NewInt(22)
	latestBlockHeader := &types.Header{ParentHash: latestBlockParentHash, Number: latestBlockNumber, Time: latestBlockTime}
	latestBlock := types.NewBlockWithHeader(latestBlockHeader)

	EthermanMock.EXPECT().HeaderByNumber(gomock.Any(), finalizedBlockNumber).Return(finalizedBlockHeader, nil).AnyTimes()
	EthermanMock.EXPECT().BlockByNumber(gomock.Any(), big.NewInt(rpc.FinalizedBlockNumber.Int64())).Return(finalizedBlock, nil).AnyTimes()
	EthermanMock.EXPECT().HeaderByNumber(gomock.Any(), latestBlockNumber).Return(latestBlockHeader, nil).AnyTimes()
	EthermanMock.EXPECT().BlockByNumber(gomock.Any(), nil).Return(latestBlock, nil).AnyTimes()

	filterQuery := ethereum.FilterQuery{
		FromBlock: l1FirstBlock,
		ToBlock:   latestBlockNumber,
		Addresses: l1ContractAddresses,
		Topics:    l1ContractTopics,
	}

	type testCase struct {
		name   string
		getLog func(hDB *hermez_db.HermezDb) (types.Log, error)
		assert func(t *testing.T, hDB *hermez_db.HermezDb)
	}

	const (
		forkIdBytesStartPosition = 64
		forkIdBytesEndPosition   = 96
		rollupDataSize           = 100

		injectedBatchLogTransactionStartByte = 128
		injectedBatchLastGerStartByte        = 32
		injectedBatchLastGerEndByte          = 64
		injectedBatchSequencerStartByte      = 76
		injectedBatchSequencerEndByte        = 96
	)

	testCases := []testCase{
		{
			name: "InitialSequenceBatchesTopic",
			getLog: func(hDB *hermez_db.HermezDb) (types.Log, error) {
				ger := common.HexToHash("0x111111111")
				sequencer := common.HexToAddress("0x222222222")
				batchL2Data := common.HexToHash("0x333333333")

				initialSequenceBatchesData := make([]byte, 200)
				copy(initialSequenceBatchesData[injectedBatchLastGerStartByte:injectedBatchLastGerEndByte], ger.Bytes())
				copy(initialSequenceBatchesData[injectedBatchSequencerStartByte:injectedBatchSequencerEndByte], sequencer.Bytes())
				copy(initialSequenceBatchesData[injectedBatchLogTransactionStartByte:], batchL2Data.Bytes())
				return types.Log{
					BlockNumber: latestBlockNumber.Uint64(),
					Address:     l1ContractAddresses[0],
					Topics:      []common.Hash{contracts.InitialSequenceBatchesTopic},
					Data:        initialSequenceBatchesData,
				}, nil
			},
			assert: func(t *testing.T, hDB *hermez_db.HermezDb) {
				ger := common.HexToHash("0x111111111")
				sequencer := common.HexToAddress("0x222222222")
				batchL2Data := common.HexToHash("0x333333333")

				l1InjectedBatch, err := hDB.GetL1InjectedBatch(0)
				require.NoError(t, err)

				assert.Equal(t, l1InjectedBatch.L1BlockNumber, latestBlock.NumberU64())
				assert.Equal(t, l1InjectedBatch.Timestamp, latestBlock.Time())
				assert.Equal(t, l1InjectedBatch.L1BlockHash, latestBlock.Hash())
				assert.Equal(t, l1InjectedBatch.L1ParentHash, latestBlock.ParentHash())
				assert.Equal(t, l1InjectedBatch.LastGlobalExitRoot.String(), ger.String())
				assert.Equal(t, l1InjectedBatch.Sequencer.String(), sequencer.String())
				assert.ElementsMatch(t, l1InjectedBatch.Transaction, batchL2Data.Bytes())
			},
		},
		{
			name: "AddNewRollupType",
			getLog: func(hDB *hermez_db.HermezDb) (types.Log, error) {
				rollupType := uint64(1)
				rollupTypeHash := common.BytesToHash(big.NewInt(0).SetUint64(rollupType).Bytes())
				rollupData := make([]byte, rollupDataSize)
				rollupForkId := uint64(111)
				rollupForkIdHash := common.BytesToHash(big.NewInt(0).SetUint64(rollupForkId).Bytes())
				copy(rollupData[forkIdBytesStartPosition:forkIdBytesEndPosition], rollupForkIdHash.Bytes())
				return types.Log{
					BlockNumber: latestBlockNumber.Uint64(),
					Address:     l1ContractAddresses[0],
					Topics:      []common.Hash{contracts.AddNewRollupTypeTopic, rollupTypeHash},
					Data:        rollupData,
				}, nil
			},
			assert: func(t *testing.T, hDB *hermez_db.HermezDb) {
				forkID, err := hDB.GetForkFromRollupType(uint64(1))
				require.NoError(t, err)

				assert.Equal(t, forkID, uint64(111))
			},
		},
		{
			name: "AddNewRollupTypeTopicBanana",
			getLog: func(hDB *hermez_db.HermezDb) (types.Log, error) {
				rollupType := uint64(2)
				rollupTypeHash := common.BytesToHash(big.NewInt(0).SetUint64(rollupType).Bytes())
				rollupData := make([]byte, rollupDataSize)
				rollupForkId := uint64(222)
				rollupForkIdHash := common.BytesToHash(big.NewInt(0).SetUint64(rollupForkId).Bytes())
				copy(rollupData[forkIdBytesStartPosition:forkIdBytesEndPosition], rollupForkIdHash.Bytes())
				return types.Log{
					BlockNumber: latestBlockNumber.Uint64(),
					Address:     l1ContractAddresses[0],
					Topics:      []common.Hash{contracts.AddNewRollupTypeTopicBanana, rollupTypeHash},
					Data:        rollupData,
				}, nil
			},
			assert: func(t *testing.T, hDB *hermez_db.HermezDb) {
				forkID, err := hDB.GetForkFromRollupType(uint64(2))
				require.NoError(t, err)

				assert.Equal(t, forkID, uint64(222))
			},
		},
		{
			name: "CreateNewRollupTopic",
			getLog: func(hDB *hermez_db.HermezDb) (types.Log, error) {
				rollupID := uint64(99999)
				rollupIDHash := common.BytesToHash(big.NewInt(0).SetUint64(rollupID).Bytes())
				rollupType := uint64(33)
				rollupForkID := uint64(333)
				if funcErr := hDB.WriteRollupType(rollupType, rollupForkID); funcErr != nil {
					return types.Log{}, funcErr
				}
				newRollupDataCreation := common.BytesToHash(big.NewInt(0).SetUint64(rollupType).Bytes()).Bytes()

				return types.Log{
					BlockNumber: latestBlockNumber.Uint64(),
					Address:     l1ContractAddresses[0],
					Topics:      []common.Hash{contracts.CreateNewRollupTopic, rollupIDHash},
					Data:        newRollupDataCreation,
				}, nil
			},
			assert: func(t *testing.T, hDB *hermez_db.HermezDb) {
				forks, batches, err := hDB.GetAllForkHistory()
				for i := 0; i < len(forks); i++ {
					if forks[i] == uint64(333) {
						assert.Equal(t, batches[i], uint64(0))
						break
					}
				}
				require.NoError(t, err)
			},
		},
		{
			name: "UpdateRollupTopic",
			getLog: func(hDB *hermez_db.HermezDb) (types.Log, error) {
				rollupID := uint64(99999)
				rollupIDHash := common.BytesToHash(big.NewInt(0).SetUint64(rollupID).Bytes())
				rollupType := uint64(44)
				rollupTypeHash := common.BytesToHash(big.NewInt(0).SetUint64(rollupType).Bytes())
				rollupForkID := uint64(444)
				if funcErr := hDB.WriteRollupType(rollupType, rollupForkID); funcErr != nil {
					return types.Log{}, funcErr
				}
				latestVerified := uint64(4444)
				latestVerifiedHash := common.BytesToHash(big.NewInt(0).SetUint64(latestVerified).Bytes())
				updateRollupData := rollupTypeHash.Bytes()
				updateRollupData = append(updateRollupData, latestVerifiedHash.Bytes()...)

				return types.Log{
					BlockNumber: latestBlockNumber.Uint64(),
					Address:     l1ContractAddresses[0],
					Topics:      []common.Hash{contracts.UpdateRollupTopic, rollupIDHash},
					Data:        updateRollupData,
				}, nil
			},
			assert: func(t *testing.T, hDB *hermez_db.HermezDb) {
				forks, batches, err := hDB.GetAllForkHistory()
				for i := 0; i < len(forks); i++ {
					if forks[i] == uint64(444) {
						assert.Equal(t, batches[i], uint64(4444))
						break
					}
				}
				require.NoError(t, err)
			},
		},
	}

	filteredLogs := []types.Log{}
	for _, tc := range testCases {
		ll, err := tc.getLog(hDB)
		require.NoError(t, err)
		filteredLogs = append(filteredLogs, ll)
	}

	EthermanMock.EXPECT().FilterLogs(gomock.Any(), filterQuery).Return(filteredLogs, nil).AnyTimes()

	l1Syncer := syncer.NewL1Syncer(ctx, db2, []syncer.IEtherman{EthermanMock}, l1ContractAddresses, l1ContractTopics, 10, 0, "latest")

	updater := l1infotree.NewUpdater(&ethconfig.Zk{}, l1Syncer, l1infotree.NewInfoTreeL2RpcSyncer(ctx, &ethconfig.Zk{}))

	zkCfg := &ethconfig.Zk{
		L1RollupId:                  uint64(99999),
		L1FirstBlock:                l1FirstBlock.Uint64(),
		L1FinalizedBlockRequirement: uint64(21),
	}
	cfg := StageL1CombinedSyncerCfg(db1, l1Syncer, zkCfg, updater)

	// act
	err = SpawnStageL1CombinedSyncer(s, u, ctx, tx, cfg, false)
	require.NoError(t, err)

	// assert
	for _, tc := range testCases {
		tc.assert(t, hDB)
	}
}

func TestSpawnL1InfoTreeStage(t *testing.T) {
	// arrange
	ctx, db1 := context.Background(), memdb.NewTestDB(t)
	tx := memdb.BeginRw(t, db1)
	err := hermez_db.CreateHermezBuckets(tx)
	require.NoError(t, err)
	err = db.CreateEriDbBuckets(tx)
	require.NoError(t, err)

	_, db2 := context.Background(), memdb.NewTestDB(t)

	hDB := hermez_db.NewHermezDb(tx)
	err = hDB.WriteBlockBatch(0, 0)
	require.NoError(t, err)
	err = stages.SaveStageProgress(tx, stages.L1InfoTree, 20)
	require.NoError(t, err)

	s := &stagedsync.StageState{ID: stages.L1InfoTree, BlockNumber: 0}
	u := &stagedsync.Sync{}

	// mocks
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	EthermanMock := mocks.NewMockIEtherman(mockCtrl)

	l1ContractAddresses := []common.Address{
		common.HexToAddress("0x1"),
		common.HexToAddress("0x2"),
		common.HexToAddress("0x3"),
	}
	l1ContractTopics := [][]common.Hash{
		[]common.Hash{common.HexToHash("0x1")},
		[]common.Hash{common.HexToHash("0x2")},
		[]common.Hash{common.HexToHash("0x3")},
	}

	latestBlockParentHash := common.HexToHash("0x123456789")
	latestBlockTime := uint64(time.Now().Unix())
	latestBlockNumber := big.NewInt(21)
	latestBlockHeader := &types.Header{ParentHash: latestBlockParentHash, Number: latestBlockNumber, Time: latestBlockTime}
	latestBlock := types.NewBlockWithHeader(latestBlockHeader)

	EthermanMock.EXPECT().HeaderByNumber(gomock.Any(), latestBlockNumber).Return(latestBlockHeader, nil).AnyTimes()
	EthermanMock.EXPECT().BlockByNumber(gomock.Any(), nil).Return(latestBlock, nil).AnyTimes()
	filterQuery := ethereum.FilterQuery{
		FromBlock: latestBlockNumber,
		ToBlock:   latestBlockNumber,
		Addresses: l1ContractAddresses,
		Topics:    l1ContractTopics,
	}
	mainnetExitRoot := common.HexToHash("0x111")
	rollupExitRoot := common.HexToHash("0x222")

	l1InfoTreeLog := types.Log{
		BlockNumber: latestBlockNumber.Uint64(),
		Address:     l1ContractAddresses[0],
		Topics:      []common.Hash{contracts.UpdateL1InfoTreeTopic, mainnetExitRoot, rollupExitRoot},
	}
	filteredLogs := []types.Log{l1InfoTreeLog}
	EthermanMock.EXPECT().FilterLogs(gomock.Any(), filterQuery).Return(filteredLogs, nil).AnyTimes()

	l1Syncer := syncer.NewL1Syncer(ctx, db2, []syncer.IEtherman{EthermanMock}, l1ContractAddresses, l1ContractTopics, 10, 0, "latest")

	zkCfg := &ethconfig.Zk{
		L1FirstBlock: latestBlockNumber.Uint64(),
	}

	updater := l1infotree.NewUpdater(zkCfg, l1Syncer, l1infotree.NewInfoTreeL2RpcSyncer(ctx, &ethconfig.Zk{}))
	cfg := StageL1CombinedSyncerCfg(db1, l1Syncer, zkCfg, updater)

	// act
	err = SpawnStageL1CombinedSyncer(s, u, ctx, tx, cfg, false)
	require.NoError(t, err)

	// assert
	// check tree
	tree, err := l1infotree.InitialiseL1InfoTree(hDB)
	require.NoError(t, err)

	combined := append(mainnetExitRoot.Bytes(), rollupExitRoot.Bytes()...)
	gerBytes := keccak256.Hash(combined)
	ger := common.BytesToHash(gerBytes)
	leafBytes := l1infotree.HashLeafData(ger, latestBlockParentHash, latestBlockTime)

	assert.True(t, tree.LeafExists(leafBytes))

	// check WriteL1InfoTreeLeaf
	leaves, err := hDB.GetAllL1InfoTreeLeaves()
	require.NoError(t, err)
	require.NotNil(t, leaves)

	leafHash := common.BytesToHash(leafBytes[:])
	require.NotNil(t, leafHash)
	assert.Len(t, leaves, 1)
	assert.Equal(t, leafHash.String(), leaves[0].String())

	// check WriteL1InfoTreeUpdate
	l1InfoTreeUpdate, err := hDB.GetL1InfoTreeUpdate(0)
	require.NoError(t, err)

	assert.Equal(t, uint64(0), l1InfoTreeUpdate.Index)
	assert.Equal(t, ger, l1InfoTreeUpdate.GER)
	assert.Equal(t, mainnetExitRoot, l1InfoTreeUpdate.MainnetExitRoot)
	assert.Equal(t, rollupExitRoot, l1InfoTreeUpdate.RollupExitRoot)
	assert.Equal(t, latestBlockNumber.Uint64(), l1InfoTreeUpdate.BlockNumber)
	assert.Equal(t, latestBlockTime, l1InfoTreeUpdate.Timestamp)
	assert.Equal(t, latestBlockParentHash, l1InfoTreeUpdate.ParentHash)

	//check  WriteL1InfoTreeUpdateToGer
	l1InfoTreeUpdateToGer, err := hDB.GetL1InfoTreeUpdateByGer(ger)
	require.NoError(t, err)

	assert.Equal(t, uint64(0), l1InfoTreeUpdateToGer.Index)
	assert.Equal(t, ger, l1InfoTreeUpdateToGer.GER)
	assert.Equal(t, mainnetExitRoot, l1InfoTreeUpdateToGer.MainnetExitRoot)
	assert.Equal(t, rollupExitRoot, l1InfoTreeUpdateToGer.RollupExitRoot)
	assert.Equal(t, latestBlockNumber.Uint64(), l1InfoTreeUpdateToGer.BlockNumber)
	assert.Equal(t, latestBlockTime, l1InfoTreeUpdateToGer.Timestamp)
	assert.Equal(t, latestBlockParentHash, l1InfoTreeUpdateToGer.ParentHash)

	// check WriteL1InfoTreeRoot
	root, _, _ := tree.GetCurrentRootCountAndSiblings()
	index, found, err := hDB.GetL1InfoTreeIndexByRoot(root)
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), index)
	assert.True(t, found)

	// check SaveStageProgress
	progress, err := stages.GetStageProgress(tx, stages.L1CombinedSyncer)
	require.NoError(t, err)
	t.Logf("%d", latestBlockNumber.Uint64())
	assert.Equal(t, latestBlockNumber.Uint64()+1, progress)
}

func TestUnwindL1CombinedSyncerStage(t *testing.T) {
	err := UnwindL1CombinedSyncerStage(nil, nil, L1CombinedSyncerCfg{}, context.Background())
	assert.Nil(t, err)
}

func TestPruneL1CombinedSyncerStage(t *testing.T) {
	err := PruneL1CombinedSyncerStage(nil, nil, L1CombinedSyncerCfg{}, context.Background())
	assert.Nil(t, err)
}
