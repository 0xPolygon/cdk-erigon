package natsstream

import (
	"context"
	"fmt"
	"time"

	erigoncommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/log/v3"
	eritypes "github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/zk/datastream/server"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/erigontech/erigon/zk/hermez_db"
	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
	dslog "github.com/gateway-fm/zkevm-data-streamer/log"
	"github.com/nats-io/nats.go/jetstream" // Add the new jetstream package
)

// NATSDataStreamServer is a wrapper around a DataStreamServer
type NATSDataStreamServer struct {
	delegate server.DataStreamServer
	js       jetstream.JetStream // Changed from nats.JetStreamContext to jetstream.JetStream
	logger   log.Logger
}

func (srv *NATSDataStreamServer) CompileGenesisStreamEntries(genesis *eritypes.Block, reader *hermez_db.HermezDbReader, tx kv.Tx) ([]server.DataStreamEntryProto, error) {
	//TODO implement me
	panic("implement me")
}

// NATSDataStreamServerFactory creates DataStreamServer instances
type NATSDataStreamServerFactory struct {
	delegateFactory server.DataStreamServerFactory
	natsManager     *Manager
	logger          log.Logger
}

// NewNATSDataStreamServerFactory creates a new factory for NATS-enabled DataStreamServer instances
func NewNATSDataStreamServerFactory(delegateFactory server.DataStreamServerFactory, natsManager *Manager, logger log.Logger) *NATSDataStreamServerFactory {
	return &NATSDataStreamServerFactory{
		delegateFactory: delegateFactory,
		natsManager:     natsManager,
		logger:          logger,
	}
}

// CreateStreamServer forwards to the delegate factory
func (f *NATSDataStreamServerFactory) CreateStreamServer(port uint16, systemID uint64, streamType datastreamer.StreamType, fileName string, writeTimeout time.Duration, inactivityTimeout time.Duration, inactivityCheckInterval time.Duration, cfg *dslog.Config, chainId uint64) (server.StreamServer, error) {

	delegate, err := f.delegateFactory.CreateStreamServer(port, systemID, streamType, fileName, writeTimeout, inactivityTimeout, inactivityCheckInterval, cfg, chainId)

	// Connect to NATS
	conn, err := f.natsManager.Connect()
	if err != nil {
		f.logger.Error("Failed to connect to NATS server", "error", err)
		return delegate, nil // Fallback to original if NATS connection fails
	}

	// Set up JetStream using the new API
	js, err := jetstream.New(conn)
	if err != nil {
		f.logger.Error("Failed to create JetStream context", "error", err)
		conn.Close()
		return delegate, nil // Fallback to original if JetStream creation fails
	}

	// Create streams for persistence if they don't exist using the new API
	_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{
		Name:     fmt.Sprintf("DATASTREAM_%d", chainId),
		Subjects: []string{"datastream.>"}, // Changed from datastream.* to datastream.> to capture all levels
		Storage:  jetstream.FileStorage,
	})
	if err != nil {
		f.logger.Error("Failed to create JetStream stream", "error", err)
	}

	return &NATSStreamServer{
		delegate: delegate,
		js:       js,
		chainId:  chainId,
		logger:   f.logger,
	}, nil
}

// CreateDataStreamServer creates a new NATSDataStreamServer that wraps the original implementation
func (f *NATSDataStreamServerFactory) CreateDataStreamServer(stream server.StreamServer, chainId uint64) server.DataStreamServer {

	delegate := f.delegateFactory.CreateDataStreamServer(stream, chainId)
	// Connect to NATS
	conn, err := f.natsManager.Connect()
	if err != nil {
		f.logger.Error("Failed to connect to NATS server", "error", err)
		return delegate // Fallback to original if NATS connection fails
	}

	// Set up JetStream using the new API
	js, err := jetstream.New(conn)
	if err != nil {
		f.logger.Error("Failed to create JetStream context", "error", err)
		conn.Close()
		return delegate // Fallback to original if JetStream creation fails
	}

	// Create streams for persistence if they don't exist using the new API
	_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{
		Name:     fmt.Sprintf("DATASTREAM_%d", chainId),
		Subjects: []string{"datastream.>"}, // Changed from datastream.* to datastream.> to capture all levels
		Storage:  jetstream.FileStorage,
	})
	if err != nil {
		f.logger.Error("Failed to create JetStream stream", "error", err)
	}

	// Create the original DataStreamServer
	delegate = f.delegateFactory.CreateDataStreamServer(
		NewNATSStreamServer(stream, js, chainId, f.logger),
		chainId,
	)

	return &NATSDataStreamServer{
		delegate: delegate,
		js:       js,
		logger:   f.logger,
	}
}

// All methods below simply forward to the delegate implementation and publish events to NATS when appropriate

func (srv *NATSDataStreamServer) GetStreamServer() server.StreamServer {
	return srv.delegate.GetStreamServer()
}

func (srv *NATSDataStreamServer) GetChainId() uint64 {
	return srv.delegate.GetChainId()
}

func (srv *NATSDataStreamServer) IsLastEntryBatchEnd() (isBatchEnd bool, err error) {
	return srv.delegate.IsLastEntryBatchEnd()
}

func (srv *NATSDataStreamServer) GetHighestBlockNumber() (uint64, error) {
	return srv.delegate.GetHighestBlockNumber()
}

func (srv *NATSDataStreamServer) GetHighestBatchNumber() (uint64, error) {
	return srv.delegate.GetHighestBatchNumber()
}

func (srv *NATSDataStreamServer) GetHighestClosedBatch() (uint64, error) {
	return srv.delegate.GetHighestClosedBatch()
}

func (srv *NATSDataStreamServer) GetHighestClosedBatchNoCache() (uint64, error) {
	return srv.delegate.GetHighestClosedBatchNoCache()
}

func (srv *NATSDataStreamServer) UnwindToBlock(blockNumber uint64) error {
	return srv.delegate.UnwindToBlock(blockNumber)
}

func (srv *NATSDataStreamServer) UnwindToBatchStart(batchNumber uint64) error {
	return srv.delegate.UnwindToBatchStart(batchNumber)
}

func (srv *NATSDataStreamServer) ReadBatches(start uint64, end uint64) ([][]*types.FullL2Block, error) {
	return srv.delegate.ReadBatches(start, end)
}

func (srv *NATSDataStreamServer) ReadBatchesWithConcurrency(start uint64, end uint64) ([][]*types.FullL2Block, error) {
	return srv.delegate.ReadBatchesWithConcurrency(start, end)
}

func (srv *NATSDataStreamServer) WriteWholeBatchToStream(logPrefix string, tx kv.Tx, reader server.DbReader, prevBatchNum, batchNum uint64) error {
	return srv.delegate.WriteWholeBatchToStream(logPrefix, tx, reader, prevBatchNum, batchNum)
}

func (srv *NATSDataStreamServer) WriteBlocksToStreamConsecutively(ctx context.Context, logPrefix string, tx kv.Tx, reader server.DbReader, from, to uint64) error {
	return srv.delegate.WriteBlocksToStreamConsecutively(ctx, logPrefix, tx, reader, from, to)
}

func (srv *NATSDataStreamServer) WriteBlockWithBatchStartToStream(logPrefix string, tx kv.Tx, reader server.DbReader, forkId, batchNum, prevBlockBatchNum uint64, prevBlock, block eritypes.Block) (err error) {
	return srv.delegate.WriteBlockWithBatchStartToStream(logPrefix, tx, reader, forkId, batchNum, prevBlockBatchNum, prevBlock, block)
}

func (srv *NATSDataStreamServer) UnwindIfNecessary(logPrefix string, reader server.DbReader, blockNum, prevBlockBatchNum, batchNum uint64) error {
	return srv.delegate.UnwindIfNecessary(logPrefix, reader, blockNum, prevBlockBatchNum, batchNum)
}

func (srv *NATSDataStreamServer) WriteBatchEnd(reader server.DbReader, batchNumber uint64, stateRoot *erigoncommon.Hash, localExitRoot *erigoncommon.Hash) (err error) {
	return srv.delegate.WriteBatchEnd(reader, batchNumber, stateRoot, localExitRoot)
}

func (srv *NATSDataStreamServer) WriteGenesisToStream(genesis *eritypes.Block, reader *hermez_db.HermezDbReader, tx kv.Tx) error {
	// First delegate to the original implementation
	err := srv.delegate.WriteGenesisToStream(genesis, reader, tx)
	if err != nil {
		return err
	}

	//entries, err := srv.delegate.CompileGenesisStreamEntries(genesis, reader, tx)
	//if err != nil {
	//	srv.logger.Error("Failed to compile genesis stream entries", "error", err)
	//	return err // Return the error if compilation fails
	//}
	//if entries == nil || len(entries) == 0 {
	//	srv.logger.Warn("No entries compiled for genesis block, skipping NATS publish")
	//	return nil // No entries to publish
	//}
	//
	//// Then publish each genesis entry individually to NATS using the new jetstream API
	//if srv.js != nil {
	//	// Base subject for all genesis entries
	//	baseSubject := fmt.Sprintf("datastream.genesis.%d", srv.GetChainId())
	//
	//	// Pre-compute common header values to avoid redundant string formatting in the loop
	//	blockNumberStr := fmt.Sprintf("%d", genesis.NumberU64())
	//	hash := genesis.Hash().String()
	//	chainId := fmt.Sprintf("%d", srv.GetChainId())
	//	totalEntriesStr := fmt.Sprintf("%d", len(entries))
	//
	//	// Publish each entry individually
	//	for i, entry := range entries {
	//		// Serialize this specific entry
	//		entryBytes, err := entry.Marshal()
	//		if err != nil {
	//			srv.logger.Error("Failed to marshal genesis entry",
	//				"error", err,
	//				"entryType", string(entry.Type()),
	//				"index", i)
	//			continue
	//		}
	//
	//		// Create a message with pre-allocated header
	//		msg := &nats.Msg{
	//			Subject: baseSubject,
	//			Data:    entryBytes,
	//			Header: nats.Header{
	//				"blockNumber":  []string{blockNumberStr},
	//				"hash":         []string{hash},
	//				"chainId":      []string{chainId},
	//				"totalEntries": []string{totalEntriesStr},
	//				"entryIndex":   []string{strconv.Itoa(i)},
	//				"entryType":    []string{strconv.Itoa(int(entry.Type()))},
	//				"Nats-Msg-Id":  []string{fmt.Sprintf("genesis-%d-entry-%d", genesis.NumberU64(), i)},
	//			},
	//		}
	//
	//		// Publish the message using the new API
	//		_, err = srv.js.PublishMsg(context.Background(), msg)
	//
	//		if err != nil {
	//			srv.logger.Error("Failed to publish genesis entry to NATS",
	//				"error", err,
	//				"entryType", strconv.Itoa(int(entry.Type())),
	//				"index", i)
	//			// Continue publishing other entries even if one fails
	//		}
	//	}
	//
	//	srv.logger.Info("Published genesis entries to NATS",
	//		"blockNumber", genesis.NumberU64(),
	//		"hash", genesis.Hash().String(),
	//		"chainId", srv.GetChainId(),
	//		"entryCount", len(entries))
	//}

	return nil
}
