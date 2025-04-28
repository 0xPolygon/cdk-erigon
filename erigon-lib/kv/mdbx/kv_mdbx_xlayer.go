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
	"encoding/json"
	"time"

	"github.com/c2h5oh/datasize"
	"github.com/ledgerwatch/log/v3"

	"github.com/ledgerwatch/erigon-lib/kv"
)

// Custom marshaling for MdbxOpts
func MdbxOptsFromJSON(data []byte) (*MdbxOpts, error) {
	var aux struct {
		Path            string            `json:"path"`
		SyncPeriod      time.Duration     `json:"syncPeriod"`
		MapSize         datasize.ByteSize `json:"mapSize"`
		GrowthStep      datasize.ByteSize `json:"growthStep"`
		ShrinkThreshold int               `json:"shrinkThreshold"`
		Flags           uint              `json:"flags"`
		PageSize        uint64            `json:"pageSize"`
		DirtySpace      uint64            `json:"dirtySpace"`
		MergeThreshold  uint64            `json:"mergeThreshold"`
		Verbosity       kv.DBVerbosityLvl `json:"verbosity"`
		Label           kv.Label          `json:"label"`
		InMem           bool              `json:"inMem"`
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return nil, err
	}

	opts := &MdbxOpts{
		path:            aux.Path,
		syncPeriod:      aux.SyncPeriod,
		mapSize:         aux.MapSize,
		growthStep:      aux.GrowthStep,
		shrinkThreshold: aux.ShrinkThreshold,
		flags:           aux.Flags,
		pageSize:        aux.PageSize,
		dirtySpace:      aux.DirtySpace,
		mergeThreshold:  aux.MergeThreshold,
		verbosity:       aux.Verbosity,
		label:           aux.Label,
		inMem:           aux.InMem,
	}

	return opts, nil
}

func (opts MdbxOpts) GetMapSize() datasize.ByteSize {
	return opts.mapSize
}
func (opts MdbxOpts) GetFlags() uint { return opts.flags }

func (opts MdbxOpts) Logger(log log.Logger) MdbxOpts {
	opts.log = log
	return opts
}

func (opts MdbxOpts) toMap() map[string]interface{} {
	return map[string]interface{}{
		"path":            opts.path,
		"syncPeriod":      opts.syncPeriod,
		"mapSize":         opts.mapSize,
		"growthStep":      opts.growthStep,
		"shrinkThreshold": opts.shrinkThreshold,
		"flags":           opts.flags,
		"pageSize":        opts.pageSize,
		"dirtySpace":      opts.dirtySpace,
		"mergeThreshold":  opts.mergeThreshold,
		"verbosity":       opts.verbosity,
		"label":           opts.label,
		"inMem":           opts.inMem,
	}
}
