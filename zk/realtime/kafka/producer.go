package kafka

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"
	"github.com/ledgerwatch/erigon/core/types"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

// KafkaProducer represents a Kafka producer client for sending transaction messages
type KafkaProducer struct {
	producer sarama.SyncProducer
	config   KafkaConfig
}

func NewKafkaProducer(config KafkaConfig) (*KafkaProducer, error) {
	saramaConfig := sarama.NewConfig()
	saramaConfig.Version = DEFAULT_VERSION
	saramaConfig.ClientID = config.ClientID
	saramaConfig.Producer.Return.Successes = true
	saramaConfig.Producer.Return.Errors = true

	// Create sync producer
	producer, err := sarama.NewSyncProducer(config.BootstrapServers, saramaConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating Kafka producer: %v", err)
	}

	return &KafkaProducer{
		producer: producer,
		config:   config,
	}, nil
}

func (client *KafkaProducer) SendKafkaTransaction(ctx context.Context, blockNumber uint64, tx types.Transaction, receipt *types.Receipt, innerTxs []*zktypes.InnerTx, changeset *realtimeTypes.Changeset) error {
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
	_, _, err = client.producer.SendMessage(kafkaMsg)
	if err != nil {
		return fmt.Errorf("error sending message to Kafka: %v", err)
	}

	return nil
}

func (client *KafkaProducer) Close() error {
	return client.producer.Close()
}

func (client *KafkaProducer) SendKafkaBlockInfo(ctx context.Context, header *types.Header, prevBlockTxCount int64) error {
	msg, err := kafkaTypes.ToKafkaBlockMessage(header, prevBlockTxCount)
	if err != nil {
		return fmt.Errorf("SendKafkaBlockInfo error: %v", err)
	}

	// Marshal message to JSON
	jsonData, err := msg.MarshalJSON()
	if err != nil {
		return fmt.Errorf("error marshaling block message: %v", err)
	}

	// Create Kafka message
	kafkaMsg := &sarama.ProducerMessage{
		Topic: client.config.BlockTopic,
		Value: sarama.StringEncoder(jsonData),
		Key:   sarama.StringEncoder(header.Hash().String()),
	}

	// Send message
	_, _, err = client.producer.SendMessage(kafkaMsg)
	if err != nil {
		return fmt.Errorf("error sending message to Kafka: %v", err)
	}

	return nil
}

func (client *KafkaProducer) SendKafkaErrorTrigger(ctx context.Context, blockNumber uint64) error {
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
	_, _, err = client.producer.SendMessage(kafkaMsg)
	if err != nil {
		return fmt.Errorf("error sending message to Kafka: %v", err)
	}

	return nil
}
