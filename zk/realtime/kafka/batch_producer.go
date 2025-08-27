package kafka

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/ledgerwatch/log/v3"
)

type BatchProducer struct {
	ctx      context.Context
	producer sarama.AsyncProducer
	buffer   chan *sarama.ProducerMessage
	wg       sync.WaitGroup
	done     chan struct{}
}

func NewBatchProducer(ctx context.Context, config KafkaConfig, successChan chan struct{}) (*BatchProducer, error) {
	saramaConfig := sarama.NewConfig()
	saramaConfig.Version = DEFAULT_VERSION
	saramaConfig.ClientID = config.ClientID
	saramaConfig.Producer.Return.Successes = true
	saramaConfig.Producer.Return.Errors = true

	// For AsyncProducer
	saramaConfig.Producer.Flush.Messages = 100
	saramaConfig.Producer.Flush.Frequency = 3 * time.Millisecond
	saramaConfig.Producer.Flush.MaxMessages = 0
	saramaConfig.Producer.Compression = sarama.CompressionSnappy

	if err := verifyProducerConfig(saramaConfig); err != nil {
		return nil, err
	}

	producer, err := sarama.NewAsyncProducer(config.BootstrapServers, saramaConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating Kafka producer: %v", err)
	}

	bp := &BatchProducer{
		ctx:      ctx,
		producer: producer,
		buffer:   make(chan *sarama.ProducerMessage, 1000),
		wg:       sync.WaitGroup{},
		done:     make(chan struct{}),
	}

	// Start the background goroutine that handles message forwarding
	bp.wg.Add(2)
	go bp.handle()
	go bp.handleResults(successChan)

	return bp, nil
}

func (bp *BatchProducer) Close() error {
	close(bp.done)
	err := bp.producer.Close()
	bp.wg.Wait()
	return err
}

func verifyProducerConfig(config *sarama.Config) error {
	if !config.Producer.Return.Errors {
		return sarama.ConfigurationError("Producer.Return.Errors must be true to be used in a SyncProducer")
	}
	if !config.Producer.Return.Successes {
		return sarama.ConfigurationError("Producer.Return.Successes must be true to be used in a SyncProducer")
	}
	return nil
}

// SendMessage queues a message for production without waiting for results
func (bp *BatchProducer) SendMessage(msg *sarama.ProducerMessage) error {
	select {
	case <-bp.ctx.Done():
		return fmt.Errorf("context done, stopping")
	case <-bp.done:
		return fmt.Errorf("producer is closed")
	case bp.buffer <- msg:
		return nil
	default:
		return fmt.Errorf("buffer is full, cannot queue message")
	}
}

func (bp *BatchProducer) handle() {
	defer bp.wg.Done()
	for {
		select {
		case <-bp.ctx.Done():
			return
		case <-bp.done:
			return
		case msg := <-bp.buffer:
			select {
			// Queue message to kafka broker producer
			case bp.producer.Input() <- msg:
			case <-bp.ctx.Done():
				return
			}
		}
	}
}

func (bp *BatchProducer) handleResults(successChan chan struct{}) {
	defer bp.wg.Done()
	for {
		select {
		case <-bp.ctx.Done():
			return
		case <-bp.done:
			return
		case <-bp.producer.Successes():
			if successChan != nil {
				successChan <- struct{}{}
			}
		case err := <-bp.producer.Errors():
			log.Error("[Realtime] error sending message to kafka", "error", err)
		}
	}
}
