// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package node

import (
	"context"
	"path/filepath"
	"time"

	"github.com/c2h5oh/datasize"
	"golang.org/x/sync/semaphore"

	"github.com/ledgerwatch/erigon/node/nodecfg"

	"github.com/ledgerwatch/log/v3"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/mdbx"
	"github.com/ledgerwatch/erigon-lib/kv/memdb"
)

func OpenDatabaseSMT(ctx context.Context, config *nodecfg.Config, logger log.Logger) (kv.RwDB, error) {
	label := kv.SmtDB
	name := kv.SmtDB.String()

	var db kv.RwDB = nil
	if config.Dirs.DataDir == "" {
		db = memdb.New("")
		return db, nil
	}

	dbPath := filepath.Join(config.Dirs.DataDir, name)

	logger.Info("Opening Database", "label", name, "path", dbPath)
	openFunc := func(exclusive bool) (kv.RwDB, error) {
		roTxLimit := int64(32)
		if config.Http.DBReadConcurrency > 0 {
			roTxLimit = int64(config.Http.DBReadConcurrency)
		}
		roTxsLimiter := semaphore.NewWeighted(roTxLimit) // 1 less than max to allow unlocking to happen
		opts := mdbx.NewMDBX(logger).
			Path(dbPath).Label(label).
			GrowthStep(16 * datasize.MB).
			SyncPeriod(30 * time.Second).
			DBVerbosity(config.DatabaseVerbosity).RoTxsLimiter(roTxsLimiter)

		if exclusive {
			opts = opts.Exclusive()
		}
		if config.MdbxPageSize.Bytes() > 0 {
			opts = opts.PageSize(config.MdbxPageSize.Bytes())
		}
		if config.MdbxDBSizeLimit > 0 {
			opts = opts.MapSize(config.MdbxDBSizeLimit)
		}
		if config.MdbxGrowthStep > 0 {
			opts = opts.GrowthStep(config.MdbxGrowthStep)
		}
		opts = opts.DirtySpace(uint64(512 * datasize.MB))
		return opts.Open(ctx)
	}

	return openFunc(false)
}
