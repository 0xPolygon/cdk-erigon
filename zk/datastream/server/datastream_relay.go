package server

import (
	"github.com/0xPolygonHermez/zkevm-data-streamer/datastreamer"
)

type StreamRelay struct {
	streamServer StreamServer
}

func CreateStreamRelayServer(streamServer StreamServer) *StreamRelay {
	return &StreamRelay{
		streamServer: streamServer,
	}
}

func (s *StreamRelay) CommitEntryToStream(entryType datastreamer.EntryType, data []byte) error {
	if err := s.streamServer.StartAtomicOp(); err != nil {
		return err
	}
	defer s.streamServer.RollbackAtomicOp()

	if _, err := s.streamServer.AddStreamEntry(entryType, data); err != nil {
		return err
	}

	return s.streamServer.CommitAtomicOp()
}

func (s *StreamRelay) CommitBookmarkToStream(bookmark []byte) error {
	if err := s.streamServer.StartAtomicOp(); err != nil {
		return err
	}
	defer s.streamServer.RollbackAtomicOp()

	if _, err := s.streamServer.AddStreamBookmark(bookmark); err != nil {
		return err
	}

	return s.streamServer.CommitAtomicOp()
}
