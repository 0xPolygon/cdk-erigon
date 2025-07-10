package test

import (
	"math/big"
	"sync"
	"testing"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/common"
	ethTypes "github.com/ledgerwatch/erigon/core/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
	"github.com/stretchr/testify/assert"
)

func TestBlockInfoMap(t *testing.T) {
	bm := realtimeTypes.NewBlockInfoMap(100)

	blockNum := uint64(2)
	header := &ethTypes.Header{
		Number: big.NewInt(int64(blockNum)),
		Time:   1000,
	}
	prevTxCount := int64(10)

	t.Run("PutHeader and Get", func(t *testing.T) {
		bm.PutHeader(blockNum, header, prevTxCount)
		gotHeader, gotTxCount, exists := bm.Get(blockNum)
		assert.True(t, exists)
		assert.Equal(t, header, gotHeader)
		// Init txCount is -1
		assert.Equal(t, int64(-1), gotTxCount)
	})

	t.Run("Get non-existent", func(t *testing.T) {
		nonExistentNum := uint64(888)
		_, _, exists := bm.Get(nonExistentNum)
		assert.False(t, exists)
	})

	t.Run("Delete", func(t *testing.T) {
		bm.Delete(blockNum)
		_, _, exists := bm.Get(blockNum)
		assert.False(t, exists)
	})

	t.Run("Incremental operations", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			blockNum := uint64(i)
			prevBlockNum := blockNum - 1
			header := &ethTypes.Header{
				Number: big.NewInt(int64(i)),
				Time:   uint64(i * 1000),
			}
			prevTxCount := int64(i * 5)

			// Test PutHeader
			bm.PutHeader(blockNum, header, prevTxCount)
			gotHeader, gotTxCount, exists := bm.Get(blockNum)
			assert.True(t, exists)
			assert.NotNil(t, gotHeader)
			assert.Equal(t, big.NewInt(int64(i)), gotHeader.Number)
			assert.Equal(t, uint64(i*1000), gotHeader.Time)
			// Init txCount is -1
			assert.Equal(t, int64(-1), gotTxCount)

			// Check previous block txCount
			if i == 0 {
				continue
			}
			_, gotTxCount, exists = bm.Get(prevBlockNum)
			assert.True(t, exists)
			assert.Equal(t, gotTxCount, prevTxCount)
		}

		// Test delete
		for i := 0; i < 10; i++ {
			blockNum := uint64(i)
			bm.Delete(blockNum)
			_, _, exists := bm.Get(blockNum)
			assert.False(t, exists)
		}
	})
}

func TestTxInfoMap(t *testing.T) {
	tm := realtimeTypes.NewTxInfoMap(100, 1000)

	blockNumber := uint64(5)
	txHash := common.HexToHash("0x123")
	value := uint256.NewInt(0)
	gasPrice := uint256.NewInt(0)
	tx := ethTypes.NewTransaction(0, common.Address{}, value, 0, gasPrice, nil)
	receipt := &ethTypes.Receipt{
		Status: 1,
	}
	innerTxs := []*zktypes.InnerTx{
		{
			Dept:          *big.NewInt(1),
			InternalIndex: *big.NewInt(1),
			CallType:      "call",
			Name:          "call_1",
			TraceAddress:  "0",
			CodeAddress:   "0x123",
			From:          "0x456",
			To:            "0x789",
			Input:         "0x",
			Output:        "0x",
			IsError:       false,
			Gas:           21000,
			GasUsed:       21000,
			Value:         "0",
			ValueWei:      "0",
			CallValueWei:  "0",
			Error:         "",
		},
		{
			Dept:          *big.NewInt(1),
			InternalIndex: *big.NewInt(2),
			CallType:      "call",
			Name:          "call_2",
			TraceAddress:  "1",
			CodeAddress:   "0x123",
			From:          "0x456",
			To:            "0x789",
			Input:         "0x",
			Output:        "0x",
			IsError:       false,
			Gas:           21000,
			GasUsed:       21000,
			Value:         "0",
			ValueWei:      "0",
			CallValueWei:  "0",
			Error:         "",
		},
	}

	t.Run("Put and Get", func(t *testing.T) {
		tm.Put(blockNumber, txHash, tx, receipt, innerTxs)
		gotTx, gotReceipt, _, gotInnerTxs, exists := tm.GetTx(txHash)
		assert.True(t, exists)
		assert.Equal(t, tx, gotTx)
		assert.Equal(t, receipt, gotReceipt)
		assert.Equal(t, innerTxs, gotInnerTxs)
		txHashes, ok := tm.GetBlockTxs(blockNumber)
		assert.True(t, ok)
		assert.Equal(t, txHashes, []common.Hash{txHash})
	})

	t.Run("Get non-existent", func(t *testing.T) {
		nonExistentHash := common.HexToHash("0x456")
		_, _, _, _, exists := tm.GetTx(nonExistentHash)
		assert.False(t, exists)
	})

	t.Run("Delete", func(t *testing.T) {
		tm.Delete(blockNumber)
		_, _, _, _, exists := tm.GetTx(txHash)
		assert.False(t, exists)
	})

	blockNumber = 10
	t.Run("Concurrent operations", func(t *testing.T) {
		const goroutines = 10
		var wg sync.WaitGroup
		hashes := make([]common.Hash, 0, goroutines)

		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			hash := common.BytesToHash([]byte{byte(i), byte(i >> 8), byte(i >> 16), byte(i >> 24)})
			go func(i int, hash common.Hash) {
				defer wg.Done()

				value := uint256.NewInt(uint64(i))
				gasPrice := uint256.NewInt(uint64(i))
				tx := ethTypes.NewTransaction(uint64(i), common.Address{}, value, uint64(i), gasPrice, nil)
				receipt := &ethTypes.Receipt{Status: uint64(i)}
				innerTxs := []*zktypes.InnerTx{
					{
						Dept:          *big.NewInt(int64(i)),
						InternalIndex: *big.NewInt(int64(i)),
						CallType:      "call",
						Name:          "call_1",
						TraceAddress:  "0",
						CodeAddress:   "0x123",
						From:          "0x456",
						To:            "0x789",
						Input:         "0x",
						Output:        "0x",
						IsError:       false,
						Gas:           uint64(i),
						GasUsed:       uint64(i),
						Value:         "0",
						ValueWei:      "0",
						CallValueWei:  "0",
						Error:         "",
					},
					{
						Dept:          *big.NewInt(int64(i)),
						InternalIndex: *big.NewInt(int64(i + 1)),
						CallType:      "call",
						Name:          "call_2",
						TraceAddress:  "1",
						CodeAddress:   "0x123",
						From:          "0x456",
						To:            "0x789",
						Input:         "0x",
						Output:        "0x",
						IsError:       false,
						Gas:           uint64(i),
						GasUsed:       uint64(i),
						Value:         "0",
						ValueWei:      "0",
						CallValueWei:  "0",
						Error:         "",
					},
				}

				tm.Put(blockNumber, hash, tx, receipt, innerTxs)

				gotTx, gotReceipt, _, gotInnerTxs, exists := tm.GetTx(hash)
				assert.True(t, exists)
				assert.NotNil(t, gotTx)
				assert.NotNil(t, gotReceipt)
				assert.Equal(t, uint64(i), gotReceipt.Status)
				assert.Equal(t, innerTxs, gotInnerTxs)
			}(i, hash)
			hashes = append(hashes, hash)
		}

		wg.Wait()

		// Check if all hashes are in the block
		txHashes, ok := tm.GetBlockTxs(blockNumber)
		assert.True(t, ok)
		assert.Equal(t, len(hashes), len(txHashes))
		for _, hash := range hashes {
			assert.Contains(t, txHashes, hash)
		}

		tm.Delete(blockNumber)
		_, ok = tm.GetBlockTxs(blockNumber)
		assert.False(t, ok)

		for i := 0; i < goroutines; i++ {
			hash := common.BytesToHash([]byte{byte(i), byte(i >> 8), byte(i >> 16), byte(i >> 24)})
			_, _, _, _, exists := tm.GetTx(hash)
			assert.False(t, exists)
		}
	})
}
