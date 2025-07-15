package test

import (
	"context"
	"math/big"
	"testing"

	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/common"
	"github.com/ledgerwatch/erigon/common/u256"
	ethTypes "github.com/ledgerwatch/erigon/core/types"
	types1 "github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/zk/realtime/kafka"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
	"github.com/ledgerwatch/log/v3"
	"gotest.tools/v3/assert"
)

var (
	sigBytes = "98ff921201554726367d2be8c804a7ff89ccf285ebc57dff8ae4c44b9c19ac4a8887321be575c8095f789dd4c743dfe42c1820f9231f98a962b210e3ac2452a301"

	rightvrsTx, _ = ethTypes.NewTransaction(
		3,
		testToAddr,
		uint256.NewInt(10),
		2000,
		u256.Num1,
		libcommon.FromHex("5544"),
	).WithSignature(
		*ethTypes.LatestSignerForChainID(nil),
		libcommon.Hex2Bytes(sigBytes),
	)

	rightvrsTxReceipt = &ethTypes.Receipt{
		PostState:         libcommon.Hash{2}.Bytes(),
		CumulativeGasUsed: 3,
		Logs: []*ethTypes.Log{
			{Address: libcommon.BytesToAddress([]byte{0x22})},
			{Address: libcommon.BytesToAddress([]byte{0x02, 0x22})},
		},
		TxHash:          rightvrsTx.Hash(),
		ContractAddress: libcommon.BytesToAddress([]byte{0x02, 0x22, 0x22}),
		GasUsed:         2,
	}

	rightvrsTxInnerTxs = []*zktypes.InnerTx{
		{
			Name:     "innerTx1",
			CallType: vm.CALL_TYP,
		},
	}

	rightvrsTxChangeset = &realtimeTypes.Changeset{
		BalanceChanges: map[libcommon.Address]*uint256.Int{
			testToAddr: uint256.NewInt(10),
		},
	}

	accessListTx = &types1.AccessListTx{
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

	difficulty, _ = new(big.Int).SetString("8398142613866510000000000000000000000000000000", 10)
	blockHeader   = &ethTypes.Header{
		ParentHash:  libcommon.HexToHash("0x8b00fcf1e541d371a3a1b79cc999a85cc3db5ee5637b5159646e1acd3613fd15"),
		UncleHash:   libcommon.HexToHash("1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"),
		Coinbase:    libcommon.HexToAddress("0x571846e42308df2dad8ed792f44a8bfddf0acb4d"),
		Root:        libcommon.HexToHash("0x351780124dae86b84998c6d4fe9a88acfb41b4856b4f2c56767b51a4e2f94dd4"),
		TxHash:      libcommon.HexToHash("0x6a35133fbff7ea2cb5ee7635c9fb623f96d31d689d806a2bfe40a2b1d90ee99c"),
		ReceiptHash: libcommon.HexToHash("0x324f54860e214ea896ea7a05bda30f85541be3157de77a9059a04fdb1e86badd"),
		Difficulty:  difficulty,
		Number:      big.NewInt(24679923),
		GasLimit:    30_000_000,
		GasUsed:     3_074_345,
		Time:        1666343339,
		Extra:       common.FromHex("0x1234"),
		BaseFee:     big.NewInt(7_000_000_000),
		AuRaStep:    13078,
		AuRaSeal:    common.FromHex("0x75bda30f85541be059646e1acd3613fd100846e42308df2dad8ed79b9a9e91c9db994386599a683820a1394684d41fc139c4805684142e6b15a722a2e9cc51f7ee"),
	}
)

func TestKafka(t *testing.T) {
	rightvrsTx.SetSender(testFromAddr)
	cfg := kafka.KafkaConfig{
		BootstrapServers: []string{"0.0.0.0:9095"},
		BlockTopic:       "xlayer-test-block",
		TxTopic:          "xlayer-test-tx",
		ErrorTopic:       "xlayer-test-error",
		ClientID:         "xlayer-test-consumer",
		GroupID:          "xlayer-test-consumer-1",
	}
	producer, err := kafka.NewKafkaProducer(cfg)
	assert.NilError(t, err)

	for i := 0; i < 10; i++ {
		err = producer.SendKafkaTransaction(context.Background(), uint64(i), rightvrsTx, rightvrsTxReceipt, rightvrsTxInnerTxs, rightvrsTxChangeset)
		assert.NilError(t, err)

		err = producer.SendKafkaBlockInfo(context.Background(), blockHeader, 10)
		assert.NilError(t, err)

		err = producer.SendKafkaErrorTrigger(context.Background(), uint64(i))
		assert.NilError(t, err)
	}

	accessListTx.SetSender(testFromAddr)
	for i := 10; i < 20; i++ {
		err = producer.SendKafkaTransaction(context.Background(), uint64(i), accessListTx, rightvrsTxReceipt, rightvrsTxInnerTxs, rightvrsTxChangeset)

		assert.NilError(t, err)
	}

	err = producer.Close()
	assert.NilError(t, err)

	consumer, err := kafka.NewKafkaConsumer(cfg, false)
	assert.NilError(t, err)
	ctx, ctxWithCancel := context.WithCancel(context.Background())
	headersChan := make(chan kafkaTypes.BlockMessage, 20)
	txMsgsChan := make(chan kafkaTypes.TransactionMessage, 20)
	errorMsgsChan := make(chan kafkaTypes.ErrorTriggerMessage, 20)
	errorChan := make(chan error, 10)
	go consumer.ConsumeKafka(ctx, headersChan, txMsgsChan, errorMsgsChan, errorChan, log.New())

	// Verify tx messages
	for i := 0; i < 10; i++ {
		select {
		case err := <-errorChan:
			t.Fatalf("Received error from consumer: %v", err)
		case txMsg := <-txMsgsChan:
			AssertCommonTx(t, txMsg, rightvrsTx, uint64(i), ethTypes.LegacyTxType)
			AssertReceipt(t, txMsg, rightvrsTxReceipt)
			AssertInnerTxs(t, txMsg, rightvrsTxInnerTxs)
			AssertChangeseet(t, txMsg, rightvrsTxChangeset)
		}
	}

	for i := 10; i < 20; i++ {
		select {
		case err := <-errorChan:
			t.Fatalf("Received error from consumer: %v", err)
		case txMsg := <-txMsgsChan:
			AssertCommonTx(t, txMsg, accessListTx, uint64(i), ethTypes.AccessListTxType)
			AssertAccessList(t, txMsg.AccessList)
			AssertReceipt(t, txMsg, rightvrsTxReceipt)
			AssertInnerTxs(t, txMsg, rightvrsTxInnerTxs)
			AssertChangeseet(t, txMsg, rightvrsTxChangeset)
		}
	}

	// Verify header messages
	for i := 0; i < 10; i++ {
		select {
		case err := <-errorChan:
			t.Fatalf("Received error from consumer: %v", err)
		case rcvHeader := <-headersChan:
			header, _, err := rcvHeader.GetBlockInfo()
			assert.NilError(t, err)
			AssertHeader(t, blockHeader, header)
		}
	}

	// Verify error trigger messages
	for i := 0; i < 10; i++ {
		select {
		case err := <-errorChan:
			t.Fatalf("Received error from consumer: %v", err)
		case errorMsg := <-errorMsgsChan:
			assert.Equal(t, errorMsg.BlockNumber, uint64(i))
		}
	}

	ctxWithCancel()
	err = consumer.Close()
	assert.NilError(t, err)
}
