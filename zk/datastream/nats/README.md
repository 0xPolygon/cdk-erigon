# NATS Server Manager

This package provides a manager for embedding a NATS server within your application. It handles the complete lifecycle of a NATS server, including configuration, startup, shutdown, and client connections.

## Features

- Embedded NATS server management (start/stop)
- JetStream support for persistent messaging
- Thread-safe operations
- Helper methods for connecting to the managed server
- Comprehensive test coverage

## Usage

### Basic Usage

```go
import (
    "github.com/erigontech/erigon/zk/datastream/nats"
    "go.uber.org/zap"
)

// Create a logger
logger, _ := zap.NewProduction()

// Create a config with default values
config := nats.DefaultConfig()

// Create the manager
manager := nats.NewManager(config, logger)

// Start the server
err := manager.Start()
if err != nil {
    logger.Fatal("Failed to start NATS server", zap.Error(err))
}

// Get the URL for connecting
url, _ := manager.URL()
logger.Info("NATS server running", zap.String("url", url))

// Connect to the server
nc, _ := manager.Connect()
defer nc.Close()

// Use the connection
nc.Publish("example.subject", []byte("Hello, world!"))

// Shutdown when done
manager.Stop()
```

### Configuration Options

The `Config` struct provides several options for configuring the NATS server:

- `Host`: The hostname or IP to bind to (default: "127.0.0.1")
- `Port`: The port to listen on (default: 4222, use -1 for random port)
- `JetStreamEnabled`: Enable JetStream for persistent messaging (default: true)
- `StorageDir`: Directory for JetStream data storage
- `MaxMemory`: Maximum memory for JetStream (default: 1GB)
- `MaxStorage`: Maximum storage for JetStream (default: 10GB)
- `Debug`: Enable debug logging (default: false)

## JetStream Usage

```go
import (
    "context"
    "github.com/erigontech/erigon/zk/datastream/nats"
    "github.com/nats-io/nats.go/jetstream"
    "go.uber.org/zap"
)

// Create and start manager
logger, _ := zap.NewProduction()
config := nats.DefaultConfig()
config.JetStreamEnabled = true
config.StorageDir = "/path/to/storage"

manager := nats.NewManager(config, logger)
manager.Start()
defer manager.Stop()

// Connect to the server
nc, _ := manager.Connect()
defer nc.Close()

// Create JetStream context
js, _ := jetstream.New(nc)

// Create a stream
ctx := context.Background()
stream, _ := js.CreateStream(ctx, jetstream.StreamConfig{
    Name:     "my_stream",
    Subjects: []string{"my_stream.>"},
})

// Publish a message
js.Publish(ctx, "my_stream.test", []byte("Hello JetStream!"))
```

## Implementation

The `Manager` struct implements the `Lifecycle` interface which defines the standard start/stop methods for service management:

```go
type Lifecycle interface {
    Start() error
    Stop() error
}
```

This allows the NATS Manager to be easily integrated with other service management frameworks. 