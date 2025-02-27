package replayer

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/zk/datastream/client"
	"github.com/ledgerwatch/erigon/zk/datastream/types"
	rpcClient "github.com/ledgerwatch/erigon/zkevm/jsonrpc/client"
	"github.com/ledgerwatch/log/v3"
	"sync/atomic"
	"time"
)

type Replayer struct {
	RemoteDsUrl string
	RpcUrl      string

	isFinished atomic.Bool

	txChan chan []types.L2Transaction
}

func New(remoteUrl, rpcUrl string) *Replayer {
	return &Replayer{
		RemoteDsUrl: remoteUrl,
		RpcUrl:      rpcUrl,
		txChan:      make(chan []types.L2Transaction),
	}
}

func (r *Replayer) Run(ctx context.Context) error {
	log.Info("Connecting to remote datastream server", "url", r.RemoteDsUrl)

	dsClient := client.NewClient(ctx, r.RemoteDsUrl, false, 0, 0, client.DefaultEntryChannelSize)
	err := dsClient.Start()
	if err != nil {
		log.Error("Failed to start datastream client", "error", err)
	}

	header, err := dsClient.GetHeader()
	if err != nil {
		log.Error("Failed to get header", "error", err)
	}
	target := header.TotalEntries
	var progress uint64

	go func() {
		for {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Error("Recovered from panic in ReadAllEntriesToChannel", "panic", r)
					}
				}()

				if err := dsClient.ReadAllEntriesToChannel(); err != nil {
					log.Error("Failed to read all entries to channel, retrying...", "error", err)
					time.Sleep(1 * time.Second)
					return
				}
			}()
		}
	}()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			log.Info("Context done, exiting")
			return nil
		case entry := <-*dsClient.GetEntryChan():
			progress++

			r.processEntry(entry)

			if progress == target {
				r.isFinished.Store(true)
				break loop
			}
		case <-ticker.C:
			if target == 0 {
				continue
			}
			log.Info(fmt.Sprintf("Datastream entries processed: %d/%d (%d%%)", progress, target, (progress*100)/target))
		}
	}

	log.Info("Datastream replayer finished")
	return nil
}

func (r *Replayer) IsFinished() bool {
	return r.isFinished.Load()
}

func (r *Replayer) processEntry(entry interface{}) {
	switch entry.(type) {
	case types.L2Transaction:
		tx := entry.(types.L2Transaction)
		r.txChan <- []types.L2Transaction{tx}
	default:
	}
}

func (r *Replayer) sendTxs() error {
	var allTxs []types.L2Transaction

loop:
	for {
		select {
		case txs := <-r.txChan:
			allTxs = append(allTxs, txs...)
		default:
			if r.IsFinished() {
				break loop
			}
		}
	}

	log.Info("Sending all transactions")

	totalTxs := len(allTxs)
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

	for _, tx := range allTxs {
		if err := r.sendTx(tx); err != nil {
			close(done)
			return err
		}
	}

	close(done)
	log.Info("All transactions sent")
	return nil
}

func (r *Replayer) sendTx(tx types.L2Transaction) error {
	res, err := rpcClient.JSONRPCCall(r.RpcUrl, "eth_sendRawTransaction", tx.Encoded)
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
