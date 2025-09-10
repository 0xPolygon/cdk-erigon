package test

import (
	"math/big"
	"testing"

	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/common/u256"
	types1 "github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/core/vm"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
	"gotest.tools/v3/assert"
)

var (
	dynFeeTx = &types1.DynamicFeeTransaction{
		CommonTx: types1.CommonTx{
			Nonce: 3,
			To:    &testToAddr,
			Value: uint256.NewInt(10),
			Gas:   25000,
			Data:  libcommon.FromHex("5544"),
		},
		ChainID:    u256.Num1,
		Tip:        uint256.NewInt(1),
		FeeCap:     uint256.NewInt(1),
		AccessList: accesses,
	}

	blobTx = &types1.BlobTx{
		DynamicFeeTransaction: *dynFeeTx,
		MaxFeePerBlobGas:      uint256.NewInt(10),
		BlobVersionedHashes:   []libcommon.Hash{{0}},
	}
)

func TestLegacyTx(t *testing.T) {
	// Test from
	emptyTx := types1.NewTransaction(
		0,
		libcommon.HexToAddress(testToAddr.String()),
		uint256.NewInt(0), 0, uint256.NewInt(10),
		nil,
	)
	emptyTx.SetSender(testFromAddr)

	emptyTxReceipt := types1.NewReceipt(false, 1000)
	emptyTxInnerTxs := []*zktypes.InnerTx{}
	emptyChangeset := &realtimeTypes.Changeset{}

	blockNumber := uint64(100)
	blockTime := uint64(1000)
	emptyMsg, err := kafkaTypes.ToKafkaTransactionMessage(emptyTx, emptyTxReceipt, emptyTxInnerTxs, emptyChangeset, blockNumber, 0)
	assert.NilError(t, err)
	AssertCommonTx(t, emptyMsg, emptyTx, blockNumber, types1.LegacyTxType)
	assert.Equal(t, emptyMsg.GasPrice, emptyTx.GetPrice().String())
	AssertReceipt(t, emptyMsg, emptyTxReceipt)
	AssertInnerTxs(t, emptyMsg, nil)

	sigBytes := "98ff921201554726367d2be8c804a7ff89ccf285ebc57dff8ae4c44b9c19ac4a8887321be575c8095f789dd4c743dfe42c1820f9231f98a962b210e3ac2452a301"
	rightvrsTx, _ := types1.NewTransaction(
		3,
		testToAddr,
		uint256.NewInt(10),
		2000,
		u256.Num1,
		libcommon.FromHex("5544"),
	).WithSignature(
		*types1.LatestSignerForChainID(nil),
		libcommon.Hex2Bytes(sigBytes),
	)
	rightvrsTx.SetSender(testFromAddr)

	rightvrsTxReceipt := &types1.Receipt{
		PostState:         libcommon.Hash{2}.Bytes(),
		CumulativeGasUsed: 3,
		Logs: []*types1.Log{
			{Address: libcommon.BytesToAddress([]byte{0x22})},
			{Address: libcommon.BytesToAddress([]byte{0x02, 0x22})},
		},
		TxHash:          rightvrsTx.Hash(),
		ContractAddress: libcommon.BytesToAddress([]byte{0x02, 0x22, 0x22}),
		GasUsed:         2,
	}

	rightvrsTxInnerTxs := []*zktypes.InnerTx{
		{
			Name:     "innerTx1",
			CallType: vm.CALL_TYP,
		},
	}

	rightvrsTxChangeset := &realtimeTypes.Changeset{
		BalanceChanges: map[libcommon.Address]*uint256.Int{
			testToAddr: uint256.NewInt(10),
		},
	}

	msg, err := kafkaTypes.ToKafkaTransactionMessage(rightvrsTx, rightvrsTxReceipt, rightvrsTxInnerTxs, rightvrsTxChangeset, blockNumber, blockTime)
	assert.NilError(t, err)
	AssertCommonTx(t, msg, rightvrsTx, blockNumber, types1.LegacyTxType)
	assert.Equal(t, msg.BlockTime, blockTime)
	assert.Equal(t, msg.GasPrice, rightvrsTx.GetPrice().String())
	AssertReceipt(t, msg, rightvrsTxReceipt)
	AssertInnerTxs(t, msg, rightvrsTxInnerTxs)
	AssertChangeseet(t, msg, rightvrsTxChangeset)

	// Test to
	convertEmptyTx, convertBlockNumber, err := emptyMsg.GetTransaction()
	assert.NilError(t, err)
	assert.Equal(t, convertBlockNumber, blockNumber)
	AssertCommonTx(t, emptyMsg, convertEmptyTx, convertBlockNumber, types1.LegacyTxType)
	assert.Equal(t, emptyMsg.GasPrice, convertEmptyTx.GetPrice().String())

	convertRightvsTx, convertBlockNumber, err := msg.GetTransaction()
	assert.NilError(t, err)
	assert.Equal(t, convertBlockNumber, blockNumber)
	AssertCommonTx(t, msg, convertRightvsTx, convertBlockNumber, types1.LegacyTxType)
	assert.Equal(t, msg.GasPrice, convertRightvsTx.GetPrice().String())
}

func TestAccessListTx(t *testing.T) {
	// Test from
	sigBytes := "c9519f4f2b30335884581971573fadf60c6204f59a911df35ee8a540456b266032f1e8e2c5dd761f9e4f88f41c8310aeaba26a8bfcdacfedfa12ec3862d3752101"
	accessListTx := &types1.AccessListTx{
		ChainID: u256.Num1,
		LegacyTx: types1.LegacyTx{
			CommonTx: types1.CommonTx{
				Nonce: 3,
				To:    &testToAddr,
				Value: uint256.NewInt(10),
				Gas:   25000,
				Data:  libcommon.FromHex("5544"),
			},
			GasPrice: uint256.NewInt(1),
		},
		AccessList: accesses,
	}

	signedAccessListTx, _ := accessListTx.WithSignature(
		*types1.LatestSignerForChainID(big.NewInt(1)),
		libcommon.Hex2Bytes(sigBytes),
	)
	signedAccessListTx.SetSender(testFromAddr)

	signedAccessListTxReceipt := &types1.Receipt{
		Type:              types1.AccessListTxType,
		PostState:         libcommon.Hash{3}.Bytes(),
		CumulativeGasUsed: 6,
		Logs: []*types1.Log{
			{Address: libcommon.BytesToAddress([]byte{0x33})},
			{Address: libcommon.BytesToAddress([]byte{0x03, 0x33})},
		},
		TxHash:          signedAccessListTx.Hash(),
		ContractAddress: libcommon.BytesToAddress([]byte{0x03, 0x33, 0x33}),
		GasUsed:         3,
	}
	signedAccessListTxInnerTxs := []*zktypes.InnerTx{
		{
			Name:     "innerTx1",
			CallType: vm.CALL_TYP,
		},
	}
	signedAccessListTxChangeset := &realtimeTypes.Changeset{
		BalanceChanges: map[libcommon.Address]*uint256.Int{
			testToAddr: uint256.NewInt(10),
		},
	}

	blockNumber := uint64(100)
	blockTime := uint64(1000)
	msg, err := kafkaTypes.ToKafkaTransactionMessage(signedAccessListTx, signedAccessListTxReceipt, signedAccessListTxInnerTxs, signedAccessListTxChangeset, blockNumber, blockTime)
	assert.NilError(t, err)
	AssertCommonTx(t, msg, signedAccessListTx, blockNumber, types1.AccessListTxType)
	assert.Equal(t, msg.BlockTime, blockTime)
	assert.Equal(t, msg.GasPrice, signedAccessListTx.GetPrice().String())
	AssertReceipt(t, msg, signedAccessListTxReceipt)
	AssertInnerTxs(t, msg, signedAccessListTxInnerTxs)
	AssertAccessList(t, msg.AccessList)
	AssertChangeseet(t, msg, signedAccessListTxChangeset)

	// Test to
	convertAccessListTx, convertBlockNumber, err := msg.GetTransaction()
	assert.NilError(t, err)
	assert.Equal(t, convertBlockNumber, blockNumber)
	AssertCommonTx(t, msg, convertAccessListTx, convertBlockNumber, types1.AccessListTxType)
	assertTxAccessList(t, convertAccessListTx.GetAccessList())
	assert.Equal(t, msg.GasPrice, convertAccessListTx.GetPrice().String())
}

func TestDynamicFeeTx(t *testing.T) {
	// Test from
	sigBytes := "c9519f4f2b30335884581971573fadf60c6204f59a911df35ee8a540456b266032f1e8e2c5dd761f9e4f88f41c8310aeaba26a8bfcdacfedfa12ec3862d3752101"
	signedDynFeeTx, _ := dynFeeTx.WithSignature(
		*types1.LatestSignerForChainID(big.NewInt(1)),
		libcommon.Hex2Bytes(sigBytes),
	)
	signedDynFeeTx.SetSender(testFromAddr)

	signedDynFeeTxReceipt := &types1.Receipt{
		Type:              types1.DynamicFeeTxType,
		PostState:         libcommon.Hash{4}.Bytes(),
		CumulativeGasUsed: 10,
		Logs: []*types1.Log{
			{Address: libcommon.BytesToAddress([]byte{0x33})},
			{Address: libcommon.BytesToAddress([]byte{0x03, 0x33})},
		},
		TxHash:          signedDynFeeTx.Hash(),
		ContractAddress: libcommon.BytesToAddress([]byte{0x03, 0x33, 0x33}),
		GasUsed:         3,
	}
	signedDynFeeTxInnerTxs := []*zktypes.InnerTx{
		{
			Name:     "innerTx1",
			CallType: vm.CALL_TYP,
		},
	}
	signedDynFeeTxChangeset := &realtimeTypes.Changeset{
		BalanceChanges: map[libcommon.Address]*uint256.Int{
			testToAddr: uint256.NewInt(10),
		},
	}

	blockNumber := uint64(100)
	blockTime := uint64(1000)
	msg, err := kafkaTypes.ToKafkaTransactionMessage(signedDynFeeTx, signedDynFeeTxReceipt, signedDynFeeTxInnerTxs, signedDynFeeTxChangeset, blockNumber, blockTime)
	assert.NilError(t, err)
	AssertCommonTx(t, msg, signedDynFeeTx, blockNumber, types1.DynamicFeeTxType)
	assert.Equal(t, msg.BlockTime, blockTime)
	assert.Equal(t, msg.Tip, signedDynFeeTx.GetTip().String())
	assert.Equal(t, msg.FeeCap, signedDynFeeTx.GetFeeCap().String())
	AssertReceipt(t, msg, signedDynFeeTxReceipt)
	AssertInnerTxs(t, msg, signedDynFeeTxInnerTxs)
	AssertAccessList(t, msg.AccessList)
	AssertChangeseet(t, msg, signedDynFeeTxChangeset)

	// Test to
	convertDynFeeTx, convertBlockNumber, err := msg.GetTransaction()
	assert.NilError(t, err)
	assert.Equal(t, convertBlockNumber, blockNumber)
	AssertCommonTx(t, msg, convertDynFeeTx, convertBlockNumber, types1.DynamicFeeTxType)
	assertTxAccessList(t, convertDynFeeTx.GetAccessList())
	assert.Equal(t, msg.Tip, convertDynFeeTx.GetTip().String())
	assert.Equal(t, msg.FeeCap, convertDynFeeTx.GetFeeCap().String())
}

func TestFromBlobTx(t *testing.T) {
	// Test from
	blobTx.SetSender(testFromAddr)
	blobTxReceipt := &types1.Receipt{
		PostState:         libcommon.Hash{2}.Bytes(),
		CumulativeGasUsed: 15,
		Logs: []*types1.Log{
			{Address: libcommon.BytesToAddress([]byte{0x22})},
			{Address: libcommon.BytesToAddress([]byte{0x02, 0x22})},
		},
		TxHash:          blobTx.Hash(),
		ContractAddress: libcommon.BytesToAddress([]byte{0x02, 0x22, 0x22}),
		GasUsed:         5,
	}
	blobTxInnerTxs := []*zktypes.InnerTx{
		{
			Name:     "innerTx1",
			CallType: vm.CALL_TYP,
		},
	}
	blobTxChangeset := &realtimeTypes.Changeset{
		BalanceChanges: map[libcommon.Address]*uint256.Int{
			testToAddr: uint256.NewInt(10),
		},
	}

	blockNumber := uint64(100)
	blockTime := uint64(1000)
	msg, err := kafkaTypes.ToKafkaTransactionMessage(blobTx, blobTxReceipt, blobTxInnerTxs, blobTxChangeset, blockNumber, blockTime)
	assert.NilError(t, err)
	AssertCommonTx(t, msg, blobTx, blockNumber, types1.BlobTxType)
	assert.Equal(t, msg.BlockTime, blockTime)
	assert.Equal(t, msg.Tip, blobTx.GetTip().String())
	assert.Equal(t, msg.FeeCap, blobTx.GetFeeCap().String())
	AssertReceipt(t, msg, blobTxReceipt)
	AssertInnerTxs(t, msg, blobTxInnerTxs)
	AssertAccessList(t, msg.AccessList)

	assert.Equal(t, msg.MaxFeePerBlobGas, "10")
	assert.Equal(t, len(msg.BlobVersionedHashes), 1)
	for _, hash := range msg.BlobVersionedHashes {
		assert.Equal(t, hash, "0x0000000000000000000000000000000000000000000000000000000000000000")
	}

	// Test to
	convertBlobTx, convertBlockNumber, err := msg.GetTransaction()
	assert.NilError(t, err)
	AssertCommonTx(t, msg, convertBlobTx, convertBlockNumber, types1.BlobTxType)
	assert.Equal(t, msg.Tip, convertBlobTx.GetTip().String())
	assert.Equal(t, msg.FeeCap, convertBlobTx.GetFeeCap().String())
	assertTxAccessList(t, convertBlobTx.GetAccessList())
}
