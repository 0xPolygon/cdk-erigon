package natsstream

import (
	"context"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/server"
	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
	dslog "github.com/gateway-fm/zkevm-data-streamer/log"
)

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

// CreateStreamServer forwards to the delegate factory and wraps the result with a NATS-enabled stream server
func (f *NATSDataStreamServerFactory) CreateStreamServer(port uint16, systemID uint64, streamType datastreamer.StreamType, fileName string, writeTimeout time.Duration, inactivityTimeout time.Duration, inactivityCheckInterval time.Duration, cfg *dslog.Config) (server.StreamServer, error) {
	delegate, err := f.delegateFactory.CreateStreamServer(port, systemID, streamType, fileName, writeTimeout, inactivityTimeout, inactivityCheckInterval, cfg)
	if err != nil {
		return nil, err
	}

	// Initialize streams
	err = f.natsManager.InitStreams(context.Background())
	if err != nil {
		f.logger.Error("Failed to initialize streams", "error", err)
		return delegate, nil // Fallback to original if stream initialization fails
	}

	return &NATSStreamServer{
		delegate:    delegate,
		natsManager: f.natsManager,
		logger:      f.logger,
		// Initialize transaction fields
		txActive: false,
		txMsgs:   nil,
	}, nil
}

// CreateDataStreamServer creates a data stream server without any NATS wrapper
// We're just using the StreamServer level for NATS integration
func (f *NATSDataStreamServerFactory) CreateDataStreamServer(stream server.StreamServer, chainId uint64) server.DataStreamServer {
	// If the stream is not already a NATS stream server, wrap it
	if _, ok := stream.(*NATSStreamServer); !ok {
		// Initialize streams
		err := f.natsManager.InitStreams(context.Background())
		if err != nil {
			f.logger.Error("Failed to initialize streams", "error", err)
			// Continue with the unwrapped stream
		} else {
			// Wrap the stream with a NATS stream server
			stream = &NATSStreamServer{
				delegate:    stream,
				natsManager: f.natsManager,
				logger:      f.logger,
				// Initialize transaction fields
				txActive: false,
				txMsgs:   nil,
			}
		}
	}

	// Just delegate to the original factory with the potentially wrapped stream
	return f.delegateFactory.CreateDataStreamServer(stream, chainId)
}
