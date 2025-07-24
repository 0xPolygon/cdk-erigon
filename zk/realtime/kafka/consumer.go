package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IBM/sarama"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	"github.com/ledgerwatch/erigon/zkevm/log"
)

type KafkaConsumer struct {
	consumer sarama.ConsumerGroup
	config   KafkaConfig
}

func NewKafkaConsumer(config KafkaConfig, latestFlag bool) (*KafkaConsumer, error) {
	saramaConfig := sarama.NewConfig()
	saramaConfig.Version = DEFAULT_VERSION
	saramaConfig.ClientID = config.ClientID
	if latestFlag {
		saramaConfig.Consumer.Offsets.Initial = sarama.OffsetNewest
	} else {
		saramaConfig.Consumer.Offsets.Initial = sarama.OffsetOldest
	}
	saramaConfig.Consumer.Offsets.AutoCommit.Enable = false

	// Create consumer group
	consumerGroup, err := sarama.NewConsumerGroup(config.BootstrapServers, config.GroupID, saramaConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating Kafka consumer: %v", err)
	}

	return &KafkaConsumer{
		consumer: consumerGroup,
		config:   config,
	}, nil
}

type consumerGroupHandler struct {
	ctx           context.Context
	blockMsgsChan chan kafkaTypes.BlockMessage
	txMsgsChan    chan kafkaTypes.TransactionMessage
	errorMsgsChan chan kafkaTypes.ErrorTriggerMessage
	errorChan     chan error
	txTopic       string
	blockTopic    string
	errorTopic    string
}

func (h *consumerGroupHandler) Setup(session sarama.ConsumerGroupSession) error {
	return nil
}

func (h *consumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error {
	return nil
}

func (h *consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	log.Info(fmt.Sprintf("Starting kafka consumption. topic: %s, partition: %d, offset: %d", claim.Topic(), claim.Partition(), claim.InitialOffset()))
	for {
		select {
		case <-h.ctx.Done():
			err := fmt.Errorf("context cancelled - stopping consume claim")
			h.errorChan <- err
			return err
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			switch msg.Topic {
			case h.blockTopic:
				var blockMsg kafkaTypes.BlockMessage
				if err := json.Unmarshal(msg.Value, &blockMsg); err != nil {
					log.Warn(fmt.Sprintf("consume claim error, unmarshaling block message. error: %v", err))
					continue
				}

				// Send message to header channel
				select {
				case h.blockMsgsChan <- blockMsg:
					session.MarkMessage(msg, "")
				case <-h.ctx.Done():
					err := fmt.Errorf("context cancelled - stopping consume claim")
					h.errorChan <- err
					return err
				}
			case h.txTopic:
				var txMsg kafkaTypes.TransactionMessage
				if err := json.Unmarshal(msg.Value, &txMsg); err != nil {
					log.Warn(fmt.Sprintf("consume claim error, unmarshaling transaction message. error: %v", err))
					continue
				}

				// Send message to tx channel
				select {
				case h.txMsgsChan <- txMsg:
					session.MarkMessage(msg, "")
				case <-h.ctx.Done():
					err := fmt.Errorf("context cancelled - stopping consume claim")
					h.errorChan <- err
					return err
				}
			case h.errorTopic:
				var errorMsg kafkaTypes.ErrorTriggerMessage
				if err := json.Unmarshal(msg.Value, &errorMsg); err != nil {
					log.Warn(fmt.Sprintf("consume claim error, unmarshaling error trigger message. error: %v", err))
					continue
				}

				// Send message to error trigger channel
				select {
				case h.errorMsgsChan <- errorMsg:
					session.MarkMessage(msg, "")
				case <-h.ctx.Done():
					err := fmt.Errorf("context cancelled - stopping consume claim")
					h.errorChan <- err
					return err
				}
			default:
				err := fmt.Errorf("unknown topic: %s", msg.Topic)
				h.errorChan <- err
				return err
			}
		}
	}
}

// ConsumeKafka starts consuming kafka messages from the specified topics
func (client *KafkaConsumer) ConsumeKafka(ctx context.Context, blockMsgsChan chan kafkaTypes.BlockMessage, txMsgsChan chan kafkaTypes.TransactionMessage, errorMsgsChan chan kafkaTypes.ErrorTriggerMessage, errorChan chan error) {
	handler := &consumerGroupHandler{
		ctx:           ctx,
		blockMsgsChan: blockMsgsChan,
		txMsgsChan:    txMsgsChan,
		errorMsgsChan: errorMsgsChan,
		errorChan:     errorChan,
		txTopic:       client.config.TxTopic,
		blockTopic:    client.config.BlockTopic,
		errorTopic:    client.config.ErrorTopic,
	}

	topics := []string{client.config.TxTopic, client.config.BlockTopic, client.config.ErrorTopic}
	err := client.consumer.Consume(ctx, topics, handler)
	if err != nil {
		errorChan <- fmt.Errorf("ConsumeKafka error: %v", err)
		return
	}
}

func (client *KafkaConsumer) Close() error {
	return client.consumer.Close()
}
