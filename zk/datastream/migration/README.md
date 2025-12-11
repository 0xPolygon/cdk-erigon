# TCP to NATS Datastream Migration Tool

Tool for migrating existing TCP datastream files to NATS JetStream.

## Overview

This migration tool reads all entries from a TCP datastream file and republishes them to NATS JetStream, maintaining all metadata and bookmark mappings. Used for transitioning from TCP-based datastreaming to NATS-based datastreaming.

## Use Case

**Scenario**: Sequencer running with TCP datastream needs to switch to NATS.

**Workflow**:
1. Stop sequencer
2. Run migration tool to backfill NATS from TCP files
3. Reconfigure sequencer to use NATS
4. Restart sequencer (continues from where TCP left off)

## Installation

```bash
cd zk/datastream/migration
go build -o datastream-migrator main.go
```

## Usage

### Basic Migration

```bash
./datastream-migrator \
  --tcp-file /path/to/datastream.bin \
  --nats-host localhost \
  --nats-port 4222
```

### All Options

```bash
./datastream-migrator \
  --tcp-file /path/to/datastream.bin \     # Required: Path to TCP datastream file
  --nats-host localhost \                  # NATS server host (default: 127.0.0.1)
  --nats-port 4222 \                       # NATS server port (default: 4222)
  --nats-dir data/nats-storage \           # NATS storage directory (default: data/nats-storage)
  --batch-size 100 \                       # Batch size for publishing (default: 100)
  --start-from 0 \                         # Resume from entry number (default: 0)
  --dry-run \                              # Test without publishing (default: false)
  --verbose                                # Enable debug logging (default: false)
```

## Migration Process

### Step 1: Locate TCP Datastream File

TCP datastream file is typically located at:
```
data/datastream/datastream.bin
```

Or as configured by `--datastream.file` flag on sequencer.

### Step 2: Stop Sequencer

```bash
# Stop the sequencer process
pkill -SIGTERM erigon
```

**CRITICAL**: Sequencer must be stopped before migration.

### Step 3: Run Migration (Dry Run First)

```bash
# Test migration without writing to NATS
./datastream-migrator \
  --tcp-file data/datastream/datastream.bin \
  --dry-run \
  --verbose
```

Review output for any errors or warnings.

### Step 4: Run Actual Migration

```bash
# Perform actual migration
./datastream-migrator \
  --tcp-file data/datastream/datastream.bin \
  --nats-host 0.0.0.0 \
  --nats-port 4222 \
  --nats-dir data/nats-storage \
  --batch-size 100 \
  --verbose
```

Monitor progress. Migration shows:
- Total entries to migrate
- Current progress (entries/sec)
- Bookmarks migrated
- Any errors encountered

### Step 5: Verify Migration

```bash
# Check NATS stream was created
nats stream info DATASTREAM

# Verify entry count matches
nats stream state DATASTREAM
```

Compare `totalEntries` from migration output with NATS stream message count.

### Step 6: Reconfigure Sequencer

Update sequencer config to use NATS:

```yaml
# OLD (TCP)
zkevm:
  data-stream-port: 6900
  data-stream-host: localhost

# NEW (NATS)
zkevm:
  data-stream-nats: true
  data-stream-nats-host: 0.0.0.0
  data-stream-nats-port: 4222
```

### Step 7: Restart Sequencer

```bash
# Start sequencer with NATS config
./erigon --config nats-config.yaml
```

Sequencer will now publish to NATS. RPC nodes can connect via NATS.

## Resumability

If migration fails partway through:

```bash
# Resume from last successful entry
./datastream-migrator \
  --tcp-file data/datastream/datastream.bin \
  --start-from 12345 \
  --verbose
```

Entry number is shown in progress logs. Use last successful entry number.

## Performance Tuning

### Batch Size

Larger batches = faster migration, more memory:

```bash
# Fast migration (high memory)
--batch-size 1000

# Slower migration (low memory)
--batch-size 10
```

Recommended: 100-500

### NATS Storage

Ensure sufficient disk space:

```bash
# Check required space (rough estimate)
du -sh data/datastream/datastream.bin

# NATS needs ~1.5x that amount
```

Set storage limits:

```bash
--nats-dir /path/to/large/disk
```

## Troubleshooting

### Migration Fails to Start

**Error**: `failed to open TCP datastream`

**Solution**: Verify file path is correct and file exists:
```bash
ls -lh /path/to/datastream.bin
```

### NATS Server Won't Start

**Error**: `failed to start NATS server`

**Solution**: Check port not already in use:
```bash
lsof -i :4222
```

### Out of Memory

**Error**: Process killed

**Solution**: Reduce batch size:
```bash
--batch-size 10
```

### Entry Count Mismatch

**Problem**: NATS stream has fewer messages than TCP file

**Solution**: Check for errors in migration logs. Run verification:
```bash
# Compare counts
echo "TCP entries: $(grep 'totalEntries' migration.log)"
echo "NATS messages: $(nats stream state DATASTREAM | grep Messages)"
```

### Bookmarks Missing

**Problem**: Clients can't resume from bookmarks

**Solution**: Verify KV store contains bookmarks:
```bash
nats kv ls BOOKMARKS
nats kv get BOOKMARKS METADATA_TOTAL_ENTRIES
```

## Verification Steps

After migration, verify integrity:

### 1. Entry Count

```bash
# Get TCP total
TCP_COUNT=$(./datastream-migrator --tcp-file data/datastream/datastream.bin --dry-run 2>&1 | grep totalEntries | awk '{print $NF}')

# Get NATS total
NATS_COUNT=$(nats stream state DATASTREAM | grep "Messages:" | awk '{print $2}')

echo "TCP: $TCP_COUNT, NATS: $NATS_COUNT"
```

### 2. Latest Block

```bash
# Check latest block bookmark exists
nats kv get BOOKMARKS METADATA_LATEST_BLOCK_BOOKMARK
```

### 3. Test Client Connection

```bash
# Start test client
./erigon --config nats-config.yaml --l2-nats-url nats://localhost:4222
```

Check logs for successful connection and block synchronization.

## Architecture

### Data Flow

```
TCP Datastream File
  ↓ Read via datastreamer
  ↓ Parse entries
  ↓ Create NATS messages
  ↓ Publish to JetStream
  ↓ Store bookmarks in KV
  ↓ Update metadata
  ↓
NATS Stream Ready
```

### Entry Types Migrated

- L2 Blocks (EntryType: 5)
- L2 Transactions (EntryType: 6)
- L2 Block End (EntryType: 7)
- Batch Start (EntryType: 1)
- Batch End (EntryType: 2)
- GER Updates (EntryType: 8)
- Bookmarks (EntryType: 176)

### Metadata Migrated

- Total entries count
- Block bookmarks (block number → NATS sequence)
- Batch bookmarks (batch number → NATS sequence)
- Latest block bookmark

## Safety

### What Migration Does NOT Do

- ❌ Modify original TCP files
- ❌ Delete TCP files
- ❌ Require sequencer to be running
- ❌ Interrupt live sequencer operation

### Rollback

If migration fails or NATS has issues:

1. Stop sequencer
2. Revert config to TCP
3. Delete NATS storage: `rm -rf data/nats-storage`
4. Restart sequencer with TCP config

Original TCP files remain untouched.

## Performance

Typical migration rates:

| Entries | Batch Size | Time | Rate |
|---------|------------|------|------|
| 100K | 100 | 5 min | ~330/sec |
| 1M | 100 | 50 min | ~330/sec |
| 10M | 500 | 6 hours | ~460/sec |

Actual rates depend on:
- Disk I/O speed
- Entry complexity (transactions per block)
- Available memory
- NATS configuration

## FAQ

**Q: Can I run migration while sequencer is running?**
A: No. Sequencer must be stopped to ensure data consistency.

**Q: Will this delete my TCP files?**
A: No. Migration only reads TCP files, never modifies them.

**Q: Can I resume a failed migration?**
A: Yes. Use `--start-from` with the last successful entry number.

**Q: How long does migration take?**
A: Depends on datastream size. ~5-10 hours for 10M entries.

**Q: Do I need to migrate for RPC nodes?**
A: No. RPC nodes can connect directly to NATS after sequencer migration.

**Q: What if NATS runs out of space?**
A: Migration fails. Increase storage or clear old data first.

## Support

For issues:
1. Check troubleshooting section above
2. Review migration logs with `--verbose`
3. Verify NATS server is healthy: `nats server check`
4. Check NATS JetStream status: `nats account info`

## See Also

- [NATS Guide](../../../docs/zkevm/NATS_GUIDE.md) - NATS configuration and CLI commands
- [NATS Implementation](../natsstream/README.md) - NATS client/server details
- [Datastream Protocol](../../proto/) - Message format specification