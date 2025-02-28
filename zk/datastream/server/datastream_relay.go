package server

import (
	"github.com/0xPolygonHermez/zkevm-data-streamer/datastreamer"
	"github.com/ledgerwatch/erigon/zk/datastream/types"
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

func (s *StreamRelay) GetHighestBlockBookmarkEntry() (uint64, uint64, error) {
	header := s.streamServer.GetHeader()

	if header.TotalEntries == 0 {
		return 0, 0, nil
	}

	entryNum := header.TotalEntries - 1
	var err error
	var entry datastreamer.FileEntry

	for {
		entry, err = s.streamServer.GetEntry(entryNum)
		if err != nil {
			return 0, 0, err
		}
		if uint32(entry.Type) == uint32(types.EntryTypeL2BlockEnd) {
			break
		}
		entryNum -= 1
	}

	l2Block, err := types.UnmarshalL2Block(entry.Data)
	if err != nil {
		return 0, 0, err
	}

	return l2Block.L2BlockNumber, entryNum, nil
}

func (s *StreamRelay) TruncateFromFile(entryNum uint64) error {
	return s.streamServer.TruncateFile(entryNum)
}
