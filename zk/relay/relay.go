package relay

import (
	"context"
	"fmt"
	"github.com/0xPolygonHermez/zkevm-data-streamer/datastreamer"
	log2 "github.com/0xPolygonHermez/zkevm-data-streamer/log"
	"github.com/ledgerwatch/erigon/zk/datastream/client"
	"github.com/ledgerwatch/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/ledgerwatch/erigon/zk/datastream/server"
	"github.com/ledgerwatch/erigon/zk/datastream/types"
	"github.com/ledgerwatch/log/v3"
	"math"
	"os"
	"os/signal"
	"time"
)

type Relay struct {
	ctx         context.Context
	client      *client.StreamClient
	server      server.StreamServer
	serverRelay server.DatastreamRelay
}

func NewRelay(ctx context.Context, remoteDsUrl string, relayPort uint, streamDir string) (*Relay, error) {
	remoteClient := client.NewClient(ctx, remoteDsUrl, false, 0, 0, client.DefaultEntryChannelSize)

	path := streamDir + "/data-stream"
	if err := os.MkdirAll(path, 0755); err != nil {
		log.Error("Failed to create directory", "error", err)
		return nil, err
	}

	serverFactory := server.NewZkEVMDataStreamServerFactory()

	logConfig := &log2.Config{
		Environment: "production",
		Level:       "info",
		Outputs:     []string{},
	}

	streamServer, err := serverFactory.CreateStreamServer(uint16(relayPort), 1, datastreamer.StreamType(1), path, 5*time.Second, 10*time.Second, 60*time.Second, logConfig)
	if err != nil {
		log.Error("Failed to create datastream server", "error", err)
		return nil, err
	}

	serverRelay := server.CreateStreamRelayServer(streamServer)

	return &Relay{
		ctx:         ctx,
		client:      remoteClient,
		server:      streamServer,
		serverRelay: serverRelay,
	}, nil
}

func (r *Relay) Run() error {
	if err := r.server.Start(); err != nil {
		log.Error("Failed to start stream server", "error", err)
		return err
	}

	localHead := r.server.GetHeader()

	log.Info("Reading local datastream entries", "entries", localHead.TotalEntries)

	if localHead.TotalEntries > 0 {
		blockNum, entryNum, err := r.serverRelay.GetHighestBlockBookmarkEntry()
		if err != nil {
			log.Error("Failed to get highest block number", "error", err)
			return err
		}

		if blockNum > 0 {
			log.Info("Truncating file", "blockNum", blockNum, "entryNum", entryNum)

			if err = r.serverRelay.TruncateFromFile(entryNum); err != nil {
				log.Error("Failed to truncate file", "error", err)
			}

			r.client.GetProgressAtomic().Store(blockNum)
		}

		newLocalHead := r.server.GetHeader()

		log.Info("Local datastream entries after truncation", "entries", newLocalHead.TotalEntries)
	}

	defer r.client.Stop()
	if err := r.client.Start(); err != nil {
		log.Error("Failed to start remote client", "error", err)
		return err
	}

	_, err := r.client.GetHeader()
	if err != nil {
		log.Error("Failed to get header", "error", err)
		return err
	}

	// correctness check
	if err = r.performCorrectnessCheck(r.processFileEntry); err != nil {
		log.Error("Failed to perform correctness check", "error", err)
		return err
	}

	go func() {
		for {
			bm := r.progressBookmark()

			log.Info("Datastream execution started", "value", bm.GetValue())

			if err := r.client.ExecutePerFile(bm, r.commitFileEntry); err != nil {
				log.Error("Failed to execute per file", "error", err)
			}

			time.Sleep(10 * time.Second)
		}
	}()

	tickerRemote := time.NewTicker(10 * time.Second)
	defer tickerRemote.Stop()

	tickerLocal := time.NewTicker(10 * time.Second)
	defer tickerLocal.Stop()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	for {
		select {
		case <-r.ctx.Done():
			return nil
		case <-signals:
			log.Info("Shutting down datastream server")
			return nil
		case <-tickerRemote.C:
			if r.client.GetEntryNumberLimit() == 0 {
				continue
			}
			log.Info(fmt.Sprintf("Datastream entries processed: %d/%d (%d%%)", r.client.GetLastWrittenEntryAtomic().Load(), r.client.GetEntryNumberLimit(), (r.client.GetLastWrittenEntryAtomic().Load()*100)/r.client.GetEntryNumberLimit()))
		case <-tickerLocal.C:
			log.Debug(fmt.Sprintf("Local datastream entries: %d", r.server.GetHeader().TotalEntries))
		}
	}
}

func (r *Relay) commitFileEntry(file *types.FileEntry) error {
	var et datastreamer.EntryType
	var bookmark *types.BookmarkProto
	var bmProto []byte
	var err error

	switch file.EntryType {
	case types.EntryTypeL2BlockEnd:
		et = 6
	case types.BookmarkEntryType:
		et = 176

		bookmark, err = types.UnmarshalBookmark(file.Data)
		if err != nil {
			return err
		}

		if bookmark.BookmarkType() == datastream.BookmarkType_BOOKMARK_TYPE_BATCH {
			bmProto, err = bookmark.Marshal()
			if err != nil {
				return err
			}

			return r.serverRelay.CommitBookmarkToStream(bmProto)
		}

		if bookmark.BookmarkType() == datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK {
			var blockNum *types.FullL2Block
			blockNum, err = types.UnmarshalL2Block(file.Data)
			if err != nil {
				return err
			}

			progBlock := r.client.GetProgressAtomic().Load()
			if blockNum.L2BlockNumber > progBlock {
				r.client.GetProgressAtomic().Store(blockNum.L2BlockNumber)
			}

			bmProto, err = bookmark.Marshal()
			if err != nil {
				return err
			}

			return r.serverRelay.CommitBookmarkToStream(bmProto)
		}

		if bookmark.BookmarkType() == datastream.BookmarkType_BOOKMARK_TYPE_UNSPECIFIED {
			log.Warn("Unspecified bookmark type", "bookmarkType", bookmark.BookmarkType())

			bmProto, err = bookmark.Marshal()
			if err != nil {
				return err
			}

			return r.serverRelay.CommitBookmarkToStream(bmProto)
		}

		log.Error("Unexpected bookmark type", "bookmarkType", bookmark.BookmarkType())
		return nil
	case types.EntryTypeBatchStart:
		et = 1
	case types.EntryTypeBatchEnd:
		et = 4
	case types.EntryTypeL2Tx:
		et = 3
	case types.EntryTypeL2Block:
		et = 2
	case types.EntryTypeGerUpdate:
		et = 5
	case types.EntryTypeUnspecified:
		et = 0
	case types.EntryTypeNotFound:
		et = math.MaxUint32
	default:
		log.Error("Unexpected entry type", "entryType", file.EntryType)
	}

	return r.serverRelay.CommitEntryToStream(et, file.Data)
}

func (r *Relay) progressBookmark() *types.BookmarkProto {
	return types.NewBookmarkProto(r.client.GetProgressAtomic().Load(), datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK)
}

func (r *Relay) performCorrectnessCheck(function func(file datastreamer.FileEntry) error) error {
	totalLocalEntries := r.server.GetHeader().TotalEntries
	if totalLocalEntries == 0 {
		log.Debug("Local datastream is empty")
		return nil
	}

	var startEntryNum uint64 = 1
	for {
		if startEntryNum == totalLocalEntries {
			log.Debug("Entry check completed", "startEntryNum", startEntryNum, "totalLocalEntries", totalLocalEntries)
			return nil
		}

		serverEntry, err := r.server.GetEntry(startEntryNum)
		if err != nil {
			return err
		}

		if err = function(serverEntry); err != nil {
			return err
		}

		startEntryNum = serverEntry.Number + 1
	}
}

func (r *Relay) processFileEntry(file datastreamer.FileEntry) error {
	switch file.Type {
	case 0:
		// EntryTypeUnspecified
	case 1:
		// EntryTypeBatchStart
	case 2:
		// EntryTypeL2Block
	case 3:
		// EntryTypeL2Tx
	case 4:
		// EntryTypeBatchEnd
	case 5:
		// EntryTypeGerUpdate
	case 176:
		// BookmarkEntryType
	case math.MaxUint32:
		// EntryTypeNotFound
	default:
		log.Error("Unexpected entry type", "entryType", file.Type)
	}
	return nil
}
