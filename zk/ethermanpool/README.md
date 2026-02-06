# Etherman Pool with Circuit Breakers

## Overview

The `ethermanpool` package provides a resilient pool of Ethereum L1 RPC clients with circuit breaker protection. It enables automatic failover between multiple clients, isolates failing endpoints, and provides a shared pool for efficient resource usage across components.

## Why

### Problem
- Single point of failure: If one L1 RPC endpoint fails, the entire syncer stalls
- No automatic failover: Manual client selection doesn't handle failures gracefully
- Resource waste: Each component (syncer, gas tracker) creates its own clients
- No failure isolation: Unhealthy endpoints continue to be used

### Solution
- **Circuit breakers**: Automatically isolate failing clients
- **Automatic failover**: Tries all healthy clients before failing
- **Shared pool**: Single pool instance shared across all components
- **Smart error handling**: Differentiates between rate limits, timeouts, and real failures

## How It Works

### Architecture

```
┌─────────────────┐
│   L1Syncer      │
│  GasTracker     │──┐
└─────────────────┘  │
                     ├──► EthermanPool ──► [Client1, Client2, ...]
┌─────────────────┐  │      │
│  Other Consumers│──┘      │
└─────────────────┘         │
                            ▼
                    ┌─────────────────┐
                    │ Circuit Breaker │
                    │   (per client)  │
                    └─────────────────┘
```

### Components

#### 1. EthermanPool
- Manages multiple `IEtherman` clients
- Implements `IEtherman` interface for drop-in replacement
- Uses channel-based pull model: workers pull jobs from shared queue
- Fast path: Single-client scenarios bypass queue for performance

#### 2. ethermanWithBreaker
- Wraps each client with a `gobreaker` circuit breaker
- Tracks circuit state (Closed → Open → HalfOpen → Closed)
- Signals recovery to waiting workers when circuit recovers

#### 3. Worker Model
- Each client has 1-16 workers (scales dynamically)
- Workers pull jobs from shared queue
- If circuit is open, worker waits for recovery signal
- Jobs are requeued if client fails (until all clients tried)

### Circuit Breaker States

```
Closed (Healthy)
  │
  │ Consecutive failures >= threshold
  ▼
Open (Tripped)
  │
  │ After OpenTimeout
  ▼
HalfOpen (Testing)
  │
  │ Success → Closed
  │ Failure → Open
  ▼
Closed
```

### Error Handling

#### 429 Errors (Rate Limiting)
- **Behavior**: Trigger backoff but **don't trip circuit**
- **Reason**: Rate limits are temporary and don't indicate endpoint failure
- **Action**: Request is retried with backoff

#### Context Errors (Timeouts/Cancellations)
- **Behavior**: **Don't trip circuit**
- **Reason**: User-defined timeouts don't indicate endpoint failure
- **Action**: Error returned to caller

#### Other Errors
- **Behavior**: Count toward circuit breaker failure threshold
- **Reason**: Indicates potential endpoint failure
- **Action**: After threshold, circuit opens and endpoint is isolated

### Fast Path

For single-client scenarios with closed circuit:
- Bypasses job queue entirely
- Executes directly through circuit breaker
- Eliminates channel/queue overhead
- Maintains same error handling and circuit breaker behavior

### Dynamic Worker Scaling

- **Min workers**: 1 per client (always running)
- **Max workers**: 16 per client (scales up under load)
- **Scale up**: When queue depth > threshold
- **Scale down**: Extra workers exit after idle timeout (30s)

## Usage

### Basic Setup

```go
// Create clients
clients := []ethermanpool.IEtherman{
    ethermanClient1,
    ethermanClient2,
}

// Create pool
config := ethermanpool.DefaultEthermanPoolConfig()
pool := ethermanpool.NewEthermanPool(clients, config)

// Use as IEtherman
header, err := pool.HeaderByNumber(ctx, blockNumber)
```

### Configuration

```go
config := &ethermanpool.EthermanPoolConfig{
    ConsecutiveFailures:   3,              // Failures before tripping
    OpenTimeout:           10 * time.Second, // Time before half-open
    HalfOpenRequests:      2,              // Test requests in half-open
    DisableCircuitBreaker: false,          // Pass-through mode
}
```

### Integration in Backend

```go
// Create single pool instance
ethermanPool := ethermanpool.NewEthermanPool(ethermanClients, poolConfig)

// Pass to syncer
syncer := syncer.NewL1Syncer(..., ethermanPool, ...)

// Pass to gas tracker
gasTracker := jsonrpc.NewRecurringL1GasPriceTracker(..., ethermanPool)
```

## Benefits

1. **Resilience**: Automatic failover prevents single point of failure
2. **Performance**: Fast path maintains single-client performance
3. **Efficiency**: Shared pool reduces resource usage
4. **Isolation**: Circuit breakers prevent cascading failures
5. **Observability**: Circuit state changes are logged

## Implementation Details

### Job Queue Model

- Jobs are submitted to a buffered channel
- Workers pull jobs (pull model, not push)
- Natural load balancing: faster/healthier clients process more jobs
- Prevents worker starvation

### Circuit Recovery

- When circuit opens, workers wait on `recoveryCh`
- Circuit breaker's `OnStateChange` callback signals recovery
- Workers wake up and resume processing
- No busy-looping or polling

### Livelock Prevention

- Jobs track which clients have tried them
- If all clients tried, return error (no infinite requeue)
- Workers skip jobs they've already tried
- Prevents single failing client from monopolizing queue

## Testing

Comprehensive test coverage includes:
- Circuit breaker state transitions
- Failover between multiple clients
- Worker recovery on circuit open
- Error handling (429, context errors)
- Fast path behavior
- Livelock prevention
- Dynamic worker scaling

## Migration Notes

- `etherman.Client` now implements `IEtherman` directly (no adapter needed)
- Components receive `IEtherman` interface instead of concrete type
- Single pool instance shared across all consumers
- Backward compatible: can disable circuit breakers via config

