package kafka

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/core/rawdb"
	"github.com/ledgerwatch/erigon/core/types"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

// KafkaProducer represents a Kafka producer client for sending transaction messages
type KafkaProducer struct {
	producer sarama.SyncProducer
	config   KafkaConfig
	ctx      context.Context
	db       kv.RoDB
}

func NewKafkaProducer(config KafkaConfig, ctx context.Context, db kv.RoDB) (*KafkaProducer, error) {
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
		ctx:      ctx,
		db:       db,
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
	_, _, err = client.producer.SendMessage(kafkaMsg)
	if err != nil {
		return fmt.Errorf("error sending message to Kafka: %v", err)
	}

	return nil
}

func (client *KafkaProducer) SendKafkaBlockInfo(header *types.Header) error {
	prevBlockInfo, err := client.getPrevBlockData(header.Number.Uint64())
	if err != nil {
		return err
	}
	msg := kafkaTypes.BlockMessage{
		Header:        header,
		PrevBlockInfo: prevBlockInfo,
	}

	return client.SendKafkaBlockMessage(msg)
}

func (client *KafkaProducer) SendKafkaBlockMessage(msg kafkaTypes.BlockMessage) error {
	// Marshal message to JSON
	jsonData, err := msg.MarshalJSON()
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
	_, _, err = client.producer.SendMessage(kafkaMsg)
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
	_, _, err = client.producer.SendMessage(kafkaMsg)
	if err != nil {
		return fmt.Errorf("error sending message to Kafka: %v", err)
	}

	return nil
}

// getPrevBlockData retrieves the previous block data from the chain db
func (client *KafkaProducer) getPrevBlockData(blockNumber uint64) (*realtimeTypes.BlockInfo, error) {
	if blockNumber <= 1 {
		// Genesis block
		return &realtimeTypes.BlockInfo{
			Header:  nil,
			TxCount: -1,
			Hash:    libcommon.Hash{},
		}, nil
	}

	tx, err := client.db.BeginRo(client.ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	prevBlockNumber := blockNumber - 1
	prevBlock, err := rawdb.ReadBlockByNumber(tx, prevBlockNumber)
	if err != nil {
		return nil, err
	}

	// Get transaction count for the previous block
	prevBlockTxCount := int64(len(prevBlock.Transactions()))

	return &realtimeTypes.BlockInfo{
		Header:  prevBlock.Header(),
		TxCount: prevBlockTxCount,
		Hash:    prevBlock.Hash(),
	}, nil
}
