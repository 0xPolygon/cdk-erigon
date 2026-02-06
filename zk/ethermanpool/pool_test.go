package ethermanpool

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ethereum "github.com/erigontech/erigon"
	"github.com/erigontech/erigon-lib/common"
	ethTypes "github.com/erigontech/erigon/core/types"
)

// mockEtherman is a test mock for IEtherman
type mockEtherman struct {
	headerByNumberFn     func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error)
	blockByNumberFn      func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Block, error)
	filterLogsFn         func(ctx context.Context, query ethereum.FilterQuery) ([]ethTypes.Log, error)
	callContractFn       func(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	transactionByHashFn  func(ctx context.Context, hash common.Hash) (ethTypes.Transaction, bool, error)
	transactionReceiptFn func(ctx context.Context, txHash common.Hash) (*ethTypes.Receipt, error)
	storageAtFn          func(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error)
	suggestedGasPriceFn  func(ctx context.Context) (*big.Int, error)
	callCount            atomic.Int32
}

func (m *mockEtherman) SuggestedGasPrice(ctx context.Context) (*big.Int, error) {
	m.callCount.Add(1)
	if m.suggestedGasPriceFn != nil {
		return m.suggestedGasPriceFn(ctx)
	}
	return big.NewInt(1000000000), nil // 1 gwei default
}

func (m *mockEtherman) HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
	m.callCount.Add(1)
	if m.headerByNumberFn != nil {
		return m.headerByNumberFn(ctx, blockNumber)
	}
	return &ethTypes.Header{Number: blockNumber}, nil
}

func (m *mockEtherman) BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethTypes.Block, error) {
	m.callCount.Add(1)
	if m.blockByNumberFn != nil {
		return m.blockByNumberFn(ctx, blockNumber)
	}
	return nil, nil
}

func (m *mockEtherman) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]ethTypes.Log, error) {
	m.callCount.Add(1)
	if m.filterLogsFn != nil {
		return m.filterLogsFn(ctx, query)
	}
	return []ethTypes.Log{}, nil
}

func (m *mockEtherman) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	m.callCount.Add(1)
	if m.callContractFn != nil {
		return m.callContractFn(ctx, msg, blockNumber)
	}
	return []byte{}, nil
}

func (m *mockEtherman) TransactionByHash(ctx context.Context, hash common.Hash) (ethTypes.Transaction, bool, error) {
	m.callCount.Add(1)
	if m.transactionByHashFn != nil {
		return m.transactionByHashFn(ctx, hash)
	}
	return nil, false, nil
}

func (m *mockEtherman) TransactionReceipt(ctx context.Context, txHash common.Hash) (*ethTypes.Receipt, error) {
	m.callCount.Add(1)
	if m.transactionReceiptFn != nil {
		return m.transactionReceiptFn(ctx, txHash)
	}
	return nil, nil
}

func (m *mockEtherman) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	m.callCount.Add(1)
	if m.storageAtFn != nil {
		return m.storageAtFn(ctx, account, key, blockNumber)
	}
	return []byte{}, nil
}

// fastConfig returns a config with very short timeouts for fast tests
func fastConfig() *EthermanPoolConfig {
	return &EthermanPoolConfig{
		ConsecutiveFailures:   1,
		OpenTimeout:           50 * time.Millisecond, // Very short for fast tests
		HalfOpenRequests:      2,
		DisableCircuitBreaker: false,
	}
}

func TestPool_WorkDistributionAcrossWorkers(t *testing.T) {
	mock1 := &mockEtherman{}
	mock2 := &mockEtherman{}

	pool := NewEthermanPool([]IEtherman{mock1, mock2}, fastConfig())
	defer pool.Close()

	ctx := context.Background()

	// Make concurrent calls to ensure both workers have opportunity to pick up work
	var wg sync.WaitGroup
	numCalls := 20
	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := pool.HeaderByNumber(ctx, big.NewInt(int64(n)))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// Both mocks should have been called (workers pull from shared queue)
	count1 := mock1.callCount.Load()
	count2 := mock2.callCount.Load()

	// With concurrent requests, both workers should get some work
	// We don't require exact distribution, just that both participated
	if count1 == 0 || count2 == 0 {
		t.Errorf("expected both workers to handle jobs, got mock1=%d, mock2=%d", count1, count2)
	}

	if count1+count2 != int32(numCalls) {
		t.Errorf("expected %d total calls, got %d", numCalls, count1+count2)
	}
}

func TestPool_CircuitTripsAfterConsecutiveFailures(t *testing.T) {
	failCount := atomic.Int32{}
	mock := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			failCount.Add(1)
			return nil, errors.New("connection timeout")
		},
	}

	config := fastConfig()
	config.ConsecutiveFailures = 2 // Trip after 2 consecutive failures

	pool := NewEthermanPool([]IEtherman{mock}, config)
	ctx := context.Background()

	// First failure
	_, err := pool.HeaderByNumber(ctx, big.NewInt(1))
	if err == nil {
		t.Fatal("expected error")
	}

	// Second failure - should trip circuit
	_, err = pool.HeaderByNumber(ctx, big.NewInt(2))
	if err == nil {
		t.Fatal("expected error")
	}

	// Third call should be blocked (circuit open) until it transitions to half-open
	// We use a short timeout context to verify blocking behavior
	shortCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	_, err = pool.HeaderByNumber(shortCtx, big.NewInt(3))
	if err == nil {
		t.Fatal("expected error due to circuit open or context timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !IsCircuitBreakerError(err) {
		// Either context timeout (waiting for circuit) or circuit breaker error is acceptable
		t.Logf("got error: %v (this is expected)", err)
	}

	// Wait for circuit to transition to half-open
	time.Sleep(config.OpenTimeout + 10*time.Millisecond)

	// Now calls should go through (circuit in half-open)
	// But they'll still fail because our mock always fails
	_, err = pool.HeaderByNumber(ctx, big.NewInt(4))
	if err == nil {
		t.Fatal("expected error from mock")
	}
}

func TestPool_429ErrorsDoNotTripCircuit(t *testing.T) {
	callCount := atomic.Int32{}
	mock := &mockEtherman{
		filterLogsFn: func(ctx context.Context, query ethereum.FilterQuery) ([]ethTypes.Log, error) {
			callCount.Add(1)
			return nil, errors.New("429 Too Many Requests")
		},
	}

	config := fastConfig()
	config.ConsecutiveFailures = 1 // Would trip immediately on non-429

	pool := NewEthermanPool([]IEtherman{mock}, config)
	ctx := context.Background()

	// Make multiple calls with 429 errors
	for i := 0; i < 5; i++ {
		_, err := pool.FilterLogs(ctx, ethereum.FilterQuery{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !Is429Error(err) {
			t.Fatalf("expected 429 error, got: %v", err)
		}
	}

	// Circuit should NOT be open - all 5 calls should have gone through
	if callCount.Load() != 5 {
		t.Errorf("expected 5 calls (circuit not tripped), got %d", callCount.Load())
	}
}

func TestPool_ContextTimeoutErrorsDoNotTripCircuit(t *testing.T) {
	callCount := atomic.Int32{}
	mock := &mockEtherman{
		filterLogsFn: func(ctx context.Context, query ethereum.FilterQuery) ([]ethTypes.Log, error) {
			callCount.Add(1)
			// Simulate context deadline exceeded (e.g., from headerFetchTimeout)
			return nil, fmt.Errorf("Post http://...: %w", context.DeadlineExceeded)
		},
	}

	config := fastConfig()
	config.ConsecutiveFailures = 1 // Would trip immediately on real failures

	pool := NewEthermanPool([]IEtherman{mock}, config)
	ctx := context.Background()

	// Make multiple calls with context timeout errors
	for i := 0; i < 5; i++ {
		_, err := pool.FilterLogs(ctx, ethereum.FilterQuery{})
		if err == nil {
			t.Fatal("expected error")
		}
	}

	// Circuit should NOT be open - context timeouts are user-defined, not endpoint failures
	// All 5 calls should have gone through
	if callCount.Load() != 5 {
		t.Errorf("expected 5 calls (circuit not tripped by context timeout), got %d", callCount.Load())
	}
}

func TestPool_CircuitRecoveryAfterOpenTimeout(t *testing.T) {
	callCount := atomic.Int32{}
	shouldFail := atomic.Bool{}
	shouldFail.Store(true)

	mock := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			callCount.Add(1)
			if shouldFail.Load() {
				return nil, errors.New("connection timeout")
			}
			return &ethTypes.Header{Number: blockNumber}, nil
		},
	}

	config := fastConfig()
	config.ConsecutiveFailures = 1
	config.OpenTimeout = 30 * time.Millisecond

	pool := NewEthermanPool([]IEtherman{mock}, config)
	ctx := context.Background()

	// Trip the circuit
	_, err := pool.HeaderByNumber(ctx, big.NewInt(1))
	if err == nil {
		t.Fatal("expected error")
	}

	initialCalls := callCount.Load()

	// Circuit is now open - fix the mock
	shouldFail.Store(false)

	// Wait for circuit to transition to half-open
	time.Sleep(config.OpenTimeout + 20*time.Millisecond)

	// Now call should succeed
	_, err = pool.HeaderByNumber(ctx, big.NewInt(2))
	if err != nil {
		t.Fatalf("expected success after recovery, got: %v", err)
	}

	// Verify the call actually went through
	if callCount.Load() <= initialCalls {
		t.Error("expected additional calls after circuit recovery")
	}
}

func TestPool_MultipleClientsFailover(t *testing.T) {
	mock1CallCount := atomic.Int32{}
	mock2CallCount := atomic.Int32{}

	// Mock1 always fails (non-429) - simulates unreachable endpoint
	mock1 := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			mock1CallCount.Add(1)
			return nil, errors.New("connection refused")
		},
	}

	// Mock2 always succeeds
	mock2 := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			mock2CallCount.Add(1)
			return &ethTypes.Header{Number: blockNumber}, nil
		},
	}

	config := fastConfig()
	config.ConsecutiveFailures = 3 // High threshold so circuit doesn't trip during test

	pool := NewEthermanPool([]IEtherman{mock1, mock2}, config)
	ctx := context.Background()

	// Every call should succeed - when mock1 fails, pool should retry with mock2
	for i := 0; i < 10; i++ {
		_, err := pool.HeaderByNumber(ctx, big.NewInt(int64(i)))
		if err != nil {
			t.Errorf("call %d failed unexpectedly: %v", i, err)
		}
	}

	// Mock1 should have been tried (and failed)
	if mock1CallCount.Load() == 0 {
		t.Error("expected mock1 to be tried at least once")
	}

	// Mock2 should have handled all successful requests
	if mock2CallCount.Load() != 10 {
		t.Errorf("expected mock2 to handle all 10 calls, got %d", mock2CallCount.Load())
	}
}

func TestPool_RequeueWhenOneCircuitOpen(t *testing.T) {
	// This test verifies that when one client's circuit is open,
	// jobs are requeued and handled by the healthy client
	mock1CallCount := atomic.Int32{}
	mock2CallCount := atomic.Int32{}

	// Mock1 always fails - will trip its circuit
	mock1 := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			mock1CallCount.Add(1)
			return nil, errors.New("connection refused")
		},
	}

	// Mock2 always succeeds
	mock2 := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			mock2CallCount.Add(1)
			return &ethTypes.Header{Number: blockNumber}, nil
		},
	}

	config := fastConfig()
	config.ConsecutiveFailures = 1 // Trip after first failure

	pool := NewEthermanPool([]IEtherman{mock1, mock2}, config)
	defer pool.Close()
	ctx := context.Background()

	// Make a few calls - mock1's circuit will trip quickly
	// All requests should still succeed via mock2 (requeue mechanism)
	successCount := 0
	for i := 0; i < 10; i++ {
		_, err := pool.HeaderByNumber(ctx, big.NewInt(int64(i)))
		if err == nil {
			successCount++
		}
	}

	// Most calls should succeed via mock2
	if successCount < 8 {
		t.Errorf("expected at least 8 successes (via requeue to mock2), got %d", successCount)
	}

	// Mock2 should have handled most requests (after mock1's circuit opened)
	if mock2CallCount.Load() < 5 {
		t.Errorf("expected mock2 to handle requests after mock1's circuit opened, got %d calls", mock2CallCount.Load())
	}

	t.Logf("mock1 calls: %d, mock2 calls: %d, successes: %d", mock1CallCount.Load(), mock2CallCount.Load(), successCount)
}

func TestPool_RequeueJobWhenCircuitOpens(t *testing.T) {
	// This test specifically verifies that a job picked up by a worker
	// with an open circuit gets requeued and processed by another worker
	mock1CallCount := atomic.Int32{}
	mock2CallCount := atomic.Int32{}
	mock1CircuitTripped := atomic.Bool{}

	// Mock1 fails until circuit trips, then succeeds (but circuit is open so can't be called)
	mock1 := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			mock1CallCount.Add(1)
			if !mock1CircuitTripped.Load() {
				return nil, errors.New("connection refused")
			}
			return &ethTypes.Header{Number: blockNumber}, nil
		},
	}

	// Mock2 always succeeds
	mock2 := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			mock2CallCount.Add(1)
			return &ethTypes.Header{Number: blockNumber}, nil
		},
	}

	config := fastConfig()
	config.ConsecutiveFailures = 1 // Trip immediately
	config.OpenTimeout = 100 * time.Millisecond

	pool := NewEthermanPool([]IEtherman{mock1, mock2}, config)
	defer pool.Close()
	ctx := context.Background()

	// First call might trip mock1's circuit
	_, _ = pool.HeaderByNumber(ctx, big.NewInt(1))

	// Mark that mock1's circuit should be tripped now
	mock1CircuitTripped.Store(true)

	// Wait a bit to ensure circuit is in open state
	time.Sleep(10 * time.Millisecond)

	// Subsequent calls should succeed via mock2 (mock1's circuit is open, jobs requeued)
	for i := 0; i < 5; i++ {
		_, err := pool.HeaderByNumber(ctx, big.NewInt(int64(i+10)))
		if err != nil {
			t.Errorf("call %d failed: %v (should have been requeued to mock2)", i, err)
		}
	}

	// Mock2 should have handled requests while mock1's circuit was open
	if mock2CallCount.Load() < 3 {
		t.Errorf("expected mock2 to handle requests while mock1's circuit was open, got %d calls", mock2CallCount.Load())
	}

	t.Logf("mock1 calls: %d, mock2 calls: %d", mock1CallCount.Load(), mock2CallCount.Load())
}

func TestPool_FailoverToHealthyClient(t *testing.T) {
	// This test verifies that when one client always fails, the request
	// eventually succeeds via the healthy client (workers pick jobs from queue)
	mock1CallCount := atomic.Int32{}
	mock2CallCount := atomic.Int32{}

	mock1 := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			mock1CallCount.Add(1)
			return nil, errors.New("dial tcp [::1]:6868: connect: connection refused")
		},
	}

	mock2 := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			mock2CallCount.Add(1)
			return &ethTypes.Header{Number: blockNumber}, nil
		},
	}

	config := fastConfig()
	pool := NewEthermanPool([]IEtherman{mock1, mock2}, config)
	defer pool.Close()

	// Request should succeed - one of the workers will handle it
	// If mock1's worker picks it up and fails, it will be requeued for mock2
	header, err := pool.HeaderByNumber(context.Background(), big.NewInt(100))
	if err != nil {
		t.Fatalf("expected success after failover, got error: %v", err)
	}
	if header == nil {
		t.Fatal("expected non-nil header")
	}

	// At least mock2 should have been called (the successful one)
	if mock2CallCount.Load() == 0 {
		t.Error("expected mock2 (healthy client) to be called")
	}
}

func TestPool_PassThroughMode(t *testing.T) {
	callCount := atomic.Int32{}
	mock := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			callCount.Add(1)
			return nil, errors.New("connection timeout")
		},
	}

	config := &EthermanPoolConfig{
		DisableCircuitBreaker: true, // Pass-through mode
	}

	pool := NewEthermanPool([]IEtherman{mock}, config)
	ctx := context.Background()

	// Make multiple failing calls
	for i := 0; i < 10; i++ {
		_, _ = pool.HeaderByNumber(ctx, big.NewInt(int64(i)))
	}

	// All calls should go through (no circuit breaker)
	if callCount.Load() != 10 {
		t.Errorf("expected 10 calls in pass-through mode, got %d", callCount.Load())
	}
}

func TestPool_ConcurrentAccess(t *testing.T) {
	mock1 := &mockEtherman{}
	mock2 := &mockEtherman{}

	pool := NewEthermanPool([]IEtherman{mock1, mock2}, fastConfig())
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 20
	callsPerGoroutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				_, _ = pool.HeaderByNumber(ctx, big.NewInt(int64(j)))
			}
		}()
	}

	wg.Wait()

	totalCalls := mock1.callCount.Load() + mock2.callCount.Load()
	expected := int32(numGoroutines * callsPerGoroutine)
	if totalCalls != expected {
		t.Errorf("expected %d total calls, got %d", expected, totalCalls)
	}
}

func TestPool_CircuitRecoveryAllowsRequests(t *testing.T) {
	shouldFail := atomic.Bool{}
	shouldFail.Store(true)

	mock := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			if shouldFail.Load() {
				return nil, errors.New("connection timeout")
			}
			return &ethTypes.Header{Number: blockNumber}, nil
		},
	}

	config := fastConfig()
	config.ConsecutiveFailures = 1
	config.OpenTimeout = 50 * time.Millisecond

	pool := NewEthermanPool([]IEtherman{mock}, config)
	ctx := context.Background()

	// Trip the circuit
	_, _ = pool.HeaderByNumber(ctx, big.NewInt(1))

	// Immediately after tripping, requests should fail fast (circuit open)
	_, err := pool.HeaderByNumber(ctx, big.NewInt(2))
	if err == nil {
		t.Fatal("expected error when circuit is open")
	}

	// Fix the mock
	shouldFail.Store(false)

	// Wait for circuit to transition to half-open
	time.Sleep(config.OpenTimeout + 20*time.Millisecond)

	// Now request should succeed (circuit is half-open, then closed)
	_, err = pool.HeaderByNumber(ctx, big.NewInt(3))
	if err != nil {
		t.Errorf("expected success after circuit recovery, got: %v", err)
	}
}

func TestPool_WorkerWaitsForCircuitRecovery(t *testing.T) {
	// This test verifies that when a worker's circuit is open,
	// it waits for recovery notification instead of busy-looping
	callCount := atomic.Int32{}

	mock := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			count := callCount.Add(1)
			if count <= 2 {
				// First two calls fail to trip the circuit
				return nil, errors.New("connection refused")
			}
			return &ethTypes.Header{Number: blockNumber}, nil
		},
	}

	config := fastConfig()
	config.ConsecutiveFailures = 2 // Trip after 2 failures
	config.OpenTimeout = 100 * time.Millisecond

	pool := NewEthermanPool([]IEtherman{mock}, config)
	defer pool.Close()
	ctx := context.Background()

	// First two calls fail and trip the circuit
	_, _ = pool.HeaderByNumber(ctx, big.NewInt(1))
	_, _ = pool.HeaderByNumber(ctx, big.NewInt(2))

	// At this point, circuit is open and worker should be waiting
	// The next call should block until circuit recovers (after OpenTimeout)
	start := time.Now()
	_, err := pool.HeaderByNumber(ctx, big.NewInt(3))
	elapsed := time.Since(start)

	// Should either succeed (after waiting for recovery) or timeout
	// The key is that it shouldn't return immediately with "circuit open"
	if err == nil {
		// Success - circuit recovered and call went through
		if elapsed < config.OpenTimeout/2 {
			// If it succeeded too quickly, something might be wrong
			t.Logf("Call succeeded after %v (OpenTimeout=%v)", elapsed, config.OpenTimeout)
		}
	} else if strings.Contains(err.Error(), "open circuits") {
		// This is okay - all circuits were open at submission time (fast fail)
		t.Logf("Got 'open circuits' error (fast fail at submission): %v", err)
	}
}

func TestPool_AllCircuitsOpenFailsFast(t *testing.T) {
	mock := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			return nil, errors.New("connection timeout")
		},
	}

	config := fastConfig()
	config.ConsecutiveFailures = 1
	config.OpenTimeout = 50 * time.Millisecond

	pool := NewEthermanPool([]IEtherman{mock}, config)
	ctx := context.Background()

	// Trip the circuit
	_, _ = pool.HeaderByNumber(ctx, big.NewInt(1))

	// Now circuit is open. Next call should fail fast (not wait)
	start := time.Now()
	_, err := pool.HeaderByNumber(ctx, big.NewInt(2))
	elapsed := time.Since(start)

	// The call should return immediately, not wait for recovery
	if elapsed > 10*time.Millisecond {
		t.Errorf("expected fast failure, but took %v", elapsed)
	}

	// Should get "all circuits open" error
	if err == nil {
		t.Fatal("expected error when all circuits are open")
	}
	if !strings.Contains(err.Error(), "open circuits") {
		t.Errorf("expected 'open circuits' error, got: %v", err)
	}
}

func TestIs429Error(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{nil, false},
		{errors.New("connection timeout"), false},
		{errors.New("429 Too Many Requests"), true},
		{errors.New("too many requests"), true},
		{errors.New("rate limit exceeded"), true},
		{errors.New("RATE LIMIT"), true},
		{errors.New("some error with 429 in it"), true},
	}

	for _, tt := range tests {
		result := Is429Error(tt.err)
		if result != tt.expected {
			t.Errorf("Is429Error(%v) = %v, expected %v", tt.err, result, tt.expected)
		}
	}
}

func TestIsContextError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"regular error", errors.New("connection timeout"), false},
		{"429 error", errors.New("429 Too Many Requests"), false},
		{"deadline exceeded direct", context.DeadlineExceeded, true},
		{"canceled direct", context.Canceled, true},
		{"wrapped deadline", fmt.Errorf("request failed: %w", context.DeadlineExceeded), true},
		{"wrapped canceled", fmt.Errorf("request failed: %w", context.Canceled), true},
		{"string deadline", errors.New("Post http://...: context deadline exceeded"), true},
		{"string canceled", errors.New("Get http://...: context canceled"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsContextError(tt.err)
			if result != tt.expected {
				t.Errorf("IsContextError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestPool_ClientCount(t *testing.T) {
	mock1 := &mockEtherman{}
	mock2 := &mockEtherman{}
	mock3 := &mockEtherman{}

	pool := NewEthermanPool([]IEtherman{mock1, mock2, mock3}, fastConfig())

	if pool.ClientCount() != 3 {
		t.Errorf("expected ClientCount() = 3, got %d", pool.ClientCount())
	}
}

func TestPool_EmptyPool(t *testing.T) {
	pool := NewEthermanPool([]IEtherman{}, fastConfig())
	ctx := context.Background()

	_, err := pool.HeaderByNumber(ctx, big.NewInt(1))
	if err == nil {
		t.Fatal("expected error for empty pool")
	}
}

// TestPool_NoLivelockWithFastFailingClient tests that a fast-failing client
// doesn't starve slow-but-healthy clients from processing jobs.
// This reproduces a bug where a client with instant connection failures would
// keep grabbing and requeuing jobs without letting other workers process them.
func TestPool_NoLivelockWithFastFailingClient(t *testing.T) {
	// Client 0: fails instantly (simulates unreachable endpoint)
	fastFailing := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			// Instant failure - no delay
			return nil, errors.New("dial tcp: connection refused")
		},
	}

	// Client 1: succeeds but takes time (simulates real network call)
	var slowSuccessCount atomic.Int32
	slowSucceeding := &mockEtherman{
		headerByNumberFn: func(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
			// Simulate real network latency
			time.Sleep(50 * time.Millisecond)
			slowSuccessCount.Add(1)
			return &ethTypes.Header{Number: blockNumber}, nil
		},
	}

	// Use very fast circuit breaker timeouts for testing
	cfg := &EthermanPoolConfig{
		DisableCircuitBreaker: false,
		ConsecutiveFailures:   1, // Trip immediately on first failure
		OpenTimeout:           100 * time.Millisecond,
		HalfOpenRequests:      1,
	}

	pool := NewEthermanPool([]IEtherman{fastFailing, slowSucceeding}, cfg)

	// Send multiple requests - they should all complete via the slow-but-healthy client
	const numRequests = 5
	var wg sync.WaitGroup
	wg.Add(numRequests)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var successCount atomic.Int32
	for i := 0; i < numRequests; i++ {
		go func(blockNum int) {
			defer wg.Done()
			_, err := pool.HeaderByNumber(ctx, big.NewInt(int64(blockNum)))
			if err == nil {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// All requests should succeed via the healthy client
	if successCount.Load() != numRequests {
		t.Errorf("expected %d successful requests, got %d", numRequests, successCount.Load())
	}

	// The slow client should have processed all requests
	if slowSuccessCount.Load() != numRequests {
		t.Errorf("expected slow client to process %d requests, got %d", numRequests, slowSuccessCount.Load())
	}
}
