package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IBM/sarama"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/types"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

// KafkaProducer represents a Kafka producer client for sending transaction messages
type KafkaProducer struct {
	producer *BatchProducer
	config   KafkaConfig
	ctx      context.Context
}

func NewKafkaProducer(config KafkaConfig, ctx context.Context, successChan chan struct{}) (*KafkaProducer, error) {
	// Create sync producer
	producer, err := NewBatchProducer(ctx, config, successChan)
	if err != nil {
		return nil, err
	}

	return &KafkaProducer{
		producer: producer,
		config:   config,
		ctx:      ctx,
	}, nil
}

func (client *KafkaProducer) Close() error {
	return client.producer.Close()
}

func (client *KafkaProducer) SendKafkaTransaction(blockNumber uint64, tx types.Transaction, receipt *types.Receipt, innerTxs []*zktypes.InnerTx, changeset *realtimeTypes.Changeset) error {
	msg, err := kafkaTypes.ToKafkaTransactionMessage(tx, receipt, innerTxs, changeset, blockNumber)
	if err != nil {
		return fmt.Errorf("SendKafkaTransaction error: %v", err)
	}

	// Marshal message to JSON
	jsonData, err := msg.MarshalJSON()
	if err != nil {
		return fmt.Errorf("error marshaling transaction message: %v", err)
	}

	// Create Kafka message
	kafkaMsg := &sarama.ProducerMessage{
		Topic: client.config.TxTopic,
		Value: sarama.StringEncoder(jsonData),
		Key:   sarama.StringEncoder(tx.Hash().String()),
	}

	// Send message
	err = client.producer.SendMessage(kafkaMsg)
	if err != nil {
		return fmt.Errorf("error sending message to Kafka: %v", err)
	}

	return nil
}

func (client *KafkaProducer) SendKafkaNewBlockInfo(header *types.Header) error {
	msg := &realtimeTypes.BlockInfo{
		Header:  header,
		TxCount: -1,
		Hash:    libcommon.Hash{},
	}

	return client.SendKafkaBlockMessage(msg)
}

func (client *KafkaProducer) SendKafkaConfirmedBlockInfo(block *types.Block) error {
	// Get transaction count for the block
	blockTxCount := int64(len(block.Transactions()))
	msg := &realtimeTypes.BlockInfo{
		Header:  block.Header(),
		TxCount: blockTxCount,
		Hash:    block.Hash(),
	}

	return client.SendKafkaBlockMessage(msg)
}

func (client *KafkaProducer) SendKafkaBlockMessage(msg *realtimeTypes.BlockInfo) error {
	// Marshal message to JSON
	jsonData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("error marshaling block message: %v", err)
	}

	// Create Kafka message
	kafkaMsg := &sarama.ProducerMessage{
		Topic: client.config.BlockTopic,
		Value: sarama.StringEncoder(jsonData),
		Key:   sarama.StringEncoder(msg.Header.Number.String()),
	}

	// Send message
	err = client.producer.SendMessage(kafkaMsg)
	if err != nil {
		return fmt.Errorf("error sending message to Kafka: %v", err)
	}

	return nil
}

func (client *KafkaProducer) SendKafkaErrorTrigger(blockNumber uint64) error {
	// Create error trigger message
	msg := kafkaTypes.ErrorTriggerMessage{
		BlockNumber: blockNumber,
	}
	jsonData, err := msg.MarshalJSON()
	if err != nil {
		return fmt.Errorf("error marshaling error trigger message: %v", err)
	}

	// Create Kafka message
	kafkaMsg := &sarama.ProducerMessage{
		Topic: client.config.ErrorTopic,
		Value: sarama.StringEncoder(jsonData),
		Key:   sarama.StringEncoder(fmt.Sprintf("%d", blockNumber)),
	}

	// Send message
	err = client.producer.SendMessage(kafkaMsg)
	if err != nil {
		return fmt.Errorf("error sending message to Kafka: %v", err)
	}

	return nil
}
