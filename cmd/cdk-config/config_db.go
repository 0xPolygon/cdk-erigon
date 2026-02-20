package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/kv/mdbx"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/core/rawdb"
)

func openDB(dataDir string) (kv.RwDB, error) {
	dbPath := filepath.Join(dataDir, "chaindata")
	log.Info("Opening database", "path", dbPath)

	db, err := mdbx.NewMDBX(log.New()).
		Path(dbPath).
		Readonly().
		Open(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}
	return db, nil
}

func getDBHead(tx kv.Tx) (uint64, error) {
	head, err := rawdb.ReadCanonicalHash(tx, 0) // Just to ensure we have markers
	if err != nil {
		return 0, err
	}
	_ = head

	// Read the actual head from the stage progress or headers
	progress := rawdb.ReadHeaderNumber(tx, rawdb.ReadHeadHeaderHash(tx))
	if progress == nil {
		return 0, nil
	}
	return *progress, nil
}
