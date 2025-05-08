package server

import (
	"github.com/erigontech/erigon-lib/log/v3"
)

// StreamStoreType identifies the underlying storage implementation
type StreamStoreType string

const (
	// StreamStoreTypeFile represents the legacy file-based storage
	StreamStoreTypeFile StreamStoreType = "file"

	// StreamStoreTypeMDBX represents the MDBX-based storage
	StreamStoreTypeMDBX StreamStoreType = "mdbx"
)

// StreamStoreConfig contains configuration for stream stores
type StreamStoreConfig struct {
	// Common config
	SystemID uint64
	FilePath string
	Logger   log.Logger

	// MDBX specific options
	MDBXMapSize int64
	MDBXMaxDBS  int
	MDBXFlags   uint
}
