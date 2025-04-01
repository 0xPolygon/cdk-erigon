package smt

import (
	"container/heap"
	"sync"
)

// Uint64MinHeap is a thread-safe min-heap for unique uint64 values
type Uint64MinHeap struct {
	data []uint64
	set  map[uint64]struct{} // Used for deduplication
	mu   sync.Mutex
}

// Len returns the number of elements in the heap
func (h *Uint64MinHeap) Len() int {
	return len(h.data)
}

// Less defines the min-heap comparison: i < j
func (h *Uint64MinHeap) Less(i, j int) bool {
	return h.data[i] < h.data[j]
}

// Swap exchanges two elements
func (h *Uint64MinHeap) Swap(i, j int) {
	h.data[i], h.data[j] = h.data[j], h.data[i]
}

// Push adds an element to the heap (required by heap.Interface), WARN: NEVER DIRECTLY USE IT
func (h *Uint64MinHeap) Push(x interface{}) {
	value := x.(uint64)
	if _, exists := h.set[value]; exists {
		return
	}

	h.data = append(h.data, value)
	h.set[value] = struct{}{}
}

// Pop removes and returns the minimum element (required by heap.Interface)
func (h *Uint64MinHeap) Pop() interface{} {
	n := len(h.data)
	x := h.data[n-1] // Note: This removes the last element, but heap ensures it's the min value after adjustment
	h.data = h.data[0 : n-1]
	//delete(h.set, x) // Clean up the set
	return x
}

// NewUint64MinHeap creates and initializes a new thread-safe min-heap
func NewUint64MinHeap() *Uint64MinHeap {
	h := &Uint64MinHeap{
		data: make([]uint64, 0),
		set:  make(map[uint64]struct{}),
	}
	heap.Init(h)
	return h
}

// ThreadSafePush adds an element to the heap in a thread-safe manner if it's not already present
func (h *Uint64MinHeap) ThreadSafePush(value uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if the value already exists
	if _, exists := h.set[value]; exists {
		return false // Duplicate value, discard it
	}

	heap.Push(h, value)
	return true // Successfully added
}

// ThreadSafePop removes and returns the minimum element in a thread-safe manner
func (h *Uint64MinHeap) ThreadSafePop() (uint64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.Len() == 0 {
		return 0, false // Heap is empty
	}

	// heap.Pop calls Pop(), which already handles set cleanup
	min := heap.Pop(h).(uint64)
	return min, true
}

// ThreadSafeTop returns the minimum element without removing it, thread-safely
func (h *Uint64MinHeap) ThreadSafeTop() (uint64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.Len() == 0 {
		return 0, false // Heap is empty
	}
	return h.data[0], true // Top of the heap is the minimum value
}

// Contains checks if a value exists in the heap
func (h *Uint64MinHeap) Contains(value uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	_, exists := h.set[value]
	return exists
}
