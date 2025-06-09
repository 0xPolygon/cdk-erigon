package natsstream

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Constants
const (
	DefaultEntryChannelSize = 100000
	DefaultConnectTimeout   = 10 * time.Second
	DefaultReadTimeout      = 5 * time.Second
)

// Errors
var (
	ErrNotConnected     = errors.New("not connected to NATS server")
	ErrNotStarted       = errors.New("client not started")
	ErrAlreadyStarted   = errors.New("client already started")
	ErrStreamNotFound   = errors.New("stream not found")
	ErrNoEntries        = errors.New("no entries available")
	ErrInvalidBookmark  = errors.New("invalid bookmark")
	ErrReadingStopped   = errors.New("reading has been stopped")
	ErrContextCancelled = errors.New("context cancelled")
)

// NATSClient implements the DatastreamClient interface to read from NATS JetStream
type NATSClient struct {
	// Configuration
	natsURL       string
	streamName    string
	subjectPrefix string
	chainID       uint64
	forkID        uint64
	useTLS        bool

	// Context for lifecycle management
	ctx        context.Context
	cancelFunc context.CancelFunc

	// NATS components
	nc  *nats.Conn
	js  jetstream.JetStream
	sub jetstream.Consumer
	kv  jetstream.KeyValue

	// State
	mutex            sync.RWMutex
	started          bool
	reading          bool
	readWg           sync.WaitGroup
	entryChan        chan interface{}
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
	logger log.Logger
}

func (c *NATSClient) ExecutePerFile(bookmark *types.BookmarkProto, function func(file *types.FileEntry) error) error {
	return nil // Not implemented in this client
}

// NewNATSClient creates a new NATS client for datastream
func NewNATSClient(ctx context.Context, natsURL string, useTLS bool, chainID uint64, forkID uint64, logger log.Logger) *NATSClient {
	clientCtx, cancelFunc := context.WithCancel(ctx)

	return &NATSClient{
		natsURL:          natsURL,
		streamName:       fmt.Sprintf("DATASTREAM_%d", chainID),
		subjectPrefix:    fmt.Sprintf("datastream.%d", chainID),
		chainID:          chainID,
		forkID:           forkID,
		currentFork:      forkID,
		useTLS:           useTLS,
		ctx:              clientCtx,
		cancelFunc:       cancelFunc,
		entryChan:        make(chan interface{}, DefaultEntryChannelSize),
		maxEntryChanSize: DefaultEntryChannelSize,
		logger:           logger,
	}
}

// RenewEntryChannel implements DatastreamClient interface
func (c *NATSClient) RenewEntryChannel() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Clear existing channel
	close(c.entryChan)
	c.entryChan = make(chan interface{}, DefaultEntryChannelSize)
}

// RenewMaxEntryChannel implements DatastreamClient interface
func (c *NATSClient) RenewMaxEntryChannel() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Create a new channel with max size
	close(c.entryChan)
	c.entryChan = make(chan interface{}, c.maxEntryChanSize)
}

// ReadAllEntriesToChannel implements DatastreamClient interface
func (c *NATSClient) ReadAllEntriesToChannel() error {
	c.mutex.Lock()
	if !c.started {
		c.mutex.Unlock()
		return ErrNotStarted
	}
	if c.reading {
		c.mutex.Unlock()
		return nil // Already reading, nothing to do
	}
	c.reading = true
	c.stopReading.Store(false)
	c.mutex.Unlock()

	c.readWg.Add(1)
	go c.readEntriesLoop()

	return nil
}

// readEntriesLoop continuously reads messages from the NATS stream
func (c *NATSClient) readEntriesLoop() {
	defer c.readWg.Done()
	defer func() {
		c.mutex.Lock()
		c.reading = false
		c.mutex.Unlock()
	}()

	c.logger.Info("Starting to read entries from NATS stream",
		"stream", c.streamName,
		"subject", c.subjectPrefix)

	// Create consumer configuration based on current progress
	consumerConfig, err := c.createConsumerConfig()
	if err != nil {
		c.logger.Error("Failed to create consumer config", "error", err)
		return
	}

	// Create and start the consumer
	_, msgChan, cleanup, err := c.createStreamConsumer(consumerConfig)
	if err != nil {
		c.logger.Error("Failed to create stream consumer", "error", err)
		return
	}
	defer cleanup()

	// Process messages using the unified message processor
	processor := &streamProcessor{
		client:     c,
		msgChan:    msgChan,
		ctx:        c.ctx,
		stopSignal: &c.stopReading,
	}

	processor.processMessages()

	// Send end-of-stream signal
	c.sendEndOfStreamSignal()
}

// createConsumerConfig creates a consumer configuration based on current progress
func (c *NATSClient) createConsumerConfig() (jetstream.ConsumerConfig, error) {
	progress := c.progress.Load()
	c.logger.Info("Reading from progress position", "progress", progress)

	consumerConfig := jetstream.ConsumerConfig{
		Durable:   fmt.Sprintf("DATASTREAM_CONSUMER_%d", c.chainID),
		AckPolicy: jetstream.AckExplicitPolicy,
	}

	if progress == 0 {
		consumerConfig.DeliverPolicy = jetstream.DeliverAllPolicy
		c.logger.Info("Starting from beginning of stream (progress=0)")
		return consumerConfig, nil
	}

	// If progress > 0, try to find a bookmark for the next block
	// TODO: there is a hole here, if a Truncate has been called on
	// the stream there may be deleted sequence numbers so progress + 1 might be invalid
	bookmark := types.NewBookmarkProto(progress+1, datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK)
	bookmarkBytes, err := bookmark.Marshal()
	if err != nil {
		return consumerConfig, fmt.Errorf("failed to marshal bookmark: %w", err)
	}

	// Try to get the bookmark from KV store using hex-encoded key
	bookmarkKey := fmt.Sprintf("%x", bookmarkBytes)
	entry, err := c.kv.Get(c.ctx, bookmarkKey)
	if err == nil {
		// Found bookmark, use its sequence
		seq := binary.BigEndian.Uint64(entry.Value())
		consumerConfig.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
		consumerConfig.OptStartSeq = seq
		c.logger.Info("Starting from sequence for block after progress",
			"progress", progress,
			"nextBlock", progress+1,
			"sequence", seq)
	} else {
		// No bookmark found, need to find closest starting point
		// For now, start from the beginning but log the issue
		consumerConfig.DeliverPolicy = jetstream.DeliverAllPolicy
		c.logger.Warn("No bookmark found for position after progress, starting from beginning",
			"progress", progress,
			"nextBlock", progress+1,
			"error", err)
	}

	return consumerConfig, nil
}

// createStreamConsumer creates a NATS consumer and message channel
func (c *NATSClient) createStreamConsumer(consumerConfig jetstream.ConsumerConfig) (
	jetstream.Consumer, <-chan jetstream.Msg, func(), error) {

	consumer, err := c.js.CreateOrUpdateConsumer(c.ctx, c.streamName, consumerConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create consumer: %w", err)
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
		return nil, nil, nil, fmt.Errorf("failed to start consuming: %w", err)
	}

	cleanup := func() {
		cancelConsume()
		consumerCtx.Stop()
	}

	return consumer, msgChan, cleanup, nil
}

// sendEndOfStreamSignal attempts to send a nil signal to indicate end of stream
func (c *NATSClient) sendEndOfStreamSignal() {
	retries := 0
	for retries < 5 {
		select {
		case c.entryChan <- nil:
			c.logger.Debug("Sent end-of-stream signal to channel")
			return
		default:
			if retries >= 4 {
				c.logger.Warn("Failed to send end-of-stream signal after 5 retries")
				return
			}
			retries++
			c.logger.Debug("Channel is full, waiting to write end-of-stream signal",
				"retry", retries)
			time.Sleep(1 * time.Second)
		}
	}
}

// streamProcessor handles the unified message processing for the read loop
type streamProcessor struct {
	client     *NATSClient
	msgChan    <-chan jetstream.Msg
	ctx        context.Context
	stopSignal *atomic.Bool

	// State for L2 block building
	currentBlock      *types.FullL2Block
	txs               []types.L2TransactionProto
	expectingBlockEnd bool
}

// processMessages is the main message processing loop
func (p *streamProcessor) processMessages() {
	for {
		// Check if we should stop reading
		if p.stopSignal.Load() {
			p.client.logger.Info("Stop signal received, exiting read loop")
			break
		}

		// Get next message with timeout
		msg, shouldContinue := p.getNextMessage()
		if !shouldContinue {
			break
		}
		if msg == nil {
			continue // Timeout, check stop signal again
		}

		// Process the message using unified processing
		if err := p.processMessage(msg); err != nil {
			p.client.logger.Error("Error processing message", "error", err)
		}

		msg.Ack()
	}

	// Handle any incomplete block before exiting
	p.handleIncompleteBlock()
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
		return p.handleStreamBookmark(msg)
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
		p.client.logger.Warn("Received L2 transaction outside of a block")
		return nil
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

// handleStreamBookmark processes Bookmark entries for the stream
func (p *streamProcessor) handleStreamBookmark(msg jetstream.Msg) error {
	bookmark, err := p.client.processBookmark(msg)
	if err != nil {
		return fmt.Errorf("error processing bookmark: %w", err)
	}

	p.client.logger.Debug("Processed bookmark")

	return p.sendToChannel(bookmark)
}

// finalizeAndSendBlock finalizes the current block and sends it to the channel
func (p *streamProcessor) finalizeAndSendBlock() error {
	if p.currentBlock == nil {
		return nil
	}

	// Finalize block processing
	finalizedBlock := p.client.finalizeL2Block(p.currentBlock, p.txs)

	// Send to channel
	return p.sendToChannel(finalizedBlock)
}

// sendToChannel sends an entry to the output channel with context cancellation support
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
	case <-p.ctx.Done():
		return fmt.Errorf("context cancelled while sending to channel")
	}
}

// handleIncompleteBlock handles any incomplete block processing before exiting
// Matches TCP client behavior: treat incomplete blocks as errors, don't send partial data
func (p *streamProcessor) handleIncompleteBlock() {
	if p.expectingBlockEnd && p.currentBlock != nil {
		// Match TCP client behavior: log error but don't send incomplete block
		p.client.logger.Error("Stream ended with incomplete block - not sending partial block",
			"blockNumber", p.currentBlock.L2BlockNumber,
			"txCount", len(p.txs),
			"error", "missing L2BlockEnd entry")
		// Do not call finalizeAndSendBlock() - TCP client would not send incomplete blocks
	}
}

// StopReadingToChannel implements DatastreamClient interface
func (c *NATSClient) StopReadingToChannel() {
	c.stopReading.Store(true)
	c.mutex.RLock()
	reading := c.reading
	c.mutex.RUnlock()

	if reading {
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
	// Get data from message and acknowledge it
	data, err := receiveMsg(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to receive message: %w", err)
	}

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
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
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
	entry, err := c.kv.Get(ctx, bookmarkKey)
	if err != nil {
		c.logger.Error("Error looking up bookmark",
			"blockNumber", blockNum,
			"error", err,
			"errorType", fmt.Sprintf("%T", err))
		return nil, fmt.Errorf("failed to lookup bookmark for block %d: %w", blockNum, err)
	}

	// Found bookmark - use its entry number to calculate sequence
	entryNum := binary.BigEndian.Uint64(entry.Value())
	startSeq := entryNum + 1 // NATS sequences are 1-based

	c.logger.Debug("Found bookmark for block",
		"blockNumber", blockNum,
		"entryNum", entryNum,
		"sequence", startSeq)

	// Create a consumer that starts from the bookmark sequence
	consumerConfig := jetstream.ConsumerConfig{
		Name:          fmt.Sprintf("BLOCK_LOOKUP_%d_%d", blockNum, time.Now().UnixNano()),
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    1,
		DeliverPolicy: jetstream.DeliverByStartSequencePolicy,
		OptStartSeq:   startSeq,
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

func receiveMsg(msg jetstream.Msg) ([]byte, error) {
	// Acknowledge the message
	if err := msg.Ack(); err != nil {
		return nil, fmt.Errorf("failed to acknowledge message: %w", err)
	}

	// Return the message data
	return msg.Data(), nil
}

// GetLatestL2Block implements DatastreamClient interface
func (c *NATSClient) GetLatestL2Block() (*types.FullL2Block, error) {
	c.mutex.RLock()
	if !c.started {
		c.mutex.RUnlock()
		return nil, ErrNotStarted
	}
	c.mutex.RUnlock()

	c.latestBlockMutex.RLock()
	latestBlock := c.latestBlock
	c.latestBlockMutex.RUnlock()

	if latestBlock == nil {
		return nil, nil // Return nil, nil when no blocks exist yet
	}

	return latestBlock, nil
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

	// Connect to NATS
	opts := []nats.Option{
		nats.Name("erigon-datastream-client"),
		nats.ReconnectWait(1 * time.Second),
		nats.MaxReconnects(-1),
		nats.Timeout(DefaultConnectTimeout),
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

	// Initialize KV store for bookmarks
	kv, err := js.KeyValue(c.ctx, fmt.Sprintf("DATASTREAM_KV_%d", c.chainID))
	if err != nil {
		// If KV doesn't exist, create it
		kvConfig := jetstream.KeyValueConfig{
			Bucket:      fmt.Sprintf("DATASTREAM_KV_%d", c.chainID),
			Description: "Datastream bookmarks and metadata",
		}
		kv, err = js.CreateKeyValue(c.ctx, kvConfig)
		if err != nil {
			nc.Close()
			return fmt.Errorf("failed to create KV store: %w", err)
		}
	}
	c.kv = kv

	c.started = true
	c.logger.Info("NATS datastream client started",
		"url", c.natsURL,
		"stream", c.streamName,
		"chainID", c.chainID,
		"forkID", c.forkID)

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
	c.cancelFunc()

	// Wait for read loop to complete
	c.readWg.Wait()

	// Close NATS connection and update state
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.nc != nil {
		c.nc.Close()
		c.nc = nil
	}

	c.started = false
	c.logger.Info("NATS datastream client stopped")

	return nil
}

// HandleStart implements DatastreamClient interface
func (c *NATSClient) HandleStart() error {
	c.mutex.RLock()
	if !c.started {
		c.mutex.RUnlock()
		return ErrNotStarted
	}
	c.mutex.RUnlock()

	return nil
}

// CreateNATSDatastreamClient is a factory function that can be used to create a DatastreamClient
// implementation that reads from NATS JetStream.
func CreateNATSDatastreamClient(ctx context.Context, natsURL string, useTLS bool, timeout time.Duration, latestForkID uint16, maxChannelSize uint64) types.DatastreamClient {
	logger := log.Root()
	client := NewNATSClient(ctx, natsURL, useTLS, 0, uint64(latestForkID), logger)
	client.maxEntryChanSize = maxChannelSize
	return client
}

// processBookmarkMessage processes a bookmark entry
func (c *NATSClient) processBookmarkMessage(msg jetstream.Msg) (interface{}, error) {
	// Parse the bookmark from the message data
	bookmark, err := c.processBookmark(msg)
	if err != nil {
		return nil, err
	}

	metadata, err := msg.Metadata()
	if err != nil {
		c.logger.Debug("Failed to get message metadata", "error", err)
	} else {
		c.logger.Debug("Processed bookmark",
			"type", bookmark.BookmarkType(),
			"value", bookmark.BookMark.GetValue(),
			"sequence", metadata.Sequence.Stream)
	}

	return bookmark, nil
}
