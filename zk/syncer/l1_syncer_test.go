package syncer

import (
	"context"
	ethereum "github.com/erigontech/erigon"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/kv/memdb"
	"github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/zk/contracts"
	"github.com/erigontech/erigon/zk/syncer/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"math/big"
	"reflect"
	"testing"
	"time"
)

func TestRunQueryBlocksOnce(t *testing.T) {
	ctx := context.Background()
	l1CacheDb := memdb.NewTestDB(t)
	l1CacheSyncer, err := NewL1SyncerCache(ctx, l1CacheDb)
	assert.NoError(t, err)
	defer l1CacheSyncer.Close()

	// mocks
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	ethermanMock := mocks.NewMockIEtherman(mockCtrl)

	l1ContractAddresses := []common.Address{
		common.HexToAddress("0x1"),
		common.HexToAddress("0x2"),
		common.HexToAddress("0x3"),
	}
	l1ContractTopics := [][]common.Hash{
		{common.HexToHash("0x1")},
		{common.HexToHash("0x2")},
		{common.HexToHash("0x3")},
	}

	latestBlockParentHash := common.HexToHash("0x123456789")
	latestBlockTime := uint64(time.Now().Unix())
	latestBlockNumber := big.NewInt(21)
	latestBlockHeader := &types.Header{ParentHash: latestBlockParentHash, Number: latestBlockNumber, Time: latestBlockTime}
	latestBlock := types.NewBlockWithHeader(latestBlockHeader)

	ethermanMock.EXPECT().HeaderByNumber(gomock.Any(), latestBlockNumber).Return(latestBlockHeader, nil).AnyTimes()
	ethermanMock.EXPECT().BlockByNumber(gomock.Any(), nil).Return(latestBlock, nil).AnyTimes()
	filterQuery := ethereum.FilterQuery{
		FromBlock: big.NewInt(21),
		ToBlock:   latestBlockNumber,
		Addresses: l1ContractAddresses,
		Topics:    l1ContractTopics,
	}
	mainnetExitRoot := common.HexToHash("0x111")
	rollupExitRoot := common.HexToHash("0x222")

	filteredLogs := []types.Log{
		{
			BlockNumber: latestBlockNumber.Uint64(),
			Index:       0,
			Address:     l1ContractAddresses[0],
			Topics:      []common.Hash{contracts.UpdateL1InfoTreeTopic, mainnetExitRoot, rollupExitRoot},
		},
		{
			BlockNumber: latestBlockNumber.Uint64(),
			Index:       1,
			Address:     l1ContractAddresses[0],
			Topics:      []common.Hash{contracts.UpdateL1InfoTreeTopic, mainnetExitRoot, rollupExitRoot},
		},
		{
			BlockNumber: latestBlockNumber.Uint64(),
			Index:       2,
			Address:     l1ContractAddresses[0],
			Topics:      []common.Hash{contracts.UpdateL1InfoTreeTopic, mainnetExitRoot, rollupExitRoot},
		},
	}

	ethermanMock.EXPECT().FilterLogs(gomock.Any(), filterQuery).Return(filteredLogs, nil).AnyTimes()
	ethermanMock.EXPECT().FilterLogs(gomock.Any(), gomock.Not(filterQuery)).Return(nil, nil).AnyTimes()

	l1Syncer := NewL1Syncer(ctx, l1CacheSyncer, []IEtherman{ethermanMock}, l1ContractAddresses, l1ContractTopics, 10, 0, "latest")

	checkLogsChanClosed := func(ch chan []types.Log) bool {
		select {
		case _, ok := <-ch:
			if !ok {
				return true
			}
			panic("value from closed channel")
		default:
			return false
		}
	}

	logsCh := make(chan []types.Log, 100)
	errCh := make(chan error)

	go l1Syncer.RunQueryBlocksOnce("l1_syncer_test", 0, logsCh, errCh)

	expectedResult := processLogs(t, logsCh, errCh)

	assert.Len(t, expectedResult, len(filteredLogs))

	for index, expectedLog := range expectedResult {
		assert.Equal(t, expectedLog.BlockNumber, filteredLogs[index].BlockNumber)
		assert.Equal(t, expectedLog.Index, filteredLogs[index].Index)
		assert.Equal(t, expectedLog.Address, filteredLogs[index].Address)
		assert.Equal(t, expectedLog.Topics, filteredLogs[index].Topics)
	}

	assert.Equal(t, true, checkLogsChanClosed(logsCh))
}

func TestMakeFetchJobs_RangeBased(t *testing.T) {
	var (
		from       uint64 = 1
		to         uint64 = 100
		blockRange uint64 = 20
	)

	jobs := makeFetchJobs(from, to, blockRange)

	coveredBlocks := make([]bool, to-from+1)

	for _, job := range jobs {
		if job.From > job.To {
			t.Errorf("Invalid job range: From %d > To %d", job.From, job.To)
		}
		if job.To-job.From+1 > blockRange {
			t.Errorf("Job range too large: %d blocks (max %d)", job.To-job.From+1, blockRange)
		}
		for i := job.From; i <= job.To; i++ {
			if i < from || i > to {
				t.Errorf("Block %d out of range [%d, %d]", i, from, to)
			}
			coveredBlocks[i-from] = true
		}
	}

	// Check that all blocks are covered exactly once
	for i, covered := range coveredBlocks {
		if !covered {
			t.Errorf("Block %d not covered", i+int(from))
		}
	}
}

func TestMakeFetchJobs(t *testing.T) {
	tests := []struct {
		from       uint64
		to         uint64
		blockRange uint64
		expected   []fetchJob
	}{
		{
			from:       100,
			to:         250,
			blockRange: 50,
			expected: []fetchJob{
				{From: 100, To: 149},
				{From: 150, To: 199},
				{From: 200, To: 249},
				{From: 250, To: 250},
			},
		},
		{
			from:       1,
			to:         99,
			blockRange: 100,
			expected: []fetchJob{
				{From: 1, To: 99},
			},
		},
	}

	for i, test := range tests {
		result := makeFetchJobs(test.from, test.to, test.blockRange)
		if !reflect.DeepEqual(result, test.expected) {
			t.Errorf("Test %d failed: expected %+v, got %+v", i, test.expected, result)
		}
	}
}

func processLogs(t *testing.T, logsCh <-chan []types.Log, errCh <-chan error) []types.Log {
	resultLogs := []types.Log{}
	for {
		select {
		case logs, ok := <-logsCh:
			if !ok {
				return resultLogs
			}
			resultLogs = append(resultLogs, logs...)
		case errVal := <-errCh:
			if errVal != nil {
				t.Logf("Error received: %v", errVal)
				return resultLogs
			}
		}
	}
}
