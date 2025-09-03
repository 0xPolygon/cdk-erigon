//go:build !skip_smoke_realtime
// +build !skip_smoke_realtime

package test

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/common"
	"github.com/ledgerwatch/erigon/common/u256"
	ethTypes "github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/zk/realtime/kafka"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
	"github.com/stretchr/testify/require"
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

	accessListTx = &ethTypes.AccessListTx{
		ChainID: u256.Num1,
		LegacyTx: ethTypes.LegacyTx{
			CommonTx: ethTypes.CommonTx{
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
	testHash = libcommon.HexToHash("0x1234567890abcdef")
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

	err := createKafkaTopics(cfg)
	assert.NilError(t, err)

	producer, err := kafka.NewKafkaProducer(cfg, context.Background(), nil)
	assert.NilError(t, err)

	for i := 1; i <= 10; i++ {
		err = producer.SendKafkaTransaction(uint64(i), rightvrsTx, rightvrsTxReceipt, rightvrsTxInnerTxs, rightvrsTxChangeset)
		assert.NilError(t, err)

		err = producer.SendKafkaBlockInfo(&realtimeTypes.BlockInfo{
			Header:  blockHeader,
			TxCount: int64(i),
			Hash:    testHash,
		})
		assert.NilError(t, err)

		err = producer.SendKafkaErrorTrigger(uint64(i))
		assert.NilError(t, err)
	}

	accessListTx.SetSender(testFromAddr)
	for i := 11; i <= 20; i++ {
		err = producer.SendKafkaTransaction(uint64(i), accessListTx, rightvrsTxReceipt, rightvrsTxInnerTxs, rightvrsTxChangeset)

		assert.NilError(t, err)
	}

	err = producer.Close()
	assert.NilError(t, err)

	consumer, err := kafka.NewKafkaConsumer(cfg, false)
	assert.NilError(t, err)
	ctx, ctxWithCancel := context.WithCancel(context.Background())
	headersChan := make(chan realtimeTypes.BlockInfo, 20)
	txMsgsChan := make(chan kafkaTypes.TransactionMessage, 20)
	errorMsgsChan := make(chan kafkaTypes.ErrorTriggerMessage, 20)
	errorChan := make(chan error, 10)
	go consumer.ConsumeKafka(ctx, headersChan, txMsgsChan, errorMsgsChan, errorChan)

	// Verify tx messages
	for i := 1; i <= 10; i++ {
		select {
		case err := <-errorChan:
			t.Fatalf("Received error from consumer: %v", err)
		case txMsg := <-txMsgsChan:
			AssertCommonTxWithoutBlockNumber(t, txMsg, rightvrsTx, ethTypes.LegacyTxType)
			AssertReceipt(t, txMsg, rightvrsTxReceipt)
			AssertInnerTxs(t, txMsg, rightvrsTxInnerTxs)
			AssertChangeseet(t, txMsg, rightvrsTxChangeset)
		}
	}

	for i := 11; i <= 20; i++ {
		select {
		case err := <-errorChan:
			t.Fatalf("Received error from consumer: %v", err)
		case txMsg := <-txMsgsChan:
			AssertCommonTxWithoutBlockNumber(t, txMsg, accessListTx, ethTypes.AccessListTxType)
			AssertAccessList(t, txMsg.AccessList)
			AssertReceipt(t, txMsg, rightvrsTxReceipt)
			AssertInnerTxs(t, txMsg, rightvrsTxInnerTxs)
			AssertChangeseet(t, txMsg, rightvrsTxChangeset)
		}
	}

	// Verify header messages
	for i := 1; i <= 10; i++ {
		select {
		case err := <-errorChan:
			t.Fatalf("Received error from consumer: %v", err)
		case rcvHeader := <-headersChan:
			AssertHeader(t, blockHeader, rcvHeader.Header)
			assert.Equal(t, rcvHeader.TxCount, int64(i))
			assert.Equal(t, rcvHeader.Hash, testHash)
		}
	}

	// Verify error trigger messages
	for i := 1; i <= 10; i++ {
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

func TestStressTestKafkaProducer(t *testing.T) {
	rightvrsTx.SetSender(testFromAddr)
	cfg := kafka.KafkaConfig{
		BootstrapServers: []string{"0.0.0.0:9095"},
		BlockTopic:       "xlayer-test-block",
		TxTopic:          "xlayer-test-tx",
		ErrorTopic:       "xlayer-test-error",
		ClientID:         "xlayer-test-consumer",
		GroupID:          "xlayer-test-consumer-1",
	}

	err := createKafkaTopics(cfg)
	assert.NilError(t, err)

	successChan := make(chan struct{}, 10000)
	producer, err := kafka.NewKafkaProducer(cfg, context.Background(), successChan)
	assert.NilError(t, err)

	startTime := time.Now()
	for i := 1; i <= 1000; i++ {
		err = producer.SendKafkaTransaction(uint64(i), rightvrsTx, rightvrsTxReceipt, rightvrsTxInnerTxs, rightvrsTxChangeset)
		assert.NilError(t, err)
	}

	// Sending 1000 messages should not be blocking, and should take less than 50ms
	elapsed := time.Since(startTime)
	fmt.Printf("Batch producer send took %s to dispatch 1000 messages\n", elapsed)
	require.Less(t, elapsed, 50*time.Millisecond)

	for i := 0; i < 1000; i++ {
		select {
		case <-successChan:
		case <-time.After(1 * time.Second):
			t.Fatalf("Timeout waiting for success message %d", i)
		}
	}
	elapsed = time.Since(startTime)
	fmt.Printf("Producer took %s to send 1000 messages to kafka broker\n", elapsed)
	require.Less(t, elapsed, 100*time.Millisecond)

	err = producer.Close()
	assert.NilError(t, err)
}

// createKafkaTopics creates the required Kafka topics for testing
func createKafkaTopics(config kafka.KafkaConfig) error {
	// Create admin client
	adminClient, err := sarama.NewClusterAdmin(config.BootstrapServers, nil)
	if err != nil {
		return err
	}
	defer adminClient.Close()

	// Define topics to create
	topics := []string{config.BlockTopic, config.TxTopic, config.ErrorTopic}

	for _, topic := range topics {
		// Check if topic already exists
		metadata, err := adminClient.DescribeConfig(sarama.ConfigResource{
			Type: sarama.TopicResource,
			Name: topic,
		})

		if err != nil {
			// Topic doesn't exist, create it
			err = adminClient.CreateTopic(topic, &sarama.TopicDetail{
				NumPartitions:     1,
				ReplicationFactor: 1,
			}, false)
			if err != nil {
				return err
			}
		} else {
			// Topic exists, just verify it's accessible
			_ = metadata
		}
	}
	time.Sleep(1 * time.Second)
	return nil
}
