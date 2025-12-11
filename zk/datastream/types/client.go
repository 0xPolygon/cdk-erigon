package types

import (
	"sync/atomic"
)

// DatastreamClient defines the interface for clients that interact with data streams
type DatastreamClient interface {
	RenewEntryChannel()
	RenewMaxEntryChannel()
	ReadAllEntriesToChannel() error
	StopReadingToChannel()
	GetEntryChan() *chan interface{}
	GetL2BlockByNumber(blockNum uint64) (*FullL2Block, error)
	GetLatestL2Block() (*FullL2Block, error)
	GetProgressAtomic() *atomic.Uint64
	Start() error
	Stop() error
	HandleStart() error
	ExecutePerFile(bookmark *BookmarkProto, function func(file *FileEntry) error) error
}
