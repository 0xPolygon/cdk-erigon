package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/zk/datastream/client"
	"github.com/ledgerwatch/erigon/zk/datastream/types"
	rpcClient "github.com/ledgerwatch/erigon/zkevm/jsonrpc/client"
	"github.com/ledgerwatch/log/v3"
	"os"
	"sync/atomic"
	"time"
)

type Replayer struct {
	RemoteDsUrl   string
	RpcUrl        string
	isFinished    atomic.Bool
	encodedTxChan chan [][]byte
}

func NewReplayer(remoteUrl, rpcUrl string) *Replayer {
	return &Replayer{
		RemoteDsUrl:   remoteUrl,
		RpcUrl:        rpcUrl,
		encodedTxChan: make(chan [][]byte),
	}
}

func (r *Replayer) Run(ctx context.Context) error {
	log.Info("Connecting to remote datastream server", "url", r.RemoteDsUrl)

	dsClient := client.NewClient(ctx, r.RemoteDsUrl, false, 0, 0, client.DefaultEntryChannelSize)

	go func() {
		var prog uint64
		for {
			prog = r.startReading(dsClient, prog)

			if r.IsFinished() {
				break
			}
			time.Sleep(10 * time.Second)
		}
	}()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	entryChan := dsClient.GetEntryChan()
	var allData []byte

loop:
	for {
		select {
		case <-ctx.Done():
			log.Info("Context done, exiting")
			return nil
		case entry := <-*entryChan:
			if breakLoop := r.processEntry(entry, dsClient); breakLoop {
				break loop
			}

			if dsClient.GetLastWrittenEntryAtomic().Load() == dsClient.GetEntryNumberLimit() {
				r.isFinished.Store(true)
				break loop
			}
		case <-ticker.C:
			if dsClient.GetEntryNumberLimit() == 0 {
				continue
			}
			log.Info(fmt.Sprintf("Datastream entries processed: %d/%d (%d%%)", dsClient.GetLastWrittenEntryAtomic().Load(), dsClient.GetEntryNumberLimit(), (dsClient.GetLastWrittenEntryAtomic().Load()*100)/dsClient.GetEntryNumberLimit()))
		}
	}

	if err := os.WriteFile("tx.bin", allData, 0644); err != nil {
		log.Error("Failed to write batch data", "error", err)
		return err
	}

	log.Info("Datastream replayer finished")
	return nil
}

func (r *Replayer) startReading(dsClient *client.StreamClient, progress uint64) uint64 {
	if err := dsClient.HandleStart(); err != nil {
		log.Error("Failed to start datastream client", "error", err)
	}
	defer dsClient.Stop()

	if progress == 0 {
		log.Info("Progress is 0, starting from the beginning", "progress", progress)
	} else {
		dsClient.GetProgressAtomic().Store(progress)
		log.Info("Resuming from progress", "progress", progress)
	}

	if err := dsClient.ReadAllEntriesToChannel(); err != nil {
		prog := dsClient.GetProgressAtomic().Load()
		log.Error("Failed to read all entries to channel", "progress", prog, "error", err)
		return prog
	}

	prog := dsClient.GetProgressAtomic().Load()

	return prog
}

func (r *Replayer) IsFinished() bool {
	return r.isFinished.Load()
}

func (r *Replayer) processEntry(e interface{}, dsClient *client.StreamClient) bool {
	switch entry := e.(type) {
	case *types.L2Transaction:
		return false
	case *types.FullL2Block:
		blockNum := entry.L2BlockNumber
		prog := dsClient.GetProgressAtomic().Load()
		if blockNum > prog {
			dsClient.GetProgressAtomic().Store(blockNum)
		}
		return false
	case *types.BatchStart:
		return false
	case *types.BatchEnd:
		return false
	case *types.GerUpdate:
		return false
	case nil:
		r.isFinished.Store(true)
		return true
	default:
	}

	return false
}

func (r *Replayer) sendTxs() error {
	var allEncodedTxs [][]byte

loop:
	for {
		select {
		case txs := <-r.encodedTxChan:
			allEncodedTxs = append(allEncodedTxs, txs...)
		default:
			if r.IsFinished() {
				break loop
			}
		}
	}

	log.Info("Sending all transactions")

	totalTxs := len(allEncodedTxs)
	var progress int64

	ticker := time.NewTicker(5 * time.Second)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				current := atomic.LoadInt64(&progress)
				log.Info(fmt.Sprintf("Transactions sent: %d/%d (%d%%)", current, totalTxs, (current*100)/int64(totalTxs)))
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	for _, tx := range allEncodedTxs {
		if err := r.sendTx(tx); err != nil {
			close(done)
			return err
		}
	}

	close(done)
	log.Info("All transactions sent")
	return nil
}

func (r *Replayer) sendTx(tx []byte) error {
	res, err := rpcClient.JSONRPCCall(r.RpcUrl, "eth_sendRawTransaction", tx)
	if err != nil {
		log.Error("Failed to send transaction", "error", err)
		return err
	}

	var txHash common.Hash
	if err = json.Unmarshal(res.Result, &txHash); err != nil {
		log.Error("Failed to unmarshal transaction hash", "error", err)
		return err
	}

	log.Info("Transaction sent", "hash", txHash)

	return nil
}
