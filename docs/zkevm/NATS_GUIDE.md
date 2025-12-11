# NATS Datastream Guide

Complete guide for using NATS with cdk-erigon datastream.

## Table of Contents

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Configuration](#configuration)
4. [CLI Commands](#cli-commands)
5. [Monitoring](#monitoring)
6. [Troubleshooting](#troubleshooting)

---

## Overview

NATS replaces TCP-based datastreaming with JetStream for improved:
- **Reliability**: Automatic reconnection, message persistence
- **Scalability**: Multiple consumers, distributed architecture
- **Operations**: Built-in monitoring, clustering support

### Key Concepts

- **Publisher**: Main sequencer publishes blocks/transactions to NATS
- **Consumer**: RPC nodes/shadow sequencers consume from NATS
- **Stream**: Named JetStream storage (`DATASTREAM`)
- **Subject**: Message routing pattern (`datastream.entry`)
- **Bookmark**: Position tracking for stream resumption

---

## Architecture

```
┌─────────────────┐
│ Main Sequencer  │
│  (Publisher)    │
└────────┬────────┘
         │ Publishes
         ↓
┌─────────────────┐
│  NATS Server    │
│  + JetStream    │
└────────┬────────┘
         │ Subscribes
         ↓
┌─────────────────┐
│  RPC Nodes /    │
│  Consumers      │
└─────────────────┘
```

### Components

| Component | Location | Description |
|-----------|----------|-------------|
| Manager | `zk/datastream/natsstream/manager.go` | Embedded NATS server lifecycle |
| Client | `zk/datastream/natsstream/nats_client.go` | Consumer implementation |
| Server | `zk/datastream/natsstream/stream_server.go` | Publisher implementation |

---

## Configuration

### Publisher Configuration (Main Sequencer)

Enable NATS publishing on the main sequencer:

```bash
# Enable NATS
--zkevm.data-stream-nats=true

# NATS server bind address
--zkevm.data-stream-nats-host=0.0.0.0

# NATS server port
--zkevm.data-stream-nats-port=4222
```

**YAML Config:**
```yaml
zkevm:
  data-stream-nats: true
  data-stream-nats-host: "0.0.0.0"
  data-stream-nats-port: 4222
```

### Consumer Configuration (RPC Nodes)

Connect to NATS publisher:

```bash
# NATS server URL (connects to publisher)
--zkevm.l2-nats-url=nats://sequencer-host:4222

# Optional: TCP fallback
--zkevm.l2-datastreamer-url=sequencer-host:6900
```

**YAML Config:**
```yaml
zkevm:
  l2-nats-url: "nats://sequencer-host:4222"
  l2-datastreamer-url: "sequencer-host:6900"  # Fallback
```

### Manager Configuration

Embedded NATS server configuration (Go):

```go
config := natsstream.Config{
    Host:             "0.0.0.0",      // Bind address
    Port:             4222,            // NATS port
    ServerName:       "erigon-nats",   // Server identifier
    ClusterName:      "erigon-cluster", // Cluster name
    HTTPHost:         "127.0.0.1",     // Monitoring interface
    HTTPPort:         8222,            // Monitoring port (0=disabled)
    JetStreamEnabled: true,            // Enable persistence
    StorageDir:       "data/nats-storage", // Data directory
    MaxMemory:        1073741824,      // 1GB
    MaxStorage:       10737418240,     // 10GB
    Debug:            false,           // Debug logging
    Trace:            false,           // Trace logging
}
```

### Configuration Flags Reference

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `zkevm.data-stream-nats` | bool | false | Enable NATS publishing |
| `zkevm.data-stream-nats-host` | string | "" | NATS server bind address |
| `zkevm.data-stream-nats-port` | string | "" | NATS server port |
| `zkevm.l2-nats-url` | string | "" | NATS connection URL (consumer) |
| `zkevm.l2-datastreamer-url` | string | "" | TCP fallback URL |
| `zkevm.l2-datastreamer-max-entrychan` | uint64 | 100000 | Entry channel buffer size |

---

## CLI Commands

### Prerequisites

Install NATS CLI:
```bash
# macOS
brew install nats-io/nats-tools/nats

# Linux
curl -sf https://binaries.nats.dev/nats-io/natscli/nats@latest | sh

# Verify installation
nats --version
```

### Common Commands

#### Connect to NATS Server

```bash
# Set server URL (use for all subsequent commands)
export NATS_URL=nats://localhost:4222

# Or use -s flag with each command
nats -s nats://localhost:4222 <command>
```

#### Stream Management

```bash
# List all streams
nats stream ls

# View stream info
nats stream info DATASTREAM

# View stream state (messages, bytes, consumers)
nats stream state DATASTREAM

# View messages in stream
nats stream view DATASTREAM

# Get specific message by sequence
nats stream get DATASTREAM 42

# View stream subjects
nats stream subjects DATASTREAM

# Purge stream (delete all messages)
nats stream purge DATASTREAM

# Delete stream
nats stream rm DATASTREAM
```

#### Consumer Management

```bash
# List consumers for a stream
nats consumer ls DATASTREAM

# View consumer info
nats consumer info DATASTREAM <consumer-name>

# View consumer state
nats consumer state DATASTREAM <consumer-name>

# Delete consumer
nats consumer rm DATASTREAM <consumer-name>
```

#### Monitoring

```bash
# Server info
nats server info

# List connections
nats server list

# Real-time connection statistics
nats top

# Stream report (all streams)
nats stream report

# Consumer report (all consumers)
nats consumer report
```

#### Publishing (Testing)

```bash
# Publish test message
nats pub datastream.entry "test message"

# Subscribe to subject
nats sub "datastream.>"

# Subscribe and show raw data
nats sub "datastream.>" --raw
```

#### JetStream Info

```bash
# JetStream account info
nats account info

# Server events
nats events

# Error code documentation
nats errors
```

### Useful Command Patterns

#### Monitor Stream Growth

```bash
# Watch stream state updates
watch -n 1 'nats stream state DATASTREAM'
```

#### Find Messages by Block Number

The stream stores raw protobuf messages. Use monitoring tools or client APIs to query by block number.

#### Check Consumer Lag

```bash
# Consumer info shows pending messages
nats consumer info DATASTREAM <consumer-name> | grep -E "(Pending|Ack)"
```

#### Backup Stream

```bash
# Backup stream to file
nats stream backup DATASTREAM backup.tar.gz

# Restore stream from backup
nats stream restore DATASTREAM backup.tar.gz
```

---

## Monitoring

### HTTP Monitoring Interface

NATS server exposes monitoring endpoint:

```bash
# Default: http://localhost:8222

# View server endpoints
curl http://localhost:8222/

# Connection stats
curl http://localhost:8222/connz

# Routing stats
curl http://localhost:8222/routez

# Subscription stats
curl http://localhost:8222/subsz

# JetStream stats
curl http://localhost:8222/jsz
```

### Key Metrics

Monitor these metrics for health:

| Metric | Command | Good Value |
|--------|---------|------------|
| Stream messages | `nats stream state DATASTREAM` | Growing steadily |
| Consumer lag | `nats consumer info ...` | Pending < 1000 |
| Server connections | `nats server list` | Stable count |
| Memory usage | `curl .../jsz` | < MaxMemory |
| Storage usage | `curl .../jsz` | < MaxStorage |

### Debug Logging

Enable debug/trace in NATS manager:

```go
config.Debug = true  // Enable debug logs
config.Trace = true  // Enable verbose trace
```

Or via NATS server options:
```bash
# Server logs to console when Debug=true in config
```

---

## Troubleshooting

### Connection Issues

**Problem**: Cannot connect to NATS server

```bash
# Check server is running
nats server ping -s nats://localhost:4222

# Check server info
nats server info -s nats://localhost:4222

# Check firewall/network
telnet localhost 4222
```

**Solution**:
- Verify server is started
- Check bind address (0.0.0.0 vs 127.0.0.1)
- Check firewall rules for port 4222
- Verify URL format: `nats://host:port`

### Stream Not Found

**Problem**: `stream not found` error

```bash
# List all streams
nats stream ls -a

# Check if JetStream is enabled
nats account info
```

**Solution**:
- Ensure JetStreamEnabled=true in manager config
- Verify stream was created (InitStreams called)
- Check stream name matches (case-sensitive)

### Consumer Lag

**Problem**: Consumer falling behind

```bash
# Check consumer state
nats consumer info DATASTREAM <consumer-name>

# Check pending messages
nats stream state DATASTREAM
```

**Solution**:
- Increase consumer processing speed
- Check for errors in consumer logs
- Monitor CPU/memory on consumer node
- Consider parallel consumers

### Storage Full

**Problem**: `insufficient resources` error

```bash
# Check JetStream storage
curl http://localhost:8222/jsz | jq '.meta.config.max_storage'
```

**Solution**:
- Increase MaxStorage in config
- Purge old messages: `nats stream purge DATASTREAM`
- Implement retention policy
- Monitor storage usage regularly

### Messages Missing

**Problem**: Expected messages not appearing

```bash
# Check stream subjects
nats stream subjects DATASTREAM

# View recent messages
nats stream view DATASTREAM --tail

# Check for gaps
nats stream gaps DATASTREAM
```

**Solution**:
- Verify publisher is connected
- Check subject pattern matches: `datastream.entry`
- Review publisher logs for errors
- Check MaxPayload limits (default 8MB)

### Performance Issues

**Problem**: Slow message processing

**Diagnostics**:
```bash
# Monitor message rate
nats stream info DATASTREAM | grep "Messages per second"

# Check server stats
nats top

# View consumer performance
nats consumer report
```

**Solution**:
- Increase buffer sizes (entryChan)
- Enable file storage (vs memory)
- Optimize consumer processing logic
- Check network latency

---

## Best Practices

### Configuration

- Use `0.0.0.0` for bind address on publisher
- Set reasonable MaxMemory/MaxStorage limits
- Enable HTTP monitoring for production
- Use file storage for persistence

### Operations

- Monitor consumer lag regularly
- Set up alerts for storage usage
- Backup streams before upgrades
- Test failover scenarios

### Development

- Use debug logging during development
- Test with production-like message rates
- Validate subject patterns
- Handle reconnection gracefully

### Security

- Restrict network access to NATS port
- Use authentication in production (future)
- Enable TLS for external access (future)
- Monitor unauthorized connections

---

## Additional Resources

- [NATS Documentation](https://docs.nats.io/)
- [JetStream Guide](https://docs.nats.io/nats-concepts/jetstream)
- [NATS CLI Cheat Sheet](https://natsbyexample.com/)
- [cdk-erigon NATS Tests](../../zk/datastream/natsstream/README_TESTS.md)

---

## Quick Reference Card

```bash
# Server connection
export NATS_URL=nats://localhost:4222

# Essential commands
nats stream ls                           # List streams
nats stream info DATASTREAM              # Stream details
nats consumer ls DATASTREAM              # List consumers
nats server info                         # Server status
nats top                                 # Live statistics

# Monitoring
curl http://localhost:8222/jsz | jq     # JetStream stats
watch -n 1 'nats stream state DATASTREAM' # Live stream state

# Troubleshooting
nats stream gaps DATASTREAM              # Find missing messages
nats events                              # View server events
nats errors                              # Error code lookup
```