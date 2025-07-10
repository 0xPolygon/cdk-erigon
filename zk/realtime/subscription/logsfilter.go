package subscription

import (
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	types2 "github.com/ledgerwatch/erigon/core/types"
)

// LogsFilter is used for both representing log filter for a specific subscriber (RPC daemon usually)
// and "aggregated" log filter representing a union of all subscribers. Therefore, the values in
// the mappings are counters (of type int) and they get deleted when counter goes back to 0
// Also, addAddr and allTopic are int instead of bool because they are also counter, counting
// how many subscribers have this set on
type LogsFilter struct {
	allAddrs       int
	addrs          map[libcommon.Address]int
	allTopics      int
	topics         map[libcommon.Hash]int
	topicsOriginal [][]libcommon.Hash // Original topic filters to be applied before distributing to individual subscribers
	sender         Sub[*types2.Log]   // nil for aggregate subscriber, for appropriate stream server otherwise
}

func (l *LogsFilter) Send(lg *types2.Log) {
	l.sender.Send(lg)
}

func (l *LogsFilter) Close() {
	l.sender.Close()
}

func chooseTopics(filter *LogsFilter, logTopics []libcommon.Hash) bool {
	var found bool
	for _, logTopic := range logTopics {
		if _, ok := filter.topics[logTopic]; ok {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if len(filter.topicsOriginal) > len(logTopics) {
		return false
	}
	for i, sub := range filter.topicsOriginal {
		match := len(sub) == 0 // empty rule set == wildcard
		for _, topic := range sub {
			if logTopics[i] == topic {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	return true
}
