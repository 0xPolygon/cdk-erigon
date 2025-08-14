/*
   Copyright 2021 Erigon contributors

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

package mdbx

import (
	"context"
	"github.com/c2h5oh/datasize"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/log/v3"
)

func MustOpen(path string) kv.RwDB {
	db, err := Open(context.Background(), path, log.New(), false)
	if err != nil {
		panic(err)
	}
	return db
}

func MustOpenInMem(dirtySpaceInGb uint64) kv.RwDB {
	opts := NewMDBX(log.New()).InMem("").MapSize(DefaultMapSize).DirtySpace(dirtySpaceInGb * uint64(1*datasize.GB)).GrowthStep(1 * datasize.GB)

	db, err := opts.Open(context.Background())

	if err != nil {
		panic(err)
	}
	return db
}

// Open - main method to open database.
func Open(ctx context.Context, path string, logger log.Logger, accede bool) (kv.RwDB, error) {
	var db kv.RwDB
	var err error
	opts := NewMDBX(logger).Path(path)
	if accede {
		opts = opts.Accede()
	}
	db, err = opts.Open(ctx)

	if err != nil {
		return nil, err
	}
	return db, nil
}
