package smt

import (
	"github.com/ledgerwatch/log/v3"
	"sync"
)

// SmtCacheSnapshot defines the node structure for the doubly linked list
type SmtCacheSnapshot struct {
	BlockHeight   uint64 // Corrected typo: BlcokHeight -> BlockHeight
	DeltaSmtCache map[string]map[string][]byte
	next          *SmtCacheSnapshot // Pointer to the next (older) block snapshot
	pre           *SmtCacheSnapshot // Pointer to the previous (newer) block snapshot
}

// SmtCacheList manages the doubly linked list
type SmtCacheList struct {
	head      *SmtCacheSnapshot // Head of the list (most recent snapshot)
	tail      *SmtCacheSnapshot // Tail of the list (oldest snapshot)
	length    int               // Current number of nodes in the list
	maxHeight uint64            // Maximum block height in the list
	mutex     sync.RWMutex      // Read-write mutex for concurrent access
}

// NewSmtCacheList creates a new empty doubly linked list
func NewSmtCacheList() *SmtCacheList {
	return &SmtCacheList{
		head:      nil,
		tail:      nil,
		length:    0,
		maxHeight: 0,
	}
}

// Push adds a new node to the head of the list
func (l *SmtCacheList) Push(blockHeight uint64, deltaSmtCache map[string]map[string][]byte) {
	l.mutex.Lock() // Exclusive lock for write operation
	defer l.mutex.Unlock()

	newNode := &SmtCacheSnapshot{
		BlockHeight:   blockHeight,
		DeltaSmtCache: deltaSmtCache,
		next:          l.head,
		pre:           nil,
	}

	if l.head != nil {
		l.head.pre = newNode // Link the old head to the new node
	} else {
		l.tail = newNode // If list was empty, set tail to the new node
	}

	l.head = newNode
	l.maxHeight = blockHeight
	l.length++ // Increment length when adding a node
}

// findNodeUnsafe finds a node by BlockHeight
func (l *SmtCacheList) findNodeUnsafe(blockHeight uint64) *SmtCacheSnapshot {
	current := l.head
	for current != nil {
		if current.BlockHeight == blockHeight {
			return current
		}
		current = current.next
	}
	return nil
}

// cascadeGetCacheShapshot retrieves the DeltaSmtCache for a given BlockHeight and merges it with all older snapshots
func (l *SmtCacheList) cascadeGetCacheShapshot(blockHeight uint64) (map[string]map[string][]byte, bool) {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	// Initialize the merged cache
	mergedCache := make(map[string]map[string][]byte)

	// Find the specified node
	node := l.findNodeUnsafe(blockHeight)
	if node == nil {
		return mergedCache, false
	}

	// Traverse from the found node towards older snapshots (tail)
	current := l.tail
	for current != node.pre {
		for table, bucket := range current.DeltaSmtCache {
			if _, exists := mergedCache[table]; !exists {
				mergedCache[table] = make(map[string][]byte)
			}
			for key, value := range bucket {
				mergedCache[table][key] = value
			}
		}
		current = current.pre
	}

	return mergedCache, true
}

// getAllCacheShapshot retrieves and merges all DeltaSmtCache from the head to the tail
func (l *SmtCacheList) getAllCacheShapshot() (map[string]map[string][]byte, bool) {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	if l.head == nil {
		return nil, false
	}

	return l.cascadeGetCacheShapshot(l.head.BlockHeight)
}

// cascadeDeleteCache deletes the node with the specified BlockHeight and all its older snapshots
func (l *SmtCacheList) cascadeDeleteCache(blockHeight uint64) bool {
	l.mutex.Lock() // Exclusive lock for write operation
	defer l.mutex.Unlock()

	node := l.findNodeUnsafe(blockHeight)
	if node == nil {
		return false
	}

	// If deleting the head, clear everything up to the tail
	if node == l.head {
		l.head = nil
		l.tail = nil
		l.length = 0
		return true
	}

	cur := l.head
	l.length = 0
	for cur != node {
		l.length++
		cur = cur.next
	}

	// Truncate the list from the node to the tail
	l.tail = node.pre
	node.pre.next = nil
	node.pre = nil

	return true
}

// deleteTargetCache deletes only the node with the specified BlockHeight
func (l *SmtCacheList) deleteTargetCache(blockHeight uint64) bool {
	l.mutex.Lock() // Exclusive lock for write operation
	defer l.mutex.Unlock()

	node := l.findNodeUnsafe(blockHeight)
	if node == nil {
		return false
	}

	// Case 1: Deleting the head
	if node == l.head {
		l.head = node.next
		node.next = nil

		l.length--
		if l.head != nil {
			l.head.pre = nil
		} else {
			l.tail = nil
		}
		return true
	}

	// Case 2: Deleting the tail
	if node == l.tail {
		l.tail = node.pre
		node.pre = nil
		l.tail.next = nil

		l.length--
		return true
	}

	// Case 3: Deleting a middle node
	node.pre.next = node.next
	node.next.pre = node.pre
	node.pre = nil
	node.next = nil
	l.length--

	return true
}

// Traverse prints the list for debugging
func (l *SmtCacheList) Traverse() {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	current := l.head
	for current != nil {
		for table, bucket := range current.DeltaSmtCache {
			log.Info("Traverse", "BlockHeight", current.BlockHeight, "table", table)
			for k, v := range bucket {
				log.Info("Traverse", "\ttable", table, "key", k, "val", string(v))
			}
		}
		current = current.next
	}
}

// Length returns the number of nodes in the list
func (l *SmtCacheList) Length() int {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	return l.length
}

// MaxBlockHeight returns the maximum block height in the list
func (l *SmtCacheList) MaxBlockHeight() uint64 {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	return l.maxHeight // Directly return cached length
}

// getBlockList returns a slice of all block heights in the list
func (l *SmtCacheList) getBlockList() []uint64 {
	l.mutex.RLock() // Read lock for read-only operation
	defer l.mutex.RUnlock()

	if l.length == 0 {
		return []uint64{}
	}

	blockNumberList := make([]uint64, 0, l.length)
	current := l.head
	for current != nil {
		blockNumberList = append(blockNumberList, current.BlockHeight)
		current = current.next
	}

	return blockNumberList
}
