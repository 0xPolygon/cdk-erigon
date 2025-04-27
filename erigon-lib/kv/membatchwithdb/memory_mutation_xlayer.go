/*
   Copyright 2022 Erigon contributors
   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at
       http://www.apache.org/licenses/LICENSE-2.0
   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package membatchwithdb

import (
	"context"

	"github.com/c2h5oh/datasize"
	"github.com/ledgerwatch/log/v3"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/mdbx"
)

func NewMemoryBatchNoSequence(tx kv.Tx, tmpDir string, logger log.Logger) *MemoryMutation {
	tmpDB := mdbx.NewMDBX(logger).InMem(tmpDir).GrowthStep(64 * datasize.MB).MapSize(512 * datasize.GB).MustOpen()
	memTx, err := tmpDB.BeginRw(context.Background()) // nolint:gocritic
	if err != nil {
		panic(err)
	}

	return &MemoryMutation{
		db:             tx,
		memDb:          tmpDB,
		memTx:          memTx,
		deletedEntries: make(map[string]map[string]struct{}),
		deletedDups:    map[string]map[string]map[string]struct{}{},
		clearedTables:  make(map[string]struct{}),
	}
}

func NewMemoryBatchWithSizeNoSequence(tx kv.Tx, tmpDir string, mapSize datasize.ByteSize) *MemoryMutation {
	tmpDB := mdbx.NewMDBX(log.New()).InMem(tmpDir).MapSize(mapSize).MustOpen()
	memTx, err := tmpDB.BeginRw(context.Background())
	if err != nil {
		panic(err)
	}
	return &MemoryMutation{
		db:             tx,
		memDb:          tmpDB,
		memTx:          memTx,
		deletedEntries: make(map[string]map[string]struct{}),
		deletedDups:    map[string]map[string]map[string]struct{}{},
		clearedTables:  make(map[string]struct{}),
	}
}
