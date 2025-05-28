package natsstream

import (
	"context"
	"fmt"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/server"
	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSStreamServer implements the server.StreamServer interface
// It wraps an existing StreamServer implementation and forwards all calls to it
// While also publishing events to NATS for external subscribers
type NATSStreamServer struct {
	// The underlying StreamServer implementation
	delegate server.StreamServer
	// JetStream context
	js jetstream.JetStream
	// Logger
	logger log.Logger
	// Chain ID for subject construction
	chainId uint64
}

// NewNATSStreamServer creates a new NATS stream server that wraps an existing StreamServer
func NewNATSStreamServer(
	delegate server.StreamServer,
	js jetstream.JetStream,
	chainId uint64,
	logger log.Logger,
) *NATSStreamServer {
	return &NATSStreamServer{
		delegate: delegate,
		js:       js,
		chainId:  chainId,
		logger:   logger,
	}
}

// Start starts the underlying stream server
func (n *NATSStreamServer) Start() error {
	return n.delegate.Start()
}

// StartAtomicOp starts an atomic operation in the underlying stream server
func (n *NATSStreamServer) StartAtomicOp() error {
	return n.delegate.StartAtomicOp()
}

// AddStreamEntry adds an entry to the stream and publishes it to NATS
func (n *NATSStreamServer) AddStreamEntry(etype datastreamer.EntryType, data []byte) (uint64, error) {
	// First add the entry to the underlying stream
	entryNum, err := n.delegate.AddStreamEntry(etype, data)
	if err != nil {
		return 0, err
	}

	// Publish the entry to NATS
	if n.js != nil {
		subject := fmt.Sprintf("datastream.entry.%d", n.chainId)

		// Create a message with headers including metadata about the entry
		msg := &nats.Msg{
			Subject: subject,
			Data:    data,
			Header: nats.Header{
				"entryType":   []string{fmt.Sprintf("%d", etype)},
				"entryIndex":  []string{fmt.Sprintf("%d", entryNum)},
				"chainId":     []string{fmt.Sprintf("%d", n.chainId)},
				"Nats-Msg-Id": []string{fmt.Sprintf("entry-%d-%d", n.chainId, entryNum)},
			},
		}

		// Publish to NATS JetStream
		_, err := n.js.PublishMsg(context.Background(), msg)
		if err != nil {
			n.logger.Error("Failed to publish entry to NATS", "error", err, "entryNum", entryNum)
			// We don't return this error as the primary operation succeeded
		} else {
			n.logger.Debug("Published entry to NATS", "entryNum", entryNum, "type", etype)
		}
	}

	return entryNum, nil
}

// AddStreamBookmark adds a bookmark to the stream and publishes it to NATS
func (n *NATSStreamServer) AddStreamBookmark(bookmark []byte) (uint64, error) {
	// First add the bookmark to the underlying stream
	entryNum, err := n.delegate.AddStreamBookmark(bookmark)
	if err != nil {
		return 0, err
	}

	// Publish the bookmark to NATS
	if n.js != nil {
		subject := fmt.Sprintf("datastream.bookmark.%d", n.chainId)

		// Create a message with headers including metadata about the bookmark
		msg := &nats.Msg{
			Subject: subject,
			Data:    bookmark,
			Header: nats.Header{
				"entryIndex":  []string{fmt.Sprintf("%d", entryNum)},
				"chainId":     []string{fmt.Sprintf("%d", n.chainId)},
				"Nats-Msg-Id": []string{fmt.Sprintf("bookmark-%d-%d", n.chainId, entryNum)},
			},
		}

		// Publish to NATS JetStream
		_, err := n.js.PublishMsg(context.Background(), msg)
		if err != nil {
			n.logger.Error("Failed to publish bookmark to NATS", "error", err, "entryNum", entryNum)
			// We don't return this error as the primary operation succeeded
		} else {
			n.logger.Debug("Published bookmark to NATS", "entryNum", entryNum)
		}
	}

	return entryNum, nil
}

// CommitAtomicOp commits an atomic operation in the underlying stream server
func (n *NATSStreamServer) CommitAtomicOp() error {
	err := n.delegate.CommitAtomicOp()
	if err != nil {
		return err
	}

	// Optionally publish a commit notification to NATS
	if n.js != nil {
		subject := fmt.Sprintf("datastream.atomic.commit.%d", n.chainId)

		msg := &nats.Msg{
			Subject: subject,
			Header: nats.Header{
				"chainId": []string{fmt.Sprintf("%d", n.chainId)},
				"event":   []string{"commit"},
			},
		}

		_, err := n.js.PublishMsg(context.Background(), msg)
		if err != nil {
			n.logger.Error("Failed to publish commit notification to NATS", "error", err)
			// Don't return error as primary operation succeeded
		}
	}

	return nil
}

// RollbackAtomicOp rolls back an atomic operation in the underlying stream server
func (n *NATSStreamServer) RollbackAtomicOp() error {
	err := n.delegate.RollbackAtomicOp()
	if err != nil {
		return err
	}

	// Optionally publish a rollback notification to NATS
	if n.js != nil {
		subject := fmt.Sprintf("datastream.atomic.rollback.%d", n.chainId)

		msg := &nats.Msg{
			Subject: subject,
			Header: nats.Header{
				"chainId": []string{fmt.Sprintf("%d", n.chainId)},
				"event":   []string{"rollback"},
			},
		}

		_, err := n.js.PublishMsg(context.Background(), msg)
		if err != nil {
			n.logger.Error("Failed to publish rollback notification to NATS", "error", err)
			// Don't return error as primary operation succeeded
		}
	}

	return nil
}

// TruncateFile truncates the file in the underlying stream server
func (n *NATSStreamServer) TruncateFile(entryNum uint64) error {
	err := n.delegate.TruncateFile(entryNum)
	if err != nil {
		return err
	}

	// Publish a truncate notification to NATS
	if n.js != nil {
		subject := fmt.Sprintf("datastream.truncate.%d", n.chainId)

		msg := &nats.Msg{
			Subject: subject,
			Header: nats.Header{
				"chainId":  []string{fmt.Sprintf("%d", n.chainId)},
				"entryNum": []string{fmt.Sprintf("%d", entryNum)},
				"event":    []string{"truncate"},
			},
		}

		_, err := n.js.PublishMsg(context.Background(), msg)
		if err != nil {
			n.logger.Error("Failed to publish truncate notification to NATS", "error", err)
			// Don't return error as primary operation succeeded
		}
	}

	return nil
}

// UpdateEntryData updates an entry's data in the underlying stream
func (n *NATSStreamServer) UpdateEntryData(entryNum uint64, etype datastreamer.EntryType, data []byte) error {
	err := n.delegate.UpdateEntryData(entryNum, etype, data)
	if err != nil {
		return err
	}

	// Publish an update notification to NATS
	if n.js != nil {
		subject := fmt.Sprintf("datastream.update.%d", n.chainId)

		msg := &nats.Msg{
			Subject: subject,
			Data:    data,
			Header: nats.Header{
				"chainId":   []string{fmt.Sprintf("%d", n.chainId)},
				"entryNum":  []string{fmt.Sprintf("%d", entryNum)},
				"entryType": []string{fmt.Sprintf("%d", etype)},
				"event":     []string{"update"},
			},
		}

		_, err := n.js.PublishMsg(context.Background(), msg)
		if err != nil {
			n.logger.Error("Failed to publish update notification to NATS", "error", err)
			// Don't return error as primary operation succeeded
		}
	}

	return nil
}

// GetHeader gets the stream header from the underlying stream server
func (n *NATSStreamServer) GetHeader() datastreamer.HeaderEntry {
	return n.delegate.GetHeader()
}

// GetEntry gets an entry from the underlying stream server
func (n *NATSStreamServer) GetEntry(entryNum uint64) (datastreamer.FileEntry, error) {
	return n.delegate.GetEntry(entryNum)
}

// GetBookmark gets a bookmark from the underlying stream server
func (n *NATSStreamServer) GetBookmark(bookmark []byte) (uint64, error) {
	return n.delegate.GetBookmark(bookmark)
}

// GetFirstEventAfterBookmark gets the first event after a bookmark from the underlying stream server
func (n *NATSStreamServer) GetFirstEventAfterBookmark(bookmark []byte) (datastreamer.FileEntry, error) {
	return n.delegate.GetFirstEventAfterBookmark(bookmark)
}

// GetDataBetweenBookmarks gets data between bookmarks from the underlying stream server
func (n *NATSStreamServer) GetDataBetweenBookmarks(bookmarkFrom, bookmarkTo []byte) ([]byte, error) {
	return n.delegate.GetDataBetweenBookmarks(bookmarkFrom, bookmarkTo)
}

// BookmarkPrintDump prints the bookmark dump from the underlying stream server
func (n *NATSStreamServer) BookmarkPrintDump() {
	n.delegate.BookmarkPrintDump()
}
