package smt

import (
	"github.com/ledgerwatch/log/v3"
	"sync"
)

// SmtCacheSnapshot defines the node structure for the singly linked list
type SmtCacheSnapshot struct {
	BlcokHeight    uint64
	DeltaSmtCache  map[string]map[string][]byte
	parentSnapshot *SmtCacheSnapshot // Pointer to the parent snapshot
}

// SmtCacheList manages the singly linked list
type SmtCacheList struct {
	head      *SmtCacheSnapshot // Head of the list (most recent snapshot)
	length    int               // Current number of nodes in the list
	maxHeight uint64
	mutex     sync.RWMutex // Read-write mutex for concurrent access
}

// NewSmtCacheList creates a new empty linked list
func NewSmtCacheList() *SmtCacheList {
	return &SmtCacheList{
		head:      nil,
		length:    0,
		maxHeight: uint64(0),
	}
}

// Push adds a new node to the head of the list (helper method)
func (l *SmtCacheList) Push(blockHeight uint64, deltaSmtCache map[string]map[string][]byte) {
	l.mutex.Lock() // Exclusive lock for write operation
	defer l.mutex.Unlock()

	newNode := &SmtCacheSnapshot{
		BlcokHeight:    blockHeight,
		DeltaSmtCache:  deltaSmtCache,
		parentSnapshot: l.head,
	}
	l.head = newNode
	l.maxHeight = blockHeight
	l.length++ // Increment length when adding a node
}

// findNode finds a node by BlockHeight (helper method)
func (l *SmtCacheList) findNode(blockHeight uint64) *SmtCacheSnapshot {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	current := l.head
	for current != nil {
		if current.BlcokHeight == blockHeight {
			return current
		}
		current = current.parentSnapshot
	}
	return nil
}

// cascadeGetCacheShapshot retrieves the DeltaSmtCache for a given BlockHeight and merges it with all parent snapshots,
// return deep copy data if needCopy is true
func (l *SmtCacheList) cascadeGetCacheShapshot(blockHeight uint64, needCopy bool) (map[string]map[string][]byte, bool) {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	// Initialize the merged cache
	mergedCache := make(map[string]map[string][]byte)

	// Find the specified node
	node := l.findNode(blockHeight)
	if node == nil {
		return mergedCache, false
	}

	// Merge DeltaSmtCache from the current node and all parent snapshots
	current := node
	for current != nil {
		for outerKey, innerMap := range current.DeltaSmtCache {
			if _, exists := mergedCache[outerKey]; !exists {
				mergedCache[outerKey] = make(map[string][]byte)
			}
			for innerKey, value := range innerMap {
				// If the key already exists, keep the earliest value (parent priority)
				if _, exists := mergedCache[outerKey][innerKey]; !exists {
					if needCopy {
						valueCopy := make([]byte, len(value))
						copy(valueCopy, value)
						mergedCache[outerKey][innerKey] = valueCopy
					} else {
						mergedCache[outerKey][innerKey] = value
					}
				}
			}
		}
		current = current.parentSnapshot
	}

	return mergedCache, true
}

// getAllCacheShapshot retrieves all DeltaSmtCache and merges it with all parent snapshots
func (l *SmtCacheList) getAllCacheShapshot(needDeepCopy bool) (map[string]map[string][]byte, bool) {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	headBlockNumber := uint64(0)
	if l.head != nil {
		headBlockNumber = l.head.BlcokHeight
	} else {
		return nil, false
	}

	cache, found := l.cascadeGetCacheShapshot(headBlockNumber, needDeepCopy)
	return cache, found

}

// cascadeDeleteCache deletes the node with the specified BlockHeight and all its parent snapshots
func (l *SmtCacheList) cascadeDeleteCache(blockHeight uint64) bool {
	l.mutex.Lock() // Exclusive lock for write operation
	defer l.mutex.Unlock()

	if l.head == nil {
		return false
	}

	// Special case: delete the head node and all its parents
	if l.head.BlcokHeight == blockHeight {
		l.head = nil // Clear the entire list
		l.length = 0 // Reset length to 0 since all nodes are deleted
		return true
	}

	// Traverse to find and delete the node and its parents
	current := l.head
	l.length = 1
	for current.parentSnapshot != nil {
		if current.parentSnapshot.BlcokHeight == blockHeight {
			current.parentSnapshot = nil // Truncate the list, removing the target and its parents
			return true
		} else {
			current = current.parentSnapshot
			l.length += 1
		}
	}
	return false
}

// Traverse prints the list for debugging (helper method)
func (l *SmtCacheList) Traverse() {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	current := l.head
	for current != nil {
		for table, bucket := range current.DeltaSmtCache {
			log.Info("Traverse", "BlockHeight", current.BlcokHeight, "table", table)
			for k, v := range bucket {
				log.Info("Traverse", "\ttable", table, "key", k, "val", string(v))
			}
		}
		current = current.parentSnapshot
	}
}

// Length returns the number of nodes in the list
func (l *SmtCacheList) Length() int {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	return l.length // Directly return cached length
}

// Length returns the number of nodes in the list
func (l *SmtCacheList) MaxBlockHeight() uint64 {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	return l.maxHeight // Directly return cached length
}

// deleteTargetCache deletes the node with the specified BlockHeight
func (l *SmtCacheList) deleteTargetCache(blockHeight uint64) bool {
	l.mutex.Lock() // Exclusive lock for write operation
	defer l.mutex.Unlock()

	if l.head == nil {
		return false
	}

	// Special case: delete the head node and all its parents
	if l.head.BlcokHeight == blockHeight {
		l.head = l.head.parentSnapshot // Delete the head node
		l.length -= 1
		return true
	}

	// Traverse to find and delete the node and its parents
	current := l.head
	for current.parentSnapshot != nil {
		if current.parentSnapshot.BlcokHeight == blockHeight {
			current.parentSnapshot = current.parentSnapshot.parentSnapshot // Delete the target node
			l.length -= 1

			return true
		} else {
			current = current.parentSnapshot
		}
	}

	return false
}

func (l *SmtCacheList) getBlockList() []uint64 {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	if l.length == 0 {
		return []uint64{}
	}

	blockNumberList := make([]uint64, l.length)
	current := l.head
	for current != nil {
		blockNumberList = append(blockNumberList, current.BlcokHeight)
		current = current.parentSnapshot
	}

	return blockNumberList
}
