package server

import (
	"sync"

	"github.com/ledgerwatch/erigon/zk/datastream/client"
	"github.com/ledgerwatch/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/ledgerwatch/erigon/zk/datastream/types"
)

func (srv *ZkEVMDataStreamServer) ReadBatchesWithConcurrency(start uint64, end uint64) ([][]*types.FullL2Block, error) {
	batches := make([][]*types.FullL2Block, end-start+1)
	batchPositions := make([]uint64, end-start+1)
	var wg sync.WaitGroup
	errCh := make(chan error, end-start+1)

	for i := start; i <= end; i++ {
		wg.Add(1)
		go func(batchNum uint64) {
			defer wg.Done()
			bookmark := types.NewBookmarkProto(batchNum, datastream.BookmarkType_BOOKMARK_TYPE_BATCH)
			marshalled, err := bookmark.Marshal()
			if err != nil {
				errCh <- err
				return
			}
			pos, err := srv.streamServer.GetBookmark(marshalled)
			if err != nil {
				errCh <- err
				return
			}
			batchPositions[batchNum-start] = pos
		}(i)
	}
	wg.Wait()

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	// worker pool
	maxWorkers := 16
	jobs := make(chan uint64, end-start+1)
	wg = sync.WaitGroup{}

	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batchNum := range jobs {
				iterator := newDataStreamServerIterator(srv.streamServer, batchPositions[batchNum-start])
				blocks := []*types.FullL2Block{}
			LOOP_ENTRIES:
				for {
					parsedProto, _, err := client.ReadParsedProto(iterator)
					if err != nil {
						errCh <- err
						return
					}
					if parsedProto == nil {
						break
					}
					switch parsedProto := parsedProto.(type) {
					case *types.BatchStart:
						continue
					case *types.BatchEnd:
						if parsedProto.Number == batchNum {
							break LOOP_ENTRIES
						}
					case *types.FullL2Block:
						if parsedProto.BatchNumber == batchNum {
							blocks = append(blocks, parsedProto)
						}
					default:
						continue
					}
				}
				batches[batchNum-start] = blocks
			}
		}()
	}

	for i := start; i <= end; i++ {
		select {
		case err := <-errCh:
			close(jobs)
			return nil, err
		default:
			jobs <- i
		}
	}
	close(jobs)
	wg.Wait()

	return batches, nil
}
