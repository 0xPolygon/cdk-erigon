package subscription

import (
	"context"
	"fmt"
	"sync"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/eth/filters"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
)

const (
	DefaultChannelSize          = 1000
	DefaultSubscribeChannelSize = 256

	// Limit the number of subscriptions on the node
	MaxSubscriptionsCount = 100
)

type RealtimeSubMessage struct {
	BlockMsg *kafkaTypes.BlockMessage
	TxMsg    *kafkaTypes.TransactionMessage
}

type RealtimeSubscription struct {
	rtSubs     *SyncMap[SubID, Sub[RealtimeSubMessage]]
	logsSubs   *SyncMap[SubID, *LogsFilter]
	newMsgChan chan RealtimeSubMessage
}

func NewRealtimeSubscription() *RealtimeSubscription {
	return &RealtimeSubscription{
		rtSubs:     NewSyncMap[SubID, Sub[RealtimeSubMessage]](),
		logsSubs:   NewSyncMap[SubID, *LogsFilter](),
		newMsgChan: make(chan RealtimeSubMessage, DefaultChannelSize),
	}
}

func (ff *RealtimeSubscription) Start(ctx context.Context) {
	var wg sync.WaitGroup
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-ff.newMsgChan:
				wg.Add(1)
				go func() {
					defer wg.Done()
					ff.handleRealtimeMsgs(ctx, msg)
				}()
				if msg.TxMsg != nil {
					wg.Add(1)
					go func() {
						defer wg.Done()
						ff.handleRealtimeLogMsgs(ctx, msg.TxMsg)
					}()
				}
				wg.Wait()
			}
		}
	}()
}

func (ff *RealtimeSubscription) handleRealtimeMsgs(ctx context.Context, msg RealtimeSubMessage) {
	ff.rtSubs.Range(func(k SubID, v Sub[RealtimeSubMessage]) error {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		v.Send(msg)
		return nil
	})
}

func (ff *RealtimeSubscription) handleRealtimeLogMsgs(ctx context.Context, txMsg *kafkaTypes.TransactionMessage) {
	logs := txMsg.Receipt.Logs
	for _, log := range logs {
		ff.logsSubs.Range(func(k SubID, filter *LogsFilter) error {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			if filter.allAddrs == 0 {
				_, addrOk := filter.addrs[log.Address]
				if !addrOk {
					return nil
				}
			}
			if filter.allTopics == 0 {
				if !chooseTopics(filter, log.Topics) {
					return nil
				}
			}
			filter.sender.Send(log)
			return nil
		})
	}
}

func (ff *RealtimeSubscription) BroadcastNewMsg(blockMsg *kafkaTypes.BlockMessage, txMsg *kafkaTypes.TransactionMessage) {
	msg := RealtimeSubMessage{
		BlockMsg: blockMsg,
		TxMsg:    txMsg,
	}
	ff.newMsgChan <- msg
}

func (ff *RealtimeSubscription) SubscribeRealtime() (<-chan RealtimeSubMessage, SubID, error) {
	if ff.rtSubs.Len()+ff.logsSubs.Len() > MaxSubscriptionsCount {
		return nil, "", fmt.Errorf("max subscriptions count reached")
	}

	id := SubID(generateSubID())
	sub := newChanSub[RealtimeSubMessage](DefaultSubscribeChannelSize)
	ff.rtSubs.Put(id, sub)
	return sub.ch, id, nil
}

func (ff *RealtimeSubscription) UnsubscribeRealtime(id SubID) bool {
	ch, ok := ff.rtSubs.Get(id)
	if !ok {
		return false
	}
	ch.Close()
	if _, ok = ff.rtSubs.Delete(id); !ok {
		return false
	}
	return true
}

func (ff *RealtimeSubscription) SubscribeRealtimeLogs(size int, crit filters.FilterCriteria) (<-chan *types.Log, SubID, error) {
	if ff.rtSubs.Len()+ff.logsSubs.Len() > MaxSubscriptionsCount {
		return nil, "", fmt.Errorf("max subscriptions count reached")
	}

	id := SubID(generateSubID())
	sub := newChanSub[*types.Log](size)
	filter := &LogsFilter{addrs: map[libcommon.Address]int{}, topics: map[libcommon.Hash]int{}, sender: sub}
	filter.addrs = map[libcommon.Address]int{}
	if len(crit.Addresses) == 0 {
		filter.allAddrs = 1
	} else {
		for _, addr := range crit.Addresses {
			filter.addrs[addr] = 1
		}
	}
	filter.topics = map[libcommon.Hash]int{}
	if len(crit.Topics) == 0 {
		filter.allTopics = 1
	} else {
		for _, topics := range crit.Topics {
			for _, topic := range topics {
				filter.topics[topic] = 1
			}
		}
	}
	filter.topicsOriginal = crit.Topics
	ff.logsSubs.Put(id, filter)

	return sub.ch, id, nil
}

func (ff *RealtimeSubscription) UnsubscribeRealtimeLogs(id SubID) bool {
	filter, ok := ff.logsSubs.Get(id)
	if !ok {
		return false
	}
	filter.Close()
	if _, ok = ff.logsSubs.Delete(id); !ok {
		return false
	}
	return true
}
