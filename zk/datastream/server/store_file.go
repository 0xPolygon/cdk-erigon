package server

import (
	"time"

	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
)

// FileStreamStore wraps the legacy datastreamer.Server to implement the StreamStore interface
type FileStreamStore struct {
	server *datastreamer.StreamServer
}

// NewFileStreamStore creates a new file-based stream store
func NewFileStreamStore(config *StreamStoreConfig) (*FileStreamStore, error) {
	// Default values for backward compatibility
	inactivityCheckInterval := time.Second * 10
	writeTimeout := time.Second * 3
	inactivityTimeout := time.Second * 120

	server, err := datastreamer.NewServer(
		0, // port is not required for store
		3,
		config.SystemID,
		1,
		config.FilePath,
		writeTimeout,
		inactivityTimeout,
		inactivityCheckInterval,
		nil, // logging config
		nil, // store
	)

	if err != nil {
		return nil, err
	}

	return &FileStreamStore{
		server: server,
	}, nil
}

// AddStreamEntry adds a new entry to the stream
func (fs *FileStreamStore) AddStreamEntry(entryType datastreamer.EntryType, data []byte) (uint64, error) {
	return fs.server.AddStreamEntry(entryType, data)
}

// AddStreamBookmark adds a new bookmark to the stream
func (fs *FileStreamStore) AddStreamBookmark(data []byte) (uint64, error) {
	return fs.server.AddStreamBookmark(data)
}

// GetEntry retrieves an entry from the stream
func (fs *FileStreamStore) GetEntry(entryNum uint64) (datastreamer.FileEntry, error) {
	return fs.server.GetEntry(entryNum)
}

// GetBookmark retrieves a bookmark from the stream
func (fs *FileStreamStore) GetBookmark(data []byte) (uint64, error) {
	return fs.server.GetBookmark(data)
}

// StartAtomicOp starts an atomic operation
func (fs *FileStreamStore) StartAtomicOp() error {
	return nil
}

// CommitAtomicOp commits an atomic operation
func (fs *FileStreamStore) CommitAtomicOp() error {
	return nil
}

// RollbackAtomicOp rolls back an atomic operation
func (fs *FileStreamStore) RollbackAtomicOp() error {
	return fs.server.RollbackAtomicOp()
}

// GetHeader retrieves the header from the stream
func (fs *FileStreamStore) GetHeader() datastreamer.HeaderEntry {
	return fs.server.GetHeader()
}

// TruncateToEntry truncates the stream to the specified entry
func (fs *FileStreamStore) TruncateToEntry(entryNum uint64) error {
	return fs.server.TruncateFile(entryNum)
}

// UpdateEntryData updates the data for an entry
func (fs *FileStreamStore) UpdateEntryData(entryNum uint64, entryType datastreamer.EntryType, data []byte) error {
	return fs.server.UpdateEntryData(entryNum, entryType, data)
}

// GetFirstEventAfterBookmark gets the first event after a bookmark
func (fs *FileStreamStore) GetFirstEventAfterBookmark(bookmark []byte) (datastreamer.FileEntry, error) {
	return fs.server.GetFirstEventAfterBookmark(bookmark)
}

// GetDataBetweenBookmarks gets data between two bookmarks
func (fs *FileStreamStore) GetDataBetweenBookmarks(bookmarkFrom, bookmarkTo []byte) ([]byte, error) {
	return fs.server.GetDataBetweenBookmarks(bookmarkFrom, bookmarkTo)
}

// Start starts the stream store
func (fs *FileStreamStore) Start() error {
	return fs.server.Start()
}

// Stop stops the stream store
func (fs *FileStreamStore) Stop() error {
	// The original datastreamer.Server doesn't have a Stop method
	// This is a no-op for compatibility
	return nil
}

// BookmarkPrintDump prints debug information about bookmarks
func (fs *FileStreamStore) BookmarkPrintDump() {
	fs.server.BookmarkPrintDump()
}

// TruncateFile truncates the stream to the specified entry
func (fs *FileStreamStore) TruncateFile(entryNum uint64) error {
	return fs.server.TruncateFile(entryNum)
}
