package natsstream

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
)

// Constants
const (
	DefaultEntryChannelSize = 100000
)

// Errors
var (
	ErrNotStarted = errors.New("client not started")
)

// isFatalError determines if an error should stop processing (like TCP client)
func isFatalError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	// These are sequencing errors that should stop processing to match TCP client behavior
	return errMsg == "unexpected L2 tx entry, found outside of block" ||
		strings.Contains(errMsg, "received new L2 block") && strings.Contains(errMsg, "without proper block end") ||
		strings.Contains(errMsg, "block end number doesn't match block number") ||
		strings.Contains(errMsg, "message missing EntryType header")
}

// NATSClient implements the DatastreamClient interface to read from NATS JetStream
type NATSClient struct {
	// Configuration
	natsURL       string
	streamName    string
	subjectPrefix string
	useTLS        bool
	clientID      string // Unique identifier for this client instance

	// Context for lifecycle management
	parentCtx  context.Context // Parent context from constructor
	ctx        context.Context // Current context, created in Start()
	cancelFunc context.CancelFunc

	// NATS components
	nc  *nats.Conn
	js  jetstream.JetStream
	sub jetstream.Consumer
	kv  jetstream.KeyValue

	// State
	mutex            sync.RWMutex
	started          bool
	reading          atomic.Bool
	readWg           sync.WaitGroup
	entryChan        chan interface{}
	errorChan        chan error
	maxEntryChanSize uint64
	progress         atomic.Uint64
	lastProcessed    atomic.Uint64
	stopReading      atomic.Bool

	// Tracking current fork ID for transactions
	currentFork uint64

	// Cache for latest block
	latestBlockMutex sync.RWMutex
	latestBlock      *types.FullL2Block

	// Logging
	logger      log.Logger
	timeout     time.Duration
	natsManager *Manager
	metadata    *MetadataManager
	consumer    jetstream.Consumer
}

func (c *NATSClient) ExecutePerFile(bookmark *types.BookmarkProto, function func(file *types.FileEntry) error) error {
	return nil // Not implemented in this client
}

// NewNATSClient creates a new NATS client for datastream
func NewNATSClient(ctx context.Context, natsURL string, useTLS bool, natsManager *Manager, logger log.Logger) *NATSClient {
	// Generate unique client ID using timestamp and random component to avoid collisions
	clientID := fmt.Sprintf("client_%d", time.Now().UnixNano())

	return &NATSClient{
		natsURL:          natsURL,
		streamName:       "DATASTREAM",
		subjectPrefix:    "datastream",
		useTLS:           useTLS,
		clientID:         clientID,
		parentCtx:        ctx,
		ctx:              nil, // Will be created in Start()
		cancelFunc:       nil,
		entryChan:        make(chan interface{}, DefaultEntryChannelSize),
		errorChan:        make(chan error, 1), // Buffered to prevent blocking
		maxEntryChanSize: DefaultEntryChannelSize,
		natsManager:      natsManager,
		timeout:          10 * time.Second,
		logger:           logger,
	}
}

// RenewEntryChannel implements DatastreamClient interface
func (c *NATSClient) RenewEntryChannel() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if currently reading
	if c.reading.Load() {
		// Could add warning or handle differently
		c.logger.Warn("Renewing channel while reading is active")
	}

	// Clear existing channel
	close(c.entryChan)
	c.entryChan = make(chan interface{}, DefaultEntryChannelSize)
}

// RenewMaxEntryChannel implements DatastreamClient interface
func (c *NATSClient) RenewMaxEntryChannel() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if currently reading
	if c.reading.Load() {
		// Could add warning or handle differently
		c.logger.Warn("Renewing channel while reading is active")
	}

	// Create a new channel with max size
	close(c.entryChan)
	c.entryChan = make(chan interface{}, c.maxEntryChanSize)
}

// ReadAllEntriesToChannel implements DatastreamClient interface
func (c *NATSClient) ReadAllEntriesToChannel() error {
	c.mutex.Lock()
	started := c.started
	c.mutex.Unlock()

	if !started {
		return ErrNotStarted
	}

	// Atomic check-and-set for reading state
	if !c.reading.CompareAndSwap(false, true) {
		return nil // Already reading, nothing to do
	}

	c.stopReading.Store(false)
	c.readWg.Add(1)

	defer func() {
		c.reading.Store(false)
		c.readWg.Done()
	}()

	c.logger.Info("Starting to read entries from NATS stream",
		"stream", c.streamName,
		"subject", c.subjectPrefix)

	blockNumber, err := c.GetLatestL2BlockNumber()

	blockProgress := c.progress.Load()
	if blockProgress >= blockNumber {
		c.logger.Debug("No new entries to read, current progress matches or exceeds total entries",
			"progress", blockProgress, "blockNumber", blockNumber)
		return nil // Early return - nothing to read
	}

	// Check if there are any entries to read before creating consumer
	totalEntries, err := c.GetTotalEntries()
	if err != nil {
		return fmt.Errorf("Could not get total entry count: %w", err)
	}

	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	var entryProgress uint64 = 0

	if blockProgress > 0 {

		entryProgress, err = c.getEntryNumForBlock(blockProgress, ctx)
		if err != nil {
			return fmt.Errorf("Could not entry number for block : %w", err)
		}
	}

	// Calculate expected entries to process
	var expectedEntries uint64

	expectedEntries = totalEntries - entryProgress
	c.logger.Info("Will read available entries",
		"blockProgress", blockProgress,
		"blockNumber", blockNumber,
		"expectedEntries", expectedEntries)

	if c.consumer != nil {
		consumerName := fmt.Sprintf("DATASTREAM_CONSUMER_%s", c.clientID)
		if err := c.js.DeleteConsumer(c.ctx, c.streamName, consumerName); err != nil {
			c.logger.Warn("Failed to delete durable consumer (may not exist)",
				"consumer", consumerName,
				"error", err)
		}
		c.consumer = nil
	}

	// Create consumer configuration based on current progress
	consumerConfig, err := c.createConsumerConfig()
	if err != nil {
		c.logger.Error("Failed to create consumer config", "error", err)
		return err
	}

	// Create and start the consumer
	msgChan, cleanup, err := c.createStreamConsumer(consumerConfig)
	if err != nil {
		c.logger.Error("Failed to create stream consumer", "error", err)
		return err
	}
	defer cleanup()

	// Process messages synchronously using the unified message processor
	processor := &streamProcessor{
		client:          c,
		msgChan:         msgChan,
		ctx:             c.ctx,
		stopSignal:      &c.stopReading,
		expectedEntries: expectedEntries,
		startProgress:   entryProgress,
		timeout:         c.timeout,
	}

	// Process messages and return any fatal errors
	return c.processMessages(processor)
}

func (c *NATSClient) getEntryNumForBlock(blockProgress uint64, ctx context.Context) (uint64, error) {
	bookmark := types.NewBookmarkProto(blockProgress, datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK)
	marshalledBookmark, err := bookmark.Marshal()
	if err != nil {
		return 0, err
	}

	return c.metadata.GetBookmark(ctx, marshalledBookmark)
}

// processMessages processes messages synchronously and returns fatal errors directly (like TCP client)
func (c *NATSClient) processMessages(processor *streamProcessor) error {
	// For unbounded reading (expectedEntries == 0), set timeouts to prevent infinite loops
	unboundedEmptyTimeout := c.timeout / 2 // Timeout for completely empty streams
	unboundedIdleTimeout := c.timeout      // Timeout for streams with no new messages
	startTime := time.Now()
	lastProcessTime := startTime

	for {
		// Check if we should stop reading
		if processor.stopSignal.Load() {
			c.logger.Info("Stop signal received, exiting read loop")
			break
		}

		// Check if we've processed all expected entries (bounded reading)
		if processor.expectedEntries > 0 && processor.entriesProcessed >= processor.expectedEntries {
			c.logger.Debug("All expected entries processed, exiting read loop",
				"processed", processor.entriesProcessed,
				"expected", processor.expectedEntries)
			break
		}

		// For unbounded reading, handle different timeout scenarios
		if processor.expectedEntries == 0 {
			now := time.Now()

			// If no entries processed and enough time passed, likely empty stream
			if processor.entriesProcessed == 0 && now.Sub(startTime) > unboundedEmptyTimeout {
				c.logger.Debug("Unbounded reading timeout reached with no entries processed, likely empty stream")
				break
			}

			// If we've processed some entries but haven't seen new ones for a while, consider done
			if processor.entriesProcessed > 0 && now.Sub(lastProcessTime) > unboundedIdleTimeout {
				c.logger.Debug("Unbounded reading idle timeout reached, no new messages for a while",
					"processed", processor.entriesProcessed,
					"idleTime", now.Sub(lastProcessTime))
				break
			}
		}

		// Get next message with timeout
		msg, shouldContinue := processor.getNextMessage()
		if !shouldContinue {
			break
		}
		if msg == nil {
			continue // Timeout, check stop signal again
		}

		// Process the message using unified processing
		if err := processor.processMessage(msg); err != nil {
			c.logger.Error("Error processing message", "error", err)

			// Check if this is a fatal error that should stop processing
			if isFatalError(err) {
				// Return fatal error directly (matches TCP client behavior)
				c.logger.Error("Fatal error encountered, stopping processing", "error", err)
				return err
			}

			// Use negative acknowledgment for processing errors
			if nakErr := msg.Nak(); nakErr != nil {
				c.logger.Error("Failed to send negative acknowledgment", "error", nakErr)
			}
		} else {
			// Successfully processed message - update entry count
			processor.entriesProcessed++
			// Note: Block progress (c.progress) is updated separately when L2Blocks are finalized

			// Update last process time for unbounded reading timeout logic
			lastProcessTime = time.Now()

			// Only acknowledge if processing was successful
			if ackErr := msg.Ack(); ackErr != nil {
				c.logger.Error("Failed to acknowledge message", "error", ackErr)
			}
		}
	}

	// Handle any incomplete block before exiting
	if processor.expectingBlockEnd && processor.currentBlock != nil {
		// Match TCP client behavior: log error but don't send incomplete block
		c.logger.Error("Stream ended with incomplete block - not sending partial block",
			"blockNumber", processor.currentBlock.L2BlockNumber,
			"txCount", len(processor.txs),
			"error", "missing L2BlockEnd entry")
		// Do not call finalizeAndSendBlock() - TCP client would not send incomplete blocks
	}

	// Send termination signal to match TCP client behavior
	// This signals to BatchesProcessor that stream reading is complete
	c.logger.Debug("Sending termination signal to processor")
	select {
	case c.entryChan <- nil:
		c.logger.Debug("Termination signal sent successfully")
	default:
		c.logger.Warn("Could not send termination signal, channel full")
		// Try a few more times with backoff (matching TCP client retry logic)
		for retries := 0; retries < 3; retries++ {
			time.Sleep(100 * time.Millisecond)
			select {
			case c.entryChan <- nil:
				c.logger.Debug("Termination signal sent after retry", "retry", retries+1)
				return nil
			default:
				continue
			}
		}
		c.logger.Error("Failed to send termination signal after retries")
	}

	return nil
}

// createConsumerConfig creates a consumer configuration based on current progress
func (c *NATSClient) createConsumerConfig() (jetstream.ConsumerConfig, error) {

	consumerConfig := jetstream.ConsumerConfig{
		Durable:   fmt.Sprintf("DATASTREAM_CONSUMER_%s", c.clientID),
		AckPolicy: jetstream.AckExplicitPolicy,
	}

	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	var entryProgress uint64

	blockProgress := c.progress.Load()
	if blockProgress > 0 {
		entryNumForCurrentBlock, err := c.getEntryNumForBlock(blockProgress, ctx)
		if err != nil {
			return consumerConfig, fmt.Errorf("failed to get entry number for block: %w", err)
		}
		entryProgress, err = c.getLastValidEntryBeforeNextBlock(entryNumForCurrentBlock)

		if err != nil {
			return consumerConfig, fmt.Errorf("failed to get entryprogress for block: %w", err)
		}
	}

	c.logger.Info("Reading from progress position", "progress", entryProgress)

	if entryProgress == 0 {
		consumerConfig.DeliverPolicy = jetstream.DeliverAllPolicy
		c.logger.Info("Starting from beginning of stream (progress=0)")
		return consumerConfig, nil
	}

	consumerConfig.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
	consumerConfig.OptStartSeq = entryProgress
	c.logger.Info("Starting from sequence for block after progress",
		"progress", entryProgress,
		"nextBlock", entryProgress+1,
		"sequence", entryProgress)

	return consumerConfig, nil
}

// GetLastValidEntryBeforeNextBlock scans forward from a given entry position
// and finds the last valid (non-deleted) entry before the next block start bookmark
func (c *NATSClient) getLastValidEntryBeforeNextBlock(startEntryNum uint64) (uint64, error) {
	if !c.started {
		return 0, ErrNotStarted
	}

	// Get total entries to know the bounds
	totalEntries, err := c.GetTotalEntries()
	if err != nil {
		return 0, fmt.Errorf("failed to get total entries: %w", err)
	}

	lastValidSeq := startEntryNum
	currentEntryNum := startEntryNum + 1

	// Get deleted sequences once
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	info, err := c.natsManager.mainStream.Info(ctx)
	cancel()

	if err != nil {
		return 0, fmt.Errorf("failed to get stream info: %w", err)
	}

	// Convert to map for O(1) lookups
	deletedSeqs := make(map[uint64]bool, len(info.State.Deleted))
	for _, seq := range info.State.Deleted {
		deletedSeqs[seq] = true
	}

	// Scan forward looking for next block start bookmark
	for currentEntryNum < totalEntries {

		ctx, cancel := context.WithTimeout(c.ctx, 20*time.Second)
		msg, err := c.natsManager.mainStream.GetMsg(ctx, currentEntryNum)
		cancel()

		if err == nil {

			entryTypeStr := msg.Header.Get("EntryType")

			// Check if this is a block start bookmark
			if entryTypeStr == "176" { // Bookmark entry type
				// Parse the bookmark to see if it's a block start
				bookmark := &datastream.BookMark{}
				if proto.Unmarshal(msg.Data, bookmark) == nil {
					if bookmark.Type == datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK {
						// Found next block start, return the last valid sequence before this
						return lastValidSeq, nil
					}
				}
			}

			// This is a valid (non-deleted) entry, update our last valid sequence
			lastValidSeq = currentEntryNum
		} else {
			// Entry is deleted or doesn't exist, continue scanning but don't update lastValidSeq
			c.logger.Debug("Skipping deleted/missing entry while scanning",
				"entryNum", currentEntryNum)
		}

		currentEntryNum++
	}

	// Reached end of stream without finding next block start, return last valid sequence
	return lastValidSeq, nil
}

// createStreamConsumer creates a NATS consumer and message channel
func (c *NATSClient) createStreamConsumer(consumerConfig jetstream.ConsumerConfig) (
	<-chan jetstream.Msg, func(), error) {

	consumer, err := c.js.CreateOrUpdateConsumer(c.ctx, c.streamName, consumerConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	// Use Consume pattern which allows context cancellation
	msgChan := make(chan jetstream.Msg, 100)
	consumeCtx, cancelConsume := context.WithCancel(c.ctx)

	// Start consuming messages
	consumerCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		select {
		case msgChan <- msg:
		case <-consumeCtx.Done():
		}
	}, jetstream.ConsumeErrHandler(func(consumeCtx jetstream.ConsumeContext, err error) {
		c.logger.Error("Consumer error", "error", err)
	}))
	if err != nil {
		cancelConsume()
		return nil, nil, fmt.Errorf("failed to start consuming: %w", err)
	}

	cleanup := func() {
		cancelConsume()
		consumerCtx.Stop()
	}

	c.consumer = consumer // Store consumer for later use

	return msgChan, cleanup, nil
}

// streamProcessor handles the unified message processing for the read loop
type streamProcessor struct {
	client     *NATSClient
	msgChan    <-chan jetstream.Msg
	ctx        context.Context
	stopSignal *atomic.Bool
	timeout    time.Duration

	// Bounded reading state
	expectedEntries  uint64 // Number of entries expected to read (0 means unknown/unbounded)
	startProgress    uint64 // Progress at start of reading
	entriesProcessed uint64 // Number of entries processed so far

	// State for L2 block building
	currentBlock      *types.FullL2Block
	txs               []types.L2TransactionProto
	expectingBlockEnd bool
}

// getNextMessage retrieves the next message with timeout and stop signal checking
func (p *streamProcessor) getNextMessage() (jetstream.Msg, bool) {
	select {
	case <-p.ctx.Done():
		p.client.logger.Info("Context cancelled, stopping reading entries")
		return nil, false
	case <-time.After(100 * time.Millisecond):
		// Check stop signal periodically
		if p.stopSignal.Load() {
			p.client.logger.Info("Stop signal detected during timeout")
			return nil, false
		}
		return nil, true // Continue with timeout
	case msg := <-p.msgChan:
		return msg, true
	}
}

// processMessage processes a single message using the unified entry processor
func (p *streamProcessor) processMessage(msg jetstream.Msg) error {
	// Get entry type from header
	entryTypeStr := msg.Headers().Get("EntryType")
	if entryTypeStr == "" {
		return fmt.Errorf("message missing EntryType header")
	}

	entryTypeInt, err := strconv.Atoi(entryTypeStr)
	if err != nil {
		return fmt.Errorf("invalid EntryType header: %w", err)
	}

	entryType := types.EntryType(entryTypeInt)

	// Process using unified message handlers
	switch entryType {
	case types.EntryTypeL2Block:
		return p.handleStreamL2Block(msg)
	case types.EntryTypeL2Tx:
		return p.handleStreamL2Tx(msg)
	case types.EntryTypeL2BlockEnd:
		return p.handleStreamL2BlockEnd(msg)
	case types.EntryTypeBatchStart:
		return p.handleStreamBatchStart(msg)
	case types.EntryTypeBatchEnd:
		return p.handleStreamBatchEnd(msg)
	case types.EntryTypeGerUpdate:
		return p.handleStreamGerUpdate(msg)
	case types.BookmarkEntryType:
		return nil
	default:
		p.client.logger.Debug("Ignoring unknown entry type", "type", entryType)
		return nil
	}
}

// handleStreamL2Block processes L2Block entries for the stream
func (p *streamProcessor) handleStreamL2Block(msg jetstream.Msg) error {
	// Match TCP client behavior: error if we receive a new block without proper end for previous block
	if p.expectingBlockEnd && p.currentBlock != nil {
		return fmt.Errorf("received new L2 block %d without proper block end for previous block %d",
			getBlockNumberFromMessage(msg), p.currentBlock.L2BlockNumber)
	}

	// Process new block
	block, err := p.client.processL2Block(msg)
	if err != nil {
		return fmt.Errorf("error processing L2 block: %w", err)
	}

	p.currentBlock = block
	p.txs = make([]types.L2TransactionProto, 0)
	p.expectingBlockEnd = true

	p.client.logger.Debug("Processing L2 block",
		"blockNumber", block.L2BlockNumber,
		"batchNumber", block.BatchNumber)

	return nil
}

// getBlockNumberFromMessage extracts block number from message for error reporting
func getBlockNumberFromMessage(msg jetstream.Msg) uint64 {
	if block, err := types.UnmarshalL2Block(msg.Data()); err == nil {
		return block.L2BlockNumber
	}
	return 0 // fallback if unmarshaling fails
}

// handleStreamL2Tx processes L2Tx entries for the stream
func (p *streamProcessor) handleStreamL2Tx(msg jetstream.Msg) error {
	if !p.expectingBlockEnd || p.currentBlock == nil {
		return fmt.Errorf("unexpected L2 tx entry, found outside of block")
	}

	tx, err := p.client.processL2Transaction(msg)
	if err != nil {
		return fmt.Errorf("error processing L2 transaction: %w", err)
	}

	p.txs = append(p.txs, *tx)
	p.client.logger.Debug("Added transaction to current block",
		"blockNumber", p.currentBlock.L2BlockNumber,
		"txCount", len(p.txs))

	return nil
}

// handleStreamL2BlockEnd processes L2BlockEnd entries for the stream
func (p *streamProcessor) handleStreamL2BlockEnd(msg jetstream.Msg) error {
	if !p.expectingBlockEnd || p.currentBlock == nil {
		p.client.logger.Warn("Received L2 block end without a block")
		return nil
	}

	blockEnd, err := p.client.processL2BlockEnd(msg)
	if err != nil {
		return fmt.Errorf("error processing L2 block end: %w", err)
	}

	if blockEnd.Number != p.currentBlock.L2BlockNumber {
		return fmt.Errorf("block end number doesn't match block number: expected %d, got %d",
			p.currentBlock.L2BlockNumber, blockEnd.Number)
	}

	// Finalize and send the complete block
	if err := p.finalizeAndSendBlock(); err != nil {
		return err
	}

	p.expectingBlockEnd = false
	p.currentBlock = nil
	p.txs = nil

	return nil
}

// handleStreamBatchStart processes BatchStart entries for the stream
func (p *streamProcessor) handleStreamBatchStart(msg jetstream.Msg) error {
	batchStart, err := types.UnmarshalBatchStart(msg.Data())
	if err != nil {
		return fmt.Errorf("error unmarshaling batch start: %w", err)
	}

	// Update current fork ID
	p.client.currentFork = batchStart.ForkId

	p.client.logger.Debug("Processed batch start",
		"batchNumber", batchStart.Number,
		"forkId", batchStart.ForkId)

	return p.sendToChannel(batchStart)
}

// handleStreamBatchEnd processes BatchEnd entries for the stream
func (p *streamProcessor) handleStreamBatchEnd(msg jetstream.Msg) error {
	// Match TCP client behavior: BatchEnd can terminate an incomplete block
	if p.expectingBlockEnd && p.currentBlock != nil {
		p.client.logger.Debug("BatchEnd encountered, finalizing current block",
			"blockNumber", p.currentBlock.L2BlockNumber,
			"txCount", len(p.txs))

		// Finalize and send the current block (TCP client allows this)
		if err := p.finalizeAndSendBlock(); err != nil {
			return err
		}

		p.expectingBlockEnd = false
		p.currentBlock = nil
		p.txs = nil
	}

	batchEnd, err := types.UnmarshalBatchEnd(msg.Data())
	if err != nil {
		return fmt.Errorf("error unmarshaling batch end: %w", err)
	}

	p.client.logger.Debug("Processed batch end", "batchNumber", batchEnd.Number)

	return p.sendToChannel(batchEnd)
}

// handleStreamGerUpdate processes GerUpdate entries for the stream
func (p *streamProcessor) handleStreamGerUpdate(msg jetstream.Msg) error {
	gerUpdate, err := types.DecodeGerUpdateProto(msg.Data())
	if err != nil {
		return fmt.Errorf("error unmarshaling GER update: %w", err)
	}

	p.client.logger.Debug("Processed GER update")

	return p.sendToChannel(gerUpdate)
}

// finalizeAndSendBlock finalizes the current block and sends it to the channel
func (p *streamProcessor) finalizeAndSendBlock() error {
	if p.currentBlock == nil {
		return nil
	}

	// Finalize block processing
	finalizedBlock := p.client.finalizeL2Block(p.currentBlock, p.txs)

	// Send to channel
	err := p.sendToChannel(finalizedBlock)
	if err != nil {
		return err
	}

	// Update progress with the actual block number after successful processing
	p.client.progress.Store(finalizedBlock.L2BlockNumber)
	p.client.logger.Debug("Updated block progress", "blockNumber", finalizedBlock.L2BlockNumber)

	return nil
}

// sendToChannel sends an entry to the output channel with timeout and context cancellation support
func (p *streamProcessor) sendToChannel(entry interface{}) error {
	select {
	case p.client.entryChan <- entry:
		switch e := entry.(type) {
		case *types.FullL2Block:
			p.client.logger.Debug("Sent L2 block to channel",
				"blockNumber", e.L2BlockNumber,
				"txCount", len(e.L2Txs))
		default:
			p.client.logger.Debug("Sent entry to channel", "type", fmt.Sprintf("%T", entry))
		}
		return nil
	case <-time.After(p.timeout):
		return fmt.Errorf("timeout sending to channel after %v", p.timeout)
	case <-p.ctx.Done():
		return fmt.Errorf("context cancelled while sending to channel")
	}
}

// StopReadingToChannel implements DatastreamClient interface
func (c *NATSClient) StopReadingToChannel() {
	c.stopReading.Store(true)

	if c.reading.Load() {
		c.logger.Info("Waiting for read loop to complete")
		c.readWg.Wait()
	}
}

// GetEntryChan implements DatastreamClient interface
func (c *NATSClient) GetEntryChan() *chan interface{} {
	return &c.entryChan
}

// processBookmark processes a bookmark message and returns the unmarshaled bookmark
// This abstracts the bookmark processing logic to be used by both GetL2BlockByNumber and readEntriesLoop
func (c *NATSClient) processBookmark(msg jetstream.Msg) (*types.BookmarkProto, error) {
	bookmark, err := types.UnmarshalBookmark(msg.Data())
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling bookmark: %w", err)
	}

	c.logger.Debug("Processing bookmark",
		"type", bookmark.BookmarkType(),
		"value", bookmark.BookMark.GetValue())

	return bookmark, nil
}

// processL2Transaction processes an L2 transaction message and returns the unmarshaled transaction
// This abstracts the transaction processing logic to be used by both GetL2BlockByNumber and readEntriesLoop
func (c *NATSClient) processL2Transaction(msg jetstream.Msg) (*types.L2TransactionProto, error) {
	// Get data from message - acknowledgment is handled by the caller
	data := msg.Data()

	// Unmarshal transaction
	tx, err := types.UnmarshalTx(data)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling L2 transaction: %w", err)
	}

	return tx, nil
}

// processL2Block processes an L2 block message and returns the unmarshaled block
// This abstracts the block processing logic to be used by both GetL2BlockByNumber and readEntriesLoop
func (c *NATSClient) processL2Block(msg jetstream.Msg) (*types.FullL2Block, error) {
	// Unmarshal block from message data
	block, err := types.UnmarshalL2Block(msg.Data())
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling L2 block: %w", err)
	}

	// Set fork ID
	block.ForkId = c.currentFork

	c.logger.Debug("Processing L2 block",
		"blockNumber", block.L2BlockNumber,
		"batchNumber", block.BatchNumber)

	return block, nil
}

// finalizeL2Block adds transactions to a block and updates internal state
// This abstracts the block finalization logic to be used by both GetL2BlockByNumber and readEntriesLoop
func (c *NATSClient) finalizeL2Block(block *types.FullL2Block, txs []types.L2TransactionProto) *types.FullL2Block {
	// Assign transactions to the block
	block.L2Txs = txs

	// Update latest block cache
	c.latestBlockMutex.Lock()
	c.latestBlock = block
	c.latestBlockMutex.Unlock()
	c.lastProcessed.Store(block.L2BlockNumber)

	c.logger.Debug("Finalized L2 block",
		"blockNumber", block.L2BlockNumber,
		"txCount", len(block.L2Txs))

	return block
}

// processL2BlockEnd processes an L2 block end message and returns the unmarshaled block end
// This abstracts the block end processing logic to be used by both GetL2BlockByNumber and readEntriesLoop
func (c *NATSClient) processL2BlockEnd(msg jetstream.Msg) (*types.L2BlockEndProto, error) {
	blockEnd, err := types.UnmarshalL2BlockEnd(msg.Data())
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling L2 block end: %w", err)
	}

	c.logger.Debug("Processing L2 block end", "blockNumber", blockEnd.Number)

	return blockEnd, nil
}

// GetL2BlockByNumber implements DatastreamClient interface
func (c *NATSClient) GetL2BlockByNumber(blockNum uint64) (*types.FullL2Block, error) {
	c.mutex.RLock()
	if !c.started {
		c.mutex.RUnlock()
		return nil, ErrNotStarted
	}
	c.mutex.RUnlock()

	// Create a context with timeout for this operation
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	c.logger.Debug("Getting L2 block by number", "blockNumber", blockNum)

	// Create and get iterator starting from bookmark
	iter, err := c.createIteratorFromBookmark(ctx, blockNum)
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Stop()

	// Find and build the complete block
	return c.findAndBuildBlock(ctx, iter, blockNum)
}

// createIteratorFromBookmark creates a NATS iterator starting from the bookmark for the given block
func (c *NATSClient) createIteratorFromBookmark(ctx context.Context, blockNum uint64) (jetstream.MessagesContext, error) {
	// Create a bookmark for this block
	bookmark := types.NewBookmarkProto(blockNum, datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK)
	bookmarkBytes, err := bookmark.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bookmark: %w", err)
	}

	// Try to get the bookmark from the KV store using hex-encoded key
	bookmarkKey := fmt.Sprintf("%x", bookmarkBytes)
	entry, err := c.GetKeyValue(ctx, bookmarkKey)
	if err != nil {
		c.logger.Error("Error looking up bookmark",
			"blockNumber", blockNum,
			"error", err,
			"errorType", fmt.Sprintf("%T", err))
		return nil, fmt.Errorf("failed to lookup bookmark for block %d: %w", blockNum, err)
	}

	// Found bookmark - use its entry number to calculate sequence
	entryNum := binary.BigEndian.Uint64(entry)
	startSeq := entryNum + 1 // NATS sequences are 1-based

	c.logger.Debug("Found bookmark for block",
		"blockNumber", blockNum,
		"entryNum", entryNum,
		"sequence", startSeq)

	// Create an ephemeral consumer that starts from the bookmark sequence
	consumerConfig := jetstream.ConsumerConfig{
		AckPolicy:         jetstream.AckExplicitPolicy,
		MaxDeliver:        1,
		DeliverPolicy:     jetstream.DeliverByStartSequencePolicy,
		OptStartSeq:       startSeq,
		InactiveThreshold: 30 * time.Second, // Auto-cleanup after 30s of inactivity
	}

	// Create a temporary consumer for this query
	consumer, err := c.js.CreateOrUpdateConsumer(ctx, c.streamName, consumerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	// Get messages iterator
	iter, err := consumer.Messages()
	if err != nil {
		return nil, fmt.Errorf("failed to get messages iterator: %w", err)
	}

	return iter, nil
}

// findAndBuildBlock finds the target block and builds the complete block with all transactions
func (c *NATSClient) findAndBuildBlock(ctx context.Context, iter jetstream.MessagesContext, blockNum uint64) (*types.FullL2Block, error) {
	var targetBlock *types.FullL2Block
	var txs []types.L2TransactionProto

	// State machine for block building
	state := blockSearchState{
		targetBlockNum: blockNum,
		phase:          phaseSearchingBlock,
	}

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context deadline exceeded while searching for block %d", blockNum)
		default:
		}

		msg, err := iter.Next()
		if err != nil {
			if errors.Is(err, jetstream.ErrMsgIteratorClosed) || errors.Is(err, context.DeadlineExceeded) {
				if state.phase == phaseFoundBlock && targetBlock != nil {
					// We found the block but reached end without proper block end
					c.logger.Warn("Reached end of stream without finding block end",
						"blockNumber", blockNum, "txCount", len(txs))
					break
				}
				return nil, fmt.Errorf("reached end of stream without finding block %d", blockNum)
			}
			return nil, fmt.Errorf("error reading next message: %w", err)
		}

		// Process the message based on current state
		nextState, block, tx, shouldStop, err := c.processMessageForBlock(msg, &state)
		if err != nil {
			msg.Ack()
			return nil, err
		}

		state = nextState

		// Update block and transactions based on state
		if block != nil {
			targetBlock = block
		}
		if tx != nil {
			txs = append(txs, *tx)
		}

		msg.Ack()

		if shouldStop {
			break
		}
	}

	if targetBlock == nil {
		return nil, fmt.Errorf("block %d not found in stream", blockNum)
	}

	// Finalize the block with its transactions
	finalBlock := c.finalizeL2Block(targetBlock, txs)
	c.logger.Info("Retrieved L2 block with transactions",
		"blockNumber", finalBlock.L2BlockNumber,
		"batchNumber", finalBlock.BatchNumber,
		"txCount", len(finalBlock.L2Txs))

	return finalBlock, nil
}

// blockSearchPhase represents the current phase of block searching
type blockSearchPhase int

const (
	phaseSearchingBlock blockSearchPhase = iota
	phaseFoundBlock
	phaseComplete
)

// blockSearchState tracks the state machine for block building
type blockSearchState struct {
	targetBlockNum uint64
	phase          blockSearchPhase
}

// processMessageForBlock processes a single message during block search and returns the next state
func (c *NATSClient) processMessageForBlock(msg jetstream.Msg, state *blockSearchState) (
	blockSearchState, *types.FullL2Block, *types.L2TransactionProto, bool, error) {

	// Get entry type from header
	entryTypeStr := msg.Headers().Get("EntryType")
	if entryTypeStr == "" {
		return *state, nil, nil, false, fmt.Errorf("message missing EntryType header")
	}

	entryTypeInt, err := strconv.Atoi(entryTypeStr)
	if err != nil {
		return *state, nil, nil, false, fmt.Errorf("invalid EntryType header: %w", err)
	}

	entryType := types.EntryType(entryTypeInt)

	switch entryType {
	case types.EntryTypeL2Block:
		return c.handleL2BlockMessage(msg, state)

	case types.EntryTypeL2Tx:
		return c.handleL2TxMessage(msg, state)

	case types.EntryTypeL2BlockEnd:
		return c.handleL2BlockEndMessage(msg, state)

	case types.BookmarkEntryType:
		return c.handleBookmarkMessage(msg, state)

	case types.EntryTypeBatchEnd:
		return c.handleBatchEndMessage(msg, state)

	default:
		// Skip unknown entry types
		c.logger.Debug("Skipping unknown entry type", "type", entryType)
		return *state, nil, nil, false, nil
	}
}

// handleL2BlockMessage handles L2Block entry type during block search
func (c *NATSClient) handleL2BlockMessage(msg jetstream.Msg, state *blockSearchState) (
	blockSearchState, *types.FullL2Block, *types.L2TransactionProto, bool, error) {

	block, err := c.processL2Block(msg)
	if err != nil {
		return *state, nil, nil, false, err
	}

	if state.phase == phaseSearchingBlock {
		if block.L2BlockNumber == state.targetBlockNum {
			// Found our target block
			newState := blockSearchState{
				targetBlockNum: state.targetBlockNum,
				phase:          phaseFoundBlock,
			}
			c.logger.Debug("Found target L2 block",
				"blockNumber", block.L2BlockNumber,
				"batchNumber", block.BatchNumber)
			return newState, block, nil, false, nil
		} else if block.L2BlockNumber > state.targetBlockNum {
			// We've passed the target block - it doesn't exist
			return *state, nil, nil, false, fmt.Errorf("block %d not found (found block %d instead)",
				state.targetBlockNum, block.L2BlockNumber)
		}
		// Continue searching if block number is less than target
	} else if state.phase == phaseFoundBlock {
		// Found another block after our target - we're done
		newState := blockSearchState{
			targetBlockNum: state.targetBlockNum,
			phase:          phaseComplete,
		}
		c.logger.Debug("Found next L2 block, ending current block processing",
			"currentBlock", state.targetBlockNum,
			"nextBlock", block.L2BlockNumber)
		return newState, nil, nil, true, nil
	}

	return *state, nil, nil, false, nil
}

// handleL2TxMessage handles L2Tx entry type during block search
func (c *NATSClient) handleL2TxMessage(msg jetstream.Msg, state *blockSearchState) (
	blockSearchState, *types.FullL2Block, *types.L2TransactionProto, bool, error) {

	if state.phase != phaseFoundBlock {
		// Ignore transactions if we haven't found our target block yet
		return *state, nil, nil, false, nil
	}

	tx, err := c.processL2Transaction(msg)
	if err != nil {
		return *state, nil, nil, false, err
	}

	c.logger.Debug("Added transaction to block",
		"blockNumber", state.targetBlockNum,
		"txIndex", tx.Index)

	return *state, nil, tx, false, nil
}

// handleL2BlockEndMessage handles L2BlockEnd entry type during block search
func (c *NATSClient) handleL2BlockEndMessage(msg jetstream.Msg, state *blockSearchState) (
	blockSearchState, *types.FullL2Block, *types.L2TransactionProto, bool, error) {

	blockEnd, err := c.processL2BlockEnd(msg)
	if err != nil {
		return *state, nil, nil, false, err
	}

	if state.phase == phaseFoundBlock && blockEnd.Number == state.targetBlockNum {
		// Found the end of our target block
		newState := blockSearchState{
			targetBlockNum: state.targetBlockNum,
			phase:          phaseComplete,
		}
		c.logger.Debug("Found L2 block end", "blockNumber", blockEnd.Number)
		return newState, nil, nil, true, nil
	}

	return *state, nil, nil, false, nil
}

// handleBookmarkMessage handles Bookmark entry type during block search
func (c *NATSClient) handleBookmarkMessage(msg jetstream.Msg, state *blockSearchState) (
	blockSearchState, *types.FullL2Block, *types.L2TransactionProto, bool, error) {

	bookmark, err := c.processBookmark(msg)
	if err != nil {
		return *state, nil, nil, false, err
	}

	if state.phase == phaseFoundBlock &&
		bookmark.BookmarkType() == datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK {
		// Found another L2 block bookmark - we're done with current block
		newState := blockSearchState{
			targetBlockNum: state.targetBlockNum,
			phase:          phaseComplete,
		}
		c.logger.Debug("Found next L2 block bookmark, ending current block processing")
		return newState, nil, nil, true, nil
	}

	return *state, nil, nil, false, nil
}

// handleBatchEndMessage handles BatchEnd entry type during block search
func (c *NATSClient) handleBatchEndMessage(msg jetstream.Msg, state *blockSearchState) (
	blockSearchState, *types.FullL2Block, *types.L2TransactionProto, bool, error) {

	if state.phase == phaseFoundBlock {
		// Found batch end after our target block - we're done
		newState := blockSearchState{
			targetBlockNum: state.targetBlockNum,
			phase:          phaseComplete,
		}
		c.logger.Debug("Found batch end, ending current block processing")
		return newState, nil, nil, true, nil
	}

	return *state, nil, nil, false, nil
}

// GetLatestL2Block implements DatastreamClient interface
func (c *NATSClient) GetLatestL2Block() (*types.FullL2Block, error) {
	c.mutex.RLock()
	if !c.started {
		c.mutex.RUnlock()
		return nil, ErrNotStarted
	}
	c.mutex.RUnlock()

	blockNum, err := c.GetLatestL2BlockNumber()
	if err != nil {
		return nil, fmt.Errorf("failed to get latest L2 block number: %w", err)
	}

	return c.GetL2BlockByNumber(blockNum)
}

func (c *NATSClient) GetLatestL2BlockNumber() (uint64, error) {

	// Try to get the latest block bookmark from KV store
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	// Get the latest block bookmark
	bookmarkData, err := c.GetKeyValue(ctx, MetadataLatestBlockBookmark)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest block bookmark: %w", err)
	}

	// Unmarshal the bookmark to get the block number
	bookmark := &datastream.BookMark{}
	if err := proto.Unmarshal(bookmarkData, bookmark); err != nil {
		return 0, fmt.Errorf("failed to unmarshal bookmark: %w", err)
	}

	return bookmark.Value, nil
}

// GetProgressAtomic implements DatastreamClient interface
func (c *NATSClient) GetProgressAtomic() *atomic.Uint64 {
	return &c.progress
}

// Start implements DatastreamClient interface
func (c *NATSClient) Start() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.started {
		return nil // Already started, nothing to do
	}

	// Create fresh context for this start cycle
	// This ensures cancelled contexts from previous Stop() calls don't interfere
	c.ctx, c.cancelFunc = context.WithCancel(c.parentCtx)
	c.logger.Debug("Created fresh context for NATS client start")

	// Connect to NATS
	opts := []nats.Option{
		nats.Name("erigon-datastream-client"),
		nats.ReconnectWait(1 * time.Second),
		nats.MaxReconnects(-1),
		nats.Timeout(c.timeout),
	}

	if c.useTLS {
		opts = append(opts, nats.Secure())
	}

	nc, err := nats.Connect(c.natsURL, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	c.nc = nc

	// Create JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}
	c.js = js

	// Verify stream exists
	_, err = js.Stream(c.ctx, c.streamName)
	if err != nil {
		nc.Close()
		return fmt.Errorf("stream not found: %w", err)
	}

	c.started = true
	c.logger.Info("NATS datastream client started",
		"url", c.natsURL,
		"stream", c.streamName)

	return nil
}

// Stop implements DatastreamClient interface
func (c *NATSClient) Stop() error {
	c.mutex.Lock()
	if !c.started {
		c.mutex.Unlock()
		return nil
	}
	c.mutex.Unlock()

	// Stop reading first
	c.stopReading.Store(true)

	// Wait for read loop to complete
	c.readWg.Wait()

	// Close NATS connection and update state
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.js != nil {
		consumerName := fmt.Sprintf("DATASTREAM_CONSUMER_%s", c.clientID)
		if err := c.js.DeleteConsumer(c.ctx, c.streamName, consumerName); err != nil {
			c.logger.Warn("Failed to delete durable consumer (may not exist)",
				"consumer", consumerName,
				"error", err)
		}
	}

	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	if c.nc != nil {
		c.nc.Close()
		c.nc = nil
	}

	c.kv = nil
	c.js = nil

	// Clear context and cancel function to force recreation on next Start()
	c.ctx = nil
	c.cancelFunc = nil
	c.started = false
	c.logger.Info("NATS datastream client stopped")

	return nil
}

// HandleStart implements DatastreamClient interface
func (c *NATSClient) HandleStart() error {
	return c.Start()
}

// GetTotalEntries retrieves the total number of entries in the stream from the KV store
func (c *NATSClient) GetTotalEntries() (uint64, error) {
	if !c.started {
		return 0, ErrNotStarted
	}

	// Create a context with timeout for this operation
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	// Use the metadata key to get total entries
	entry, err := c.GetKeyValue(ctx, MetadataTotalEntriesKey)
	if err != nil {
		// No fallback - KV store is the source of truth
		if errors.Is(err, nats.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyNotFound) {
			return 0, fmt.Errorf("METADATA_TOTAL_ENTRIES not found in KV store - this indicates a critical issue with datastream state management")
		}
		return 0, fmt.Errorf("failed to get total entries from KV: %w", err)
	}

	// Validate data length before conversion
	value := entry
	if len(value) < 8 {
		return 0, fmt.Errorf("corrupted METADATA_TOTAL_ENTRIES: expected 8 bytes, got %d bytes", len(value))
	}

	// Convert bytes to uint64
	totalEntries := binary.BigEndian.Uint64(value)

	c.logger.Debug("Retrieved total entries from KV store",
		"totalEntries", totalEntries)

	return totalEntries, nil
}

func (c *NATSClient) GetKeyValue(ctx context.Context, key string) ([]byte, error) {
	kvctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if c.natsManager == nil {
		return nil, fmt.Errorf("NATS manager not initialized")
	}

	var err error
	if c.metadata == nil {
		c.metadata, err = NewMetadataManager(kvctx, c.natsManager, c.logger)
		if err != nil {
			c.logger.Error("Failed to initialize NATS metadata manager", "error", err)
			return nil, fmt.Errorf("metadata manager initialization failed: %w", err)
		}
	}

	return c.metadata.GetValue(kvctx, key)
}

// CreateNATSDatastreamClient is a factory function that can be used to create a DatastreamClient
// implementation that reads from NATS JetStream.
func CreateNATSDatastreamClient(ctx context.Context, manager *Manager, natsURL string, useTLS bool, timeout time.Duration, maxChannelSize uint64) types.DatastreamClient {
	logger := log.Root()
	client := NewNATSClient(ctx, natsURL, useTLS, manager, logger)
	return client
}
