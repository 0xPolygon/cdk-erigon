package ethermanpool

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	ethereum "github.com/erigontech/erigon"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/log/v3"
	ethTypes "github.com/erigontech/erigon/core/types"
	"github.com/sony/gobreaker/v2"
)

// EthermanPoolConfig holds configuration for the etherman pool
type EthermanPoolConfig struct {
	// Circuit breaker settings
	ConsecutiveFailures uint32        // Default: 3 - consecutive failures before tripping
	OpenTimeout         time.Duration // Default: 10s - time circuit stays open before half-open
	HalfOpenRequests    uint32        // Default: 2 - test requests allowed in half-open state

	// Disable circuit breaker (pass-through mode)
	DisableCircuitBreaker bool
}

// DefaultEthermanPoolConfig returns default configuration
func DefaultEthermanPoolConfig() *EthermanPoolConfig {
	return &EthermanPoolConfig{
		ConsecutiveFailures:   3,
		OpenTimeout:           10 * time.Second,
		HalfOpenRequests:      2,
		DisableCircuitBreaker: false,
	}
}

// ethermanWithBreaker wraps an IEtherman with a circuit breaker
type ethermanWithBreaker struct {
	client         IEtherman
	circuitBreaker *gobreaker.CircuitBreaker[any]
	config         *EthermanPoolConfig
	index          int
	// Channel signaled when circuit transitions to HalfOpen or Closed
	recoveryCh chan struct{}
}

// newEthermanWithBreaker creates a new wrapped etherman with circuit breaker
func newEthermanWithBreaker(client IEtherman, index int, config *EthermanPoolConfig) *ethermanWithBreaker {
	ewb := &ethermanWithBreaker{
		client:     client,
		config:     config,
		index:      index,
		recoveryCh: make(chan struct{}, 1), // Buffered so signal doesn't block
	}

	if config.DisableCircuitBreaker {
		// No circuit breaker in pass-through mode
		return ewb
	}

	settings := gobreaker.Settings{
		Name:        fmt.Sprintf("etherman-%d", index),
		MaxRequests: config.HalfOpenRequests,
		Interval:    0, // Don't reset counts based on time interval
		Timeout:     config.OpenTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Trip if consecutive failures >= threshold
			return counts.ConsecutiveFailures >= config.ConsecutiveFailures
		},
		IsSuccessful: func(err error) bool {
			if err == nil {
				return true
			}
			// 429 errors are NOT failures for circuit breaker purposes
			// They trigger backoff but don't trip the circuit
			if Is429Error(err) {
				return true
			}
			// Context timeouts/cancellations are user-defined and NOT failures
			// The endpoint might just be slow due to high load
			if IsContextError(err) {
				return true
			}
			log.Info("Circuit breaker error", "error", err)
			return false
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Info("Circuit breaker state changed", "name", name, "from", from, "to", to)
			// Signal recovery when transitioning to HalfOpen or Closed
			if to == gobreaker.StateHalfOpen || to == gobreaker.StateClosed {
				ewb.signalRecovery()
			}
		},
	}

	ewb.circuitBreaker = gobreaker.NewCircuitBreaker[any](settings)
	return ewb
}

// signalRecovery notifies waiting workers that the circuit has recovered
func (ewb *ethermanWithBreaker) signalRecovery() {
	select {
	case ewb.recoveryCh <- struct{}{}:
	default:
		// Channel already has a signal, don't block
	}
}

// isCircuitOpen returns true if circuit breaker is in Open state
func (ewb *ethermanWithBreaker) isCircuitOpen() bool {
	if ewb.circuitBreaker == nil {
		return false // Pass-through mode - never "open"
	}
	return ewb.circuitBreaker.State() == gobreaker.StateOpen
}

// execute runs the given function through the circuit breaker
func (ewb *ethermanWithBreaker) execute(fn func() (any, error)) (any, error) {
	if ewb.circuitBreaker == nil {
		// Pass-through mode
		return fn()
	}
	return ewb.circuitBreaker.Execute(fn)
}

// Is429Error checks if the error is a rate limit (429) error
func Is429Error(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "429") ||
		strings.Contains(lower, "too many") ||
		strings.Contains(lower, "rate limit")
}

// IsContextError checks if the error is a context timeout or cancellation
// These are user-defined timeouts and should not trip the circuit
func IsContextError(err error) bool {
	if err == nil {
		return false
	}
	// Check for wrapped context errors using errors.Is for proper unwrapping
	if err == context.DeadlineExceeded || err == context.Canceled {
		return true
	}
	// Also check error string for wrapped/nested errors that may not unwrap properly
	errStr := err.Error()
	return strings.Contains(errStr, "context deadline exceeded") ||
		strings.Contains(errStr, "context canceled")
}

// poolJob represents a unit of work to be executed by a client worker
type poolJob struct {
	fn       func(IEtherman) (any, error)
	ctx      context.Context
	resultCh chan poolJobResult
	triedBy  map[int]bool // Tracks which client indices have tried this job
	lastErr  error        // Last error encountered
	mu       sync.Mutex   // Protects triedBy and lastErr
}

// poolJobResult holds the result of a job execution
type poolJobResult struct {
	result any
	err    error
}

// markTried marks a client as having tried this job and returns whether all clients have tried
func (j *poolJob) markTried(clientIndex int, err error, totalClients int) (allTried bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.triedBy[clientIndex] = true
	if err != nil {
		j.lastErr = err
	}
	return len(j.triedBy) >= totalClients
}

// wasTried returns whether a specific client has already tried this job
func (j *poolJob) wasTried(clientIndex int) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.triedBy[clientIndex]
}

// allClientsTried returns whether all clients have tried this job
func (j *poolJob) allClientsTried(totalClients int) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.triedBy) >= totalClients
}

// getLastErr returns the last error encountered
func (j *poolJob) getLastErr() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastErr
}

// EthermanPool manages multiple IEtherman clients with circuit breakers
// Implements IEtherman interface for drop-in replacement
// Uses a channel-based pull model where client workers pick up jobs
type EthermanPool struct {
	clients  []*ethermanWithBreaker
	config   *EthermanPoolConfig
	jobQueue chan *poolJob
	stopCh   chan struct{}
	wg       sync.WaitGroup

	// Dynamic worker scaling
	workerCounts []int // Number of active workers per client
	workersMu    sync.Mutex
	minWorkers   int // Minimum workers per client (default: 1)
	maxWorkers   int // Maximum workers per client (default: 16)
}

// NewEthermanPool creates a new pool from a slice of IEtherman clients
// It starts one worker goroutine per client that pulls jobs from a shared queue
func NewEthermanPool(clients []IEtherman, config *EthermanPoolConfig) *EthermanPool {
	if config == nil {
		config = DefaultEthermanPoolConfig()
	}

	wrappedClients := make([]*ethermanWithBreaker, len(clients))
	for i, client := range clients {
		wrappedClients[i] = newEthermanWithBreaker(client, i, config)
	}

	pool := &EthermanPool{
		clients:      wrappedClients,
		config:       config,
		jobQueue:     make(chan *poolJob, 1000), // Buffered queue for jobs
		stopCh:       make(chan struct{}),
		workerCounts: make([]int, len(wrappedClients)),
		minWorkers:   1,  // Start with 1 worker per client
		maxWorkers:   16, // Scale up to 16 workers per client max
	}

	// Start minimum workers per client
	for i, client := range pool.clients {
		pool.workerCounts[i] = pool.minWorkers
		for j := 0; j < pool.minWorkers; j++ {
			pool.wg.Add(1)
			go pool.clientWorker(client)
		}
	}

	// Start dynamic scaling monitor
	pool.wg.Add(1)
	go pool.scaleWorkers()

	return pool
}

// Close shuts down the pool and waits for all workers to finish
func (p *EthermanPool) Close() {
	close(p.stopCh)
	p.wg.Wait()
}

// scaleWorkers monitors queue depth and dynamically scales workers up/down
func (p *EthermanPool) scaleWorkers() {
	defer p.wg.Done()

	ticker := time.NewTicker(2 * time.Second) // Check every 2 seconds
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			queueLen := len(p.jobQueue)
			queueCap := cap(p.jobQueue)

			p.workersMu.Lock()
			for i, client := range p.clients {
				currentWorkers := p.workerCounts[i]

				// Scale up: if queue is backing up (> 20% full) and we're below max
				if queueLen > queueCap/5 && currentWorkers < p.maxWorkers {
					// Add one worker
					p.workerCounts[i]++
					p.wg.Add(1)
					go p.clientWorker(client)
					log.Debug("Pool: scaled up worker", "client", i, "workers", p.workerCounts[i], "queueLen", queueLen)
				}
				// Scale down is handled automatically by workers exiting after idle timeout
			}
			p.workersMu.Unlock()
		}
	}
}

// ClientCount returns the number of etherman clients in the pool
func (p *EthermanPool) ClientCount() int {
	return len(p.clients)
}

// hasHealthyClient returns true if at least one client has a non-open circuit
func (p *EthermanPool) hasHealthyClient() bool {
	for _, client := range p.clients {
		if !client.isCircuitOpen() {
			return true
		}
	}
	return false
}

// clientWorker is a goroutine that pulls jobs from the queue and executes them
// Each worker is associated with a specific client
// Workers beyond minWorkers will exit after idle timeout to allow scale-down
func (p *EthermanPool) clientWorker(client *ethermanWithBreaker) {
	defer p.wg.Done()

	idleTimeout := 30 * time.Second // Exit after 30s of no jobs (for scale-down)
	idleTimer := time.NewTimer(idleTimeout)
	defer idleTimer.Stop()

	for {
		// If circuit is open, wait for recovery before picking up jobs
		// This prevents busy-looping (picking up jobs just to requeue them)
		if client.isCircuitOpen() {
			select {
			case <-p.stopCh:
				return
			case <-client.recoveryCh:
				// Circuit recovered, continue to pick up jobs
				idleTimer.Reset(idleTimeout)
			}
		}

		select {
		case <-p.stopCh:
			return
		case <-idleTimer.C:
			// Idle timeout - check if we can scale down
			p.workersMu.Lock()
			clientIdx := client.index
			if p.workerCounts[clientIdx] > p.minWorkers {
				// This is an extra worker, safe to exit
				p.workerCounts[clientIdx]--
				p.workersMu.Unlock()
				log.Debug("Pool: worker exited due to idle timeout", "client", clientIdx, "remainingWorkers", p.workerCounts[clientIdx])
				return
			}
			p.workersMu.Unlock()
			// We're at min workers, stay alive - reset timer
			idleTimer.Reset(idleTimeout)
		case j := <-p.jobQueue:
			// Got a job - reset idle timer
			if !idleTimer.Stop() {
				<-idleTimer.C
			}
			idleTimer.Reset(idleTimeout)
			// Check if we already tried this job BEFORE processing
			if j.wasTried(client.index) {
				// If all clients have tried, return error instead of requeuing
				// (prevents infinite loop with single client)
				if j.allClientsTried(len(p.clients)) {
					log.Info("Pool: all clients have tried this job, returning error", "client", client.index)
					lastErr := j.getLastErr()
					if lastErr != nil {
						j.resultCh <- poolJobResult{nil, fmt.Errorf("all %d etherman clients failed: %w", len(p.clients), lastErr)}
					} else {
						j.resultCh <- poolJobResult{nil, fmt.Errorf("all %d etherman clients have tried this job", len(p.clients))}
					}
					continue
				}
				// Requeue for another worker (we've already tried)
				p.requeueJob(j)
				continue // Skip to next iteration, let other workers handle it
			}
			p.processJob(client, j)
		}
	}
}

// processJob handles a single job for a specific client
// NOTE: This function assumes the client has NOT already tried this job
// (that check is done in clientWorker before calling this function)
func (p *EthermanPool) processJob(client *ethermanWithBreaker, j *poolJob) {
	// Check if context is already cancelled
	select {
	case <-j.ctx.Done():
		// Only send result if no one else has
		select {
		case j.resultCh <- poolJobResult{nil, j.ctx.Err()}:
		default:
		}
		return
	default:
	}

	// Check if circuit is open (this can happen if circuit opened between check and execution)
	// Note: We still execute through the circuit breaker - it will fail fast with ErrOpenState
	// This allows the circuit breaker to handle the state transition properly
	// The worker will wait on recoveryCh at the top of the loop if circuit is open

	// Execute the job
	result, err := client.execute(func() (any, error) {
		return j.fn(client.client)
	})

	if err == nil {
		// Success!
		j.resultCh <- poolJobResult{result, nil}
		return
	}

	// Failed - mark this client as tried
	log.Debug("Etherman client failed", "client", client.index, "err", err)
	allTried := j.markTried(client.index, err, len(p.clients))

	if allTried {
		// All clients have tried - return the last error
		j.resultCh <- poolJobResult{nil, fmt.Errorf("all %d etherman clients failed: %w", len(p.clients), err)}
		return
	}

	// Re-queue for another worker to try
	p.requeueJob(j)
}

// requeueJob puts a job back on the queue for another worker to pick up
func (p *EthermanPool) requeueJob(j *poolJob) {
	select {
	case <-j.ctx.Done():
		// Context cancelled, don't requeue
		select {
		case j.resultCh <- poolJobResult{nil, j.ctx.Err()}:
		default:
		}
	case p.jobQueue <- j:
		// Successfully requeued
	default:
		// Queue is full - this shouldn't happen with proper sizing
		// Send error back
		j.resultCh <- poolJobResult{nil, fmt.Errorf("job queue full, cannot retry")}
	}
}

// executeOnClient submits a job to the queue and waits for a worker to execute it.
// The external interface is synchronous, but internally jobs are pulled by workers.
// Fast path: For single client with closed circuit, execute directly to avoid queue overhead.
func (p *EthermanPool) executeOnClient(ctx context.Context, fn func(IEtherman) (any, error)) (any, error) {
	if len(p.clients) == 0 {
		return nil, fmt.Errorf("no etherman clients available")
	}

	// Fast path: Single client - execute directly to avoid queue overhead
	// This eliminates channel/queue overhead for the common single-client case
	// If circuit is open, circuit breaker will fail fast with ErrOpenState
	if len(p.clients) == 1 {
		result, err := p.clients[0].execute(func() (any, error) {
			return fn(p.clients[0].client)
		})
		// Wrap circuit breaker errors to match expected error format
		if err != nil && IsCircuitBreakerError(err) {
			return result, fmt.Errorf("all %d etherman clients have open circuits", len(p.clients))
		}
		return result, err
	}

	// Fail fast if all circuits are open (multi-client case)
	if !p.hasHealthyClient() {
		// Multi-client: fail fast (all clients have open circuits)
		log.Info("Pool: all circuits open, failing fast", "clientCount", len(p.clients))
		return nil, fmt.Errorf("all %d etherman clients have open circuits", len(p.clients))
	}

	// Multi-client or circuit open: use queue-based worker model
	// Create job with result channel
	j := &poolJob{
		fn:       fn,
		ctx:      ctx,
		resultCh: make(chan poolJobResult, 1),
		triedBy:  make(map[int]bool),
	}

	// Submit job to queue
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.jobQueue <- j:
		// Job submitted
	}

	// Wait for result
	select {
	case <-ctx.Done():
		// Context cancelled - try to drain the result channel to prevent goroutine leak
		// The job might still complete, but we can't wait for it
		select {
		case <-j.resultCh:
			// Result was ready, but we're cancelling anyway
		default:
			// No result yet
		}
		return nil, ctx.Err()
	case res := <-j.resultCh:
		return res.result, res.err
	}
}

// HeaderByNumber implements IEtherman
func (p *EthermanPool) HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error) {
	result, err := p.executeOnClient(ctx, func(client IEtherman) (any, error) {
		return client.HeaderByNumber(ctx, blockNumber)
	})
	if err != nil {
		return nil, err
	}
	return result.(*ethTypes.Header), nil
}

// BlockByNumber implements IEtherman
func (p *EthermanPool) BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethTypes.Block, error) {
	result, err := p.executeOnClient(ctx, func(client IEtherman) (any, error) {
		return client.BlockByNumber(ctx, blockNumber)
	})
	if err != nil {
		return nil, err
	}
	return result.(*ethTypes.Block), nil
}

// FilterLogs implements IEtherman
func (p *EthermanPool) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]ethTypes.Log, error) {
	result, err := p.executeOnClient(ctx, func(client IEtherman) (any, error) {
		return client.FilterLogs(ctx, query)
	})
	if err != nil {
		return nil, err
	}
	return result.([]ethTypes.Log), nil
}

// CallContract implements IEtherman
func (p *EthermanPool) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	result, err := p.executeOnClient(ctx, func(client IEtherman) (any, error) {
		return client.CallContract(ctx, msg, blockNumber)
	})
	if err != nil {
		return nil, err
	}
	return result.([]byte), nil
}

// TransactionByHash implements IEtherman
func (p *EthermanPool) TransactionByHash(ctx context.Context, hash common.Hash) (ethTypes.Transaction, bool, error) {
	result, err := p.executeOnClient(ctx, func(client IEtherman) (any, error) {
		tx, found, err := client.TransactionByHash(ctx, hash)
		if err != nil {
			return nil, err
		}
		return []interface{}{tx, found}, nil
	})
	if err != nil {
		return nil, false, err
	}
	res := result.([]interface{})
	return res[0].(ethTypes.Transaction), res[1].(bool), nil
}

// TransactionReceipt implements IEtherman
func (p *EthermanPool) TransactionReceipt(ctx context.Context, txHash common.Hash) (*ethTypes.Receipt, error) {
	result, err := p.executeOnClient(ctx, func(client IEtherman) (any, error) {
		return client.TransactionReceipt(ctx, txHash)
	})
	if err != nil {
		return nil, err
	}
	return result.(*ethTypes.Receipt), nil
}

// StorageAt implements IEtherman
func (p *EthermanPool) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	result, err := p.executeOnClient(ctx, func(client IEtherman) (any, error) {
		return client.StorageAt(ctx, account, key, blockNumber)
	})
	if err != nil {
		return nil, err
	}
	return result.([]byte), nil
}

// SuggestedGasPrice implements IEtherman
func (p *EthermanPool) SuggestedGasPrice(ctx context.Context) (*big.Int, error) {
	result, err := p.executeOnClient(ctx, func(client IEtherman) (any, error) {
		return client.SuggestedGasPrice(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.(*big.Int), nil
}

// HeadersByNumbers implements HeaderBatcher interface for batch header retrieval
func (p *EthermanPool) HeadersByNumbers(ctx context.Context, numbers []*big.Int) ([]*ethTypes.Header, error) {
	result, err := p.executeOnClient(ctx, func(client IEtherman) (any, error) {
		// Check if underlying client supports batch operations
		if batcher, ok := client.(HeaderBatcher); ok {
			return batcher.HeadersByNumbers(ctx, numbers)
		}
		// Fallback: fetch headers one by one
		headers := make([]*ethTypes.Header, len(numbers))
		for i, num := range numbers {
			header, err := client.HeaderByNumber(ctx, num)
			if err != nil {
				return nil, err
			}
			headers[i] = header
		}
		return headers, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]*ethTypes.Header), nil
}

// IsCircuitBreakerError checks if the error is due to circuit breaker being open
func IsCircuitBreakerError(err error) bool {
	if err == nil {
		return false
	}
	return err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests
}
