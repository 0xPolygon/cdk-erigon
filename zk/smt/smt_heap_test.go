package smt

import (
	"sync"
	"testing"
)

// TestUint64MinHeap tests the functionality of the thread-safe Uint64MinHeap
func TestUint64MinHeap(t *testing.T) {
	// Test case 1: Basic operations on an empty heap
	t.Run("BasicOperations", func(t *testing.T) {
		h := NewUint64MinHeap()

		// Check empty heap
		if val, ok := h.ThreadSafeTop(); ok {
			t.Errorf("Expected empty heap, but got top value %d", val)
		}
		if val, ok := h.ThreadSafePop(); ok {
			t.Errorf("Expected empty heap, but popped value %d", val)
		}

		// Push values
		h.ThreadSafePush(5)
		h.ThreadSafePush(2)
		h.ThreadSafePush(7)
		h.ThreadSafePush(1)
		h.ThreadSafePush(2)
		h.ThreadSafePush(5)

		// Check top value
		if top, ok := h.ThreadSafeTop(); !ok || top != 1 {
			t.Errorf("Expected top value 1, got %d (ok=%v)", top, ok)
		}

		// Check heap size
		if h.Len() != 4 {
			t.Errorf("Expected heap size 4, got %d", h.Len())
		}

		// Pop values and verify order
		expected := []uint64{1, 2, 5, 7}
		for i, exp := range expected {
			if val, ok := h.ThreadSafePop(); !ok || val != exp {
				t.Errorf("Pop #%d: expected %d, got %d (ok=%v)", i, exp, val, ok)
			}
		}

		// Check empty heap again
		if h.Len() != 0 {
			t.Errorf("Expected empty heap after pops, got size %d", h.Len())
		}

		h.ThreadSafePush(5) // already seen, will not accept it

		// Check empty heap again
		if h.Len() != 0 {
			t.Errorf("Expected empty heap after pops, got size %d", h.Len())
		}
	})

	// Test case 2: Concurrent operations
	t.Run("ConcurrentOperations", func(t *testing.T) {
		h := NewUint64MinHeap()
		var wg sync.WaitGroup

		// Concurrent pushes
		pushValues := []uint64{5, 2, 7, 1, 4}
		for _, val := range pushValues {
			wg.Add(1)
			go func(v uint64) {
				defer wg.Done()
				h.ThreadSafePush(v)
			}(val)
		}
		wg.Wait()

		// Verify heap size
		if h.Len() != len(pushValues) {
			t.Errorf("Expected heap size %d after concurrent pushes, got %d", len(pushValues), h.Len())
		}

		// Concurrent pops
		popped := make([]uint64, 0, len(pushValues))
		var mu sync.Mutex
		for i := 0; i < len(pushValues); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if val, ok := h.ThreadSafePop(); ok {
					mu.Lock()
					popped = append(popped, val)
					mu.Unlock()
				}
			}()
		}
		wg.Wait()

		// Verify all values were popped
		if len(popped) != len(pushValues) {
			t.Errorf("Expected %d values popped, got %d", len(pushValues), len(popped))
		}

		// Verify min-heap order (sorted ascending)
		for i := 1; i < len(popped); i++ {
			if popped[i-1] > popped[i] {
				t.Errorf("Heap order violated: %d > %d at index %d", popped[i-1], popped[i], i)
			}
		}
	})

	// Test case 3: Edge case - single element
	t.Run("SingleElement", func(t *testing.T) {
		h := NewUint64MinHeap()

		// Push one value
		h.ThreadSafePush(42)

		// Check top
		if top, ok := h.ThreadSafeTop(); !ok || top != 42 {
			t.Errorf("Expected top value 42, got %d (ok=%v)", top, ok)
		}

		// Pop and verify
		if val, ok := h.ThreadSafePop(); !ok || val != 42 {
			t.Errorf("Expected popped value 42, got %d (ok=%v)", val, ok)
		}

		// Check empty
		if h.Len() != 0 {
			t.Errorf("Expected empty heap, got size %d", h.Len())
		}
	})
}
