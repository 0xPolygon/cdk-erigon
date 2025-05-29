package syncer

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon-lib/rlp"
	ethTypes "github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/zk/hermez_db"
)

const (
	bucketL1Headers  = "l1_headers"
	bucketL1TreeLogs = "l1_tree_logs"
)

var (
	cacheBuckets = []string{
		bucketL1Headers,
		bucketL1TreeLogs,
	}
)

type L1Cache struct {
	ctx       context.Context
	logPrefix string
	l1CacheDB kv.RwDB
}

func NewL1SyncerCache(ctx context.Context, db kv.RwDB) (*L1Cache, error) {
	l1Cache := &L1Cache{
		ctx:       ctx,
		logPrefix: "L1Cache",
		l1CacheDB: db,
	}

	err := l1Cache.createCacheBuckets()

	if err != nil {
		return nil, err
	}

	return l1Cache, nil
}

func (c *L1Cache) writeL1BlockHeaderCache(header *ethTypes.Header) error {
	tx, err := c.l1CacheDB.BeginRw(c.ctx)

	if err != nil {
		return fmt.Errorf("failed to start transaction: %s", err)
	}

	defer tx.Rollback()

	var buf bytes.Buffer

	if header == nil {
		return fmt.Errorf("header is nil")
	}

	err = rlp.Encode(&buf, header)
	if err != nil {
		return fmt.Errorf("failed to serialize header: %s", err)
	}
	err = tx.Put(bucketL1Headers, hermez_db.Uint64ToBytes(header.Number.Uint64()), buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write header: %s", err)
	}

	return tx.Commit()
}

func (c *L1Cache) getL1BlockHeaderCache(l1BlockNumber uint64) (*ethTypes.Header, error) {
	tx, err := c.l1CacheDB.BeginRo(c.ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %s", err)
	}

	defer tx.Rollback()

	data, err := tx.GetOne(bucketL1Headers, hermez_db.Uint64ToBytes(l1BlockNumber))
	if err != nil {
		return nil, err
	}

	if data == nil {
		return nil, nil
	}

	var header ethTypes.Header
	err = rlp.DecodeBytes(data, &header)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize header: %d %s", l1BlockNumber, err)
	}
	return &header, nil
}

func (c *L1Cache) writeL1TreeLogs(logEntry *ethTypes.Log) error {
	var buf bytes.Buffer

	tx, err := c.l1CacheDB.BeginRw(c.ctx)

	if err != nil {
		return fmt.Errorf("failed to start transaction: %s", err)
	}

	defer tx.Rollback()

	err = rlp.Encode(&buf, logEntry)
	if err != nil {
		return fmt.Errorf("failed to serialize logs: %s", err)
	}
	err = tx.Put(bucketL1TreeLogs, encodeL1LogKey(logEntry.BlockNumber, logEntry.Index), buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write logs: %s", err)
	}

	return tx.Commit()
}

func (c *L1Cache) getLastL1TreeLogBlockNumber() (uint64, error) {
	tx, err := c.l1CacheDB.BeginRo(c.ctx)

	if err != nil {
		return 0, fmt.Errorf("failed to start transaction: %s", err)
	}
	defer tx.Rollback()

	cur, err := tx.Cursor(bucketL1TreeLogs)

	if err != nil {
		return 0, fmt.Errorf("failed to start cursor: %s", err)
	}
	defer cur.Close()

	key, _, err := cur.Last()
	if err != nil {
		return 0, fmt.Errorf("failed to get last key: %s", err)
	}

	keyBlockNumber, _, err := decodeL1LogKey(key)
	if err != nil {
		return 0, fmt.Errorf("failed to decode key: %s", err)
	}

	return keyBlockNumber, nil
}

func (c *L1Cache) getL1TreeLogs(startBlockNumber uint64, logsCh chan<- ethTypes.Log) {
	defer close(logsCh)

	tx, err := c.l1CacheDB.BeginRo(c.ctx)

	if err != nil {
		log.Warn(fmt.Sprintf("[%s] Failed to start transaction: %s", c.logPrefix, err))
		return
	}

	defer tx.Rollback()

	cur, err := tx.Cursor(bucketL1TreeLogs)

	if err != nil {
		log.Warn(fmt.Sprintf("[%s] Failed to start cursor: %s", c.logPrefix, err))
		return
	}

	defer cur.Close()

	for key, value, curErr := cur.First(); curErr == nil && key != nil; key, value, curErr = cur.Next() {
		logEntry := ethTypes.Log{}

		keyBlockNumber, keyLogIndex, err := decodeL1LogKey(key)
		if err != nil {
			log.Warn(fmt.Sprintf("[%s] GetL1TreeLogs: failed to decode key: %s", c.logPrefix, err))
			continue
		}

		if keyBlockNumber < startBlockNumber {
			continue
		}

		err = rlp.DecodeBytes(value, &logEntry)
		if err != nil {
			log.Warn(fmt.Sprintf("[%s] GetL1TreeLogs: failed to deserialize logs: %s", c.logPrefix, err))
			continue
		}

		logEntry.BlockNumber = keyBlockNumber
		logEntry.Index = keyLogIndex

		logsCh <- logEntry
	}

	return
}

func (c *L1Cache) clearTreeLogs() error {
	tx, err := c.l1CacheDB.BeginRw(c.ctx)

	if err != nil {
		return fmt.Errorf("failed to start transaction: %s", err)
	}

	defer tx.Rollback()

	return tx.ClearBucket(bucketL1TreeLogs)
}

func (c *L1Cache) truncateTreeLogs(toBlockNumber uint64) error {
	tx, err := c.l1CacheDB.BeginRw(c.ctx)

	if err != nil {
		return fmt.Errorf("failed to start transaction: %s", err)
	}

	defer tx.Rollback()

	cur, err := tx.Cursor(bucketL1TreeLogs)

	if err != nil {
		return fmt.Errorf("failed to start cursor: %s", err)
	}

	defer cur.Close()

	for key, _, curErr := cur.First(); curErr == nil && key != nil; key, _, curErr = cur.Next() {
		keyBlockNumber, _, err := decodeL1LogKey(key)
		if err != nil {
			log.Warn(fmt.Sprintf("[%s] TruncateTreeLogs: failed to decode key: %s", c.logPrefix, err))
			continue
		}
		if keyBlockNumber >= toBlockNumber {
			break
		}
		err = tx.Delete(bucketL1TreeLogs, key)
		if err != nil {
			log.Warn(fmt.Sprintf("[%s] TruncateTreeLogs: failed to delete key: %s", c.logPrefix, err))
		}
	}

	return tx.Commit()
}

func (c *L1Cache) createCacheBuckets() error {
	tx, err := c.l1CacheDB.BeginRw(c.ctx)
	if err != nil {
		log.Error(fmt.Sprintf("[%s] NewL1Syncer: l1CacheDB.BeginRw error: %s", c.logPrefix, err))
		return err
	}

	defer tx.Rollback()

	for _, bucketName := range cacheBuckets {
		if err = tx.CreateBucket(bucketName); err != nil {
			log.Warn(fmt.Sprintf("[%s] NewL1Syncer: tx.CreateBucket error: %s", c.logPrefix, err))
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		log.Error(fmt.Sprintf("[%s] NewL1Syncer: tx.Commit error: %s", c.logPrefix, err))

		return err
	}

	log.Info(fmt.Sprintf("[%s] createCacheBuckets: cache buckets initialized", c.logPrefix))

	return nil
}

func (c *L1Cache) Close() {
	c.l1CacheDB.Close()
}

func encodeL1LogKey(blockNumber uint64, logIndex uint) []byte {
	buf := make([]byte, 12) // uint64(8) + uint(4)
	binary.BigEndian.PutUint64(buf[:8], blockNumber)
	binary.BigEndian.PutUint32(buf[8:], uint32(logIndex))
	return buf
}

func decodeL1LogKey(data []byte) (uint64, uint, error) {
	if len(data) != 12 { // uint64(8) + uint(4)
		return 0, 0, fmt.Errorf("data length is not 16 bytes")
	}

	return binary.BigEndian.Uint64(data[:8]), uint(binary.BigEndian.Uint32(data[8:])), nil
}
