package natsstream

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/server"
	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
	"github.com/nats-io/nats.go"
)

// BookmarkTx represents a bookmark to be stored during transaction commit
type BookmarkTx struct {
	bookmark []byte
	entryNum uint64
}

// NATSStreamServer implements the server.StreamServer interface
// It wraps an existing StreamServer implementation and forwards all calls to it
// While also publishing events to NATS for external subscribers
type NATSStreamServer struct {
	// The underlying StreamServer implementation
	delegate server.StreamServer
	// NATS manager for stream operations
	natsManager *Manager
	// Logger
	logger log.Logger
	// Chain ID for subject construction
	chainId uint64
	// Bookmark manager
	bookmark *NATSBookmark

	// Transaction related fields
	txActive     bool
	txMsgs       []*nats.Msg
	txMutex      sync.RWMutex
	txEntryCount uint64
	nextEntry    uint64 // Tracks the number of entries in the stream
}

// Start starts the underlying stream server
func (n *NATSStreamServer) Start() error {
	// Initialize bookmark manager if not already initialized
	var err error
	if n.bookmark == nil {
		n.bookmark, err = NewNATSBookmark(n.natsManager, n.chainId, n.logger)
		if err != nil {
			n.logger.Error("Failed to initialize NATS bookmark manager", "error", err)
			// Continue with underlying system as fallback
		}
	}

	// Initialize next entry counter from NATS stream info if available
	if n.natsManager != nil && n.natsManager.mainStream != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		info, err := n.natsManager.mainStream.Info(ctx)
		if err == nil {
			// NATS sequences are 1-based, so LastSeq is the number of entries
			n.nextEntry = info.State.LastSeq
			n.logger.Info("Initialized nextEntry counter from NATS stream",
				"count", n.nextEntry,
				"lastMsgTime", info.State.LastTime)
		} else {
			// Fall back to delegate header
			n.logger.Warn("Could not get NATS stream info, falling back to delegate", "error", err)
			n.nextEntry = n.delegate.GetHeader().TotalEntries
		}
	} else {
		// Initialize from delegate as fallback
		n.nextEntry = n.delegate.GetHeader().TotalEntries
	}

	return n.delegate.Start()
}

// StartAtomicOp starts an atomic operation in the underlying stream server
func (n *NATSStreamServer) StartAtomicOp() error {
	// First start the atomic operation in the delegate
	err := n.delegate.StartAtomicOp()
	if err != nil {
		return err
	}

	// Begin a transaction in our local memory store
	n.txMutex.Lock()
	defer n.txMutex.Unlock()

	if n.txActive {
		// This is just a safeguard; the datastream API should prevent this
		n.logger.Warn("Starting a new transaction while one is already active; previous transaction data will be lost")
	}

	// Reset transaction state
	n.txActive = true
	n.txMsgs = make([]*nats.Msg, 0, 100) // Pre-allocate space for 100 messages
	n.txEntryCount = 0

	n.logger.Debug("Started transaction")
	return nil
}

// addStreamItem is a helper function that adds an item to the NATS cache
// for later publication. It handles the common logic between AddStreamEntry and AddStreamBookmark.
func (n *NATSStreamServer) addStreamItem(entryType string, data []byte, entryNum uint64, itemType string) (uint64, error) {
	if !n.txActive {
		n.logger.Warn(itemType + " called outside of a transaction; item will not be cached for NATS")
		return entryNum, fmt.Errorf("%s called outside of a transaction; item will not be cached for NATS", itemType)
	}

	// Store the item in our transaction buffer
	n.txMutex.Lock()
	defer n.txMutex.Unlock()

	// Create a NATS message for later publishing
	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    append([]byte(nil), data...), // Make a copy of data to avoid potential mutations
		Header: nats.Header{
			"EntryType": []string{entryType},
			"EntryNum":  []string{fmt.Sprintf("%d", entryNum)},
		},
	}
	n.txMsgs = append(n.txMsgs, msg)

	//TODO remove this once we take DS out, its just a quick sense check whilst we transition
	if entryNum != n.nextEntry+n.txEntryCount {
		n.logger.Warn("Entry number does not match expected value",
			"entryNum", entryNum,
			"nextEntry", n.nextEntry,
			"txEntryCount", n.txEntryCount,
			"expected", n.nextEntry+n.txEntryCount)
	}

	// Calculate adjusted entry number that accounts for items in the current transaction
	entryNum = n.nextEntry + n.txEntryCount
	n.txEntryCount++ // Increment entry count

	n.logger.Debug("Cached message for later publishing",
		"type", itemType,
		"entryNum",
		"cacheSize", len(n.txMsgs))

	return entryNum, nil
}

// AddStreamEntry adds an entry to the stream and buffers it for NATS
func (n *NATSStreamServer) AddStreamEntry(entryType datastreamer.EntryType, data []byte) (uint64, error) {
	// First add the entry to the underlying stream
	entryNum, err := n.delegate.AddStreamEntry(entryType, data)
	if err != nil {
		return 0, err
	}

	return n.addStreamItem(
		strconv.Itoa(int(entryType)),
		data,
		entryNum,
		"AddStreamEntry",
	)
}

// AddStreamBookmark adds a bookmark to the stream and buffers it for NATS
func (n *NATSStreamServer) AddStreamBookmark(bookmark []byte) (uint64, error) {
	// First add the bookmark to the underlying stream
	entryNum, err := n.delegate.AddStreamBookmark(bookmark)
	if err != nil {
		return 0, err
	}

	return n.addStreamItem(
		"176", // 0xb0 - EtBookmark
		bookmark,
		entryNum,
		"AddStreamBookmark",
	)
}

// CommitAtomicOp commits an atomic operation, publishing all buffered entries to NATS
func (n *NATSStreamServer) CommitAtomicOp() error {
	// First commit the atomic operation in the delegate
	err := n.delegate.CommitAtomicOp()
	if err != nil {
		return err
	}

	// Publish all buffered entries to NATS
	n.txMutex.Lock()
	defer n.txMutex.Unlock()

	if !n.txActive {
		n.logger.Warn("Commit called but no transaction is active")
		return nil
	}

	// Publish all cached messages
	msgCount := len(n.txMsgs)
	if msgCount > 0 && n.natsManager != nil {
		n.logger.Info("Publishing cached messages to NATS",
			"count", msgCount,
			"totalEntries", n.txEntryCount)

		// Get JetStream to publish messages
		js, err := n.natsManager.GetOrCreateDataStream()
		if err != nil {
			n.logger.Error("Failed to get JetStream for publishing", "error", err)
			// Continue without publishing
		} else {
			// Publish all messages
			for i, msg := range n.txMsgs {
				// Publish the message
				_, err := js.PublishMsg(context.Background(), msg)
				if err != nil {
					n.logger.Error("Failed to publish message to NATS during commit",
						"error", err,
						"index", i)
					// Continue with other messages even if one fails
				}

				// Store bookmark if this is a bookmark entry (check EntryType header)
				// We only do this after successful publishing so we have the correct entry number
				entryTypeHeader := msg.Header.Get("EntryType")
				if entryTypeHeader == "176" && n.bookmark != nil { // EntryType for bookmark
					err = n.bookmark.AddBookmark(n.natsManager, msg.Data, n.nextEntry)
					if err != nil {
						n.logger.Error("Failed to store bookmark in NATS KV",
							"error", err,
							"entryNum", n.nextEntry)
						// Continue even if bookmark storage fails
					}
				}

				// Increment the next entry number
				n.nextEntry++
			}
		}
	}

	// Reset transaction state
	n.txActive = false
	n.txMsgs = nil
	n.txEntryCount = 0 // Reset entry count

	return nil
}

// RollbackAtomicOp rolls back an atomic operation, discarding all buffered entries
func (n *NATSStreamServer) RollbackAtomicOp() error {
	// First roll back the atomic operation in the delegate
	err := n.delegate.RollbackAtomicOp()
	if err != nil {
		return err
	}

	// Clear all buffered entries
	n.txMutex.Lock()
	defer n.txMutex.Unlock()

	if !n.txActive {
		n.logger.Warn("Rollback called but no transaction is active")
		return nil
	}

	msgCount := len(n.txMsgs)
	n.logger.Info("Rolling back transaction, discarding cached messages",
		"count", msgCount,
		"totalEntries", n.txEntryCount)

	// Reset transaction state
	n.txActive = false
	n.txMsgs = nil
	n.txEntryCount = 0 // Reset entry count

	return nil
}

// TruncateFile truncates the file in the underlying stream server and deletes messages from NATS stream
func (n *NATSStreamServer) TruncateFile(entryNum uint64) error {
	n.logger.Info("Truncating datastream file and NATS stream", "entryNum", entryNum)

	// First truncate the file in the delegate
	err := n.delegate.TruncateFile(entryNum)
	if err != nil {
		n.logger.Error("Failed to truncate delegate stream", "error", err, "entryNum", entryNum)
		return err
	}

	// Now delete messages from NATS JetStream
	if n.natsManager != nil && n.natsManager.mainStream != nil {
		// Convert our entry number to NATS sequence number (NATS is 1-based)
		// Any entry > entryNum should be deleted, so startSeq is entryNum+1
		startSeq := entryNum + 1

		// Get stream info to check current state
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		info, err := n.natsManager.mainStream.Info(ctx)
		if err != nil {
			n.logger.Error("Failed to get stream info for truncation",
				"error", err,
				"entryNum", entryNum)
			// Continue without failing as the delegate truncation was successful
			// We'll log the error but return nil to allow operation to continue
			return nil
		}

		// Check if there are messages to delete
		if info.State.LastSeq >= startSeq {
			totalToDelete := info.State.LastSeq - startSeq + 1
			n.logger.Info("Deleting messages from NATS stream",
				"fromSeq", startSeq,
				"toSeq", info.State.LastSeq,
				"totalToDelete", totalToDelete)

			// Track deleted and failed messages
			deletedCount := uint64(0)
			failedCount := uint64(0)

			// Delete messages sequentially
			for seq := startSeq; seq <= info.State.LastSeq; seq++ {
				// Create a context with timeout for each deletion
				deleteCtx, deleteCancel := context.WithTimeout(context.Background(), 1*time.Second)
				err := n.natsManager.mainStream.DeleteMsg(deleteCtx, seq)
				deleteCancel()

				if err != nil {
					failedCount++
					if failedCount <= 5 { // Only log the first few errors to avoid flooding
						n.logger.Warn("Failed to delete message", "seq", seq, "error", err)
					}
				} else {
					deletedCount++
				}
			}

			n.logger.Info("Truncated NATS stream",
				"fromSeq", startSeq,
				"toSeq", info.State.LastSeq,
				"messagesDeleted", deletedCount,
				"messagesFailed", failedCount,
				"totalAttempted", totalToDelete)
		} else {
			n.logger.Info("No messages to truncate from NATS stream",
				"requestedSeq", startSeq,
				"lastSeq", info.State.LastSeq)
		}

		// Reset next entry counter to match truncated state
		// Unlike file-based storage, NATS keeps sequence numbers as gaps after deletion
		// So we need to get updated stream info to properly calculate the effective entry count
		postTruncCtx, postTruncCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer postTruncCancel()

		postInfo, err := n.natsManager.mainStream.Info(postTruncCtx)
		if err == nil {
			// Calculate effective entries (counting only non-deleted messages)
			numDeleted := uint64(len(postInfo.State.Deleted))
			effectiveEntries := entryNum + 1 // Include the entry we kept

			// Update our nextEntry to reflect the truncated state
			n.nextEntry = effectiveEntries

			n.logger.Info("Updated nextEntry counter after truncation",
				"nextEntry", n.nextEntry,
				"deletedCount", numDeleted)
		} else {
			// Fall back to the simple approach if we can't get stream info
			n.nextEntry = entryNum + 1
			n.logger.Warn("Could not get stream info after truncation, using fallback counter value",
				"error", err,
				"nextEntry", n.nextEntry)
		}

		// If we have bookmarks that point beyond the truncation point, they should be cleaned up
		if n.bookmark != nil {
			// Since we now have TruncateBookmarksAfter method, use it
			err = n.bookmark.TruncateBookmarksAfter(n.natsManager, entryNum)
			if err != nil {
				n.logger.Error("Failed to truncate bookmarks after entry",
					"error", err,
					"entryNum", entryNum)
				// Non-fatal error, continue
			}
		}
	}

	return nil
}

// UpdateEntryData updates an entry's data in the underlying stream
func (n *NATSStreamServer) UpdateEntryData(entryNum uint64, etype datastreamer.EntryType, data []byte) error {
	//obsolete method throw error
	return fmt.Errorf("UpdateEntryData is not supported in NATSStreamServer, use AddStreamEntry instead")
}

// GetHeader gets the stream header from the underlying stream server
func (n *NATSStreamServer) GetHeader() datastreamer.HeaderEntry {
	// Create a basic HeaderEntry with minimal required fields
	header := datastreamer.HeaderEntry{
		Version:      1,           // Use a default version
		SystemID:     n.chainId,   // Use chainId as SystemID
		TotalEntries: n.nextEntry, // Start with our tracked next entry as default
	}

	// If NATS is available, try to get the most accurate count
	if n.natsManager != nil && n.natsManager.mainStream != nil {
		// Get stream info to check current state
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		info, err := n.natsManager.mainStream.Info(ctx)
		if err == nil {
			// NATS sequence is 1-based, but our entry counts are 0-based
			// If no messages, LastSeq will be 0
			if info.State.LastSeq > 0 {
				// JetStream doesn't reindex sequences after deletion, so we need to
				// adjust the count to match file-based behavior by subtracting the
				// number of deleted messages to match file-based implementation behavior
				numDeleted := uint64(len(info.State.Deleted))

				// Calculate the effective total (accounting for deletions)
				effectiveTotal := info.State.LastSeq
				if numDeleted > 0 {
					effectiveTotal = effectiveTotal - numDeleted
				}

				header.TotalEntries = effectiveTotal
			}
		} else {
			n.logger.Debug("Failed to get stream info for header", "error", err)
			// Fall back to our tracked nextEntry or the delegate
		}
	}

	// If we don't have any data yet and there's a delegate, try it as a fallback
	if header.TotalEntries == 0 && n.delegate != nil {
		delegateHeader := n.delegate.GetHeader()
		header.TotalEntries = delegateHeader.TotalEntries
	}

	return header
}

// GetEntry gets an entry from NATS JetStream or falls back to the underlying stream server
func (n *NATSStreamServer) GetEntry(entryNum uint64) (datastreamer.FileEntry, error) {
	// TODO: update return type once old ds has been retired
	// Try to get the entry from NATS JetStream first
	if n.natsManager != nil && n.natsManager.mainStream != nil {
		// Get the stream consumer
		ctx := context.Background()

		// NATS sequence numbers are 1-based, but our entry numbers are 0-based
		// We need to add 1 to convert our entry number to a NATS sequence number
		seqNum := entryNum + 1

		// Try to fetch the message by sequence number
		msg, err := n.natsManager.mainStream.GetMsg(ctx, seqNum)
		if err == nil {
			// Convert NATS message to FileEntry
			fileEntry := datastreamer.FileEntry{}

			// Copy data
			fileEntry.Data = msg.Data

			// Set entry number
			fileEntry.Number = entryNum

			// Try to parse entry type from header
			entryTypeStr := msg.Header.Get("EntryType")
			if entryTypeStr != "" {
				entryType, err := strconv.ParseUint(entryTypeStr, 10, 32)
				if err == nil {
					fileEntry.Type = datastreamer.EntryType(entryType)
				}
			}

			// Set length - calculate based on data size plus fixed header size
			fileEntry.Length = uint32(len(fileEntry.Data)) + 1 + 4 + 4 + 8 // packetType(1) + Length(4) + Type(4) + Number(8)

			return fileEntry, nil
		} else {
			// If error occurred during NATS retrieval, log and fall back to delegate
			n.logger.Debug("Failed to get entry from NATS, falling back to delegate",
				"entryNum", entryNum,
				"seqNum", seqNum,
				"error", err)
		}
	}

	// Fall back to the underlying stream server
	return n.delegate.GetEntry(entryNum)
}

// GetBookmark gets a bookmark from the NATS KV store or falls back to the underlying stream server
func (n *NATSStreamServer) GetBookmark(bookmark []byte) (uint64, error) {
	// Try NATS KV store first if available
	if n.bookmark != nil {
		entryNum, err := n.bookmark.GetBookmark(n.natsManager, bookmark)
		if err == nil {
			return entryNum, nil
		}
		// Log the error but fall back to delegate
		n.logger.Debug("Failed to get bookmark from NATS KV, falling back to delegate",
			"error", err)
	}

	// Fall back to delegate
	return n.delegate.GetBookmark(bookmark)
}

// GetFirstEventAfterBookmark gets the first event after a bookmark from the underlying stream server
func (n *NATSStreamServer) GetFirstEventAfterBookmark(bookmark []byte) (datastreamer.FileEntry, error) {
	// Get the bookmark entry number
	bookmarkEntryNum, err := n.GetBookmark(bookmark)
	if err != nil {
		return datastreamer.FileEntry{}, err
	}

	// Get current header info to know how many entries we have
	header := n.GetHeader()
	if bookmarkEntryNum >= header.TotalEntries-1 {
		return datastreamer.FileEntry{}, fmt.Errorf("no entries after bookmark at position %d", bookmarkEntryNum)
	}

	// Start searching from the entry after the bookmark
	currentEntryNum := bookmarkEntryNum + 1

	// NATS may have deleted messages or more bookmarks, so we need to
	// iterate until we find a valid non-bookmark entry
	for currentEntryNum < header.TotalEntries {
		entry, err := n.GetEntry(currentEntryNum)
		if err == nil {
			// Skip bookmarks, we want the first actual data entry
			if entry.Type != datastreamer.EtBookmark {
				return entry, nil
			}
		}
		// If entry doesn't exist or is a bookmark, try the next one
		currentEntryNum++
	}

	return datastreamer.FileEntry{}, fmt.Errorf("no valid entries found after bookmark at position %d", bookmarkEntryNum)
}

// GetDataBetweenBookmarks gets data between bookmarks from the underlying stream server
func (n *NATSStreamServer) GetDataBetweenBookmarks(bookmarkFrom, bookmarkTo []byte) ([]byte, error) {
	// TODO: This method appears to be unused and should be removed from the interface
	// after the original datastreamer is retired. We're not implementing it for NATS.
	panic("GetDataBetweenBookmarks is not implemented for NATS and appears to be unused. " +
		"This method should be removed from the interface after datastreamer is fully retired.")
}

// BookmarkPrintDump prints the bookmark dump from NATS KV or the underlying stream server
func (n *NATSStreamServer) BookmarkPrintDump() {
	// Try to print from NATS KV if available
	if n.bookmark != nil {
		err := n.bookmark.PrintDump(n.natsManager)
		if err == nil {
			return
		}
		// Log error but continue with delegate
		n.logger.Error("Failed to print bookmarks from NATS KV, falling back to delegate",
			"error", err)
	}

	// Fall back to delegate
	n.delegate.BookmarkPrintDump()
}
