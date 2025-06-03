package utils

import (
	"context"
	"fmt"
	"testing"

	constants "github.com/ledgerwatch/erigon-lib/chain"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/mdbx"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/stretchr/testify/assert"
)

// X Layer: updated tests to be more realistic (test via hermez_db/db.go)
// Taken from hermez_db/db_test.go.
func GetDbTx() (tx kv.RwTx, cleanup func()) {
	dbi, err := mdbx.NewTemporaryMdbx(context.Background(), "")
	if err != nil {
		panic(err)
	}
	tx, err = dbi.BeginRw(context.Background())
	if err != nil {
		panic(err)
	}

	err = hermez_db.CreateHermezBuckets(tx)
	if err != nil {
		panic(err)
	}

	return tx, func() {
		tx.Rollback()
		dbi.Close()
	}
}

func NewTestHermezDb(blockForks map[constants.ForkId]uint64) (reader *hermez_db.HermezDbReader, cleanup func()) {
	tx, cleanup := GetDbTx()
	hdb := hermez_db.NewHermezDb(tx)
	for forkId, blockNum := range blockForks {
		err := hdb.WriteForkIdBlockOnce(uint64(forkId), blockNum)
		assert.NoError(nil, err, "should write forkId block without error")
	}
	return hdb.HermezDbReader, cleanup
}

type TestConfig struct {
	setCalls map[constants.ForkId]uint64
}

func NewTestConfig() *TestConfig {
	return &TestConfig{
		setCalls: make(map[constants.ForkId]uint64),
	}
}

func (tc *TestConfig) SetForkIdBlock(forkId constants.ForkId, blockNum uint64) error {
	tc.setCalls[forkId] = blockNum
	return nil
}

type testScenario struct {
	name          string
	blockForks    map[constants.ForkId]uint64
	expectedCalls map[constants.ForkId]uint64
}

func TestUpdateZkEVMBlockCfg(t *testing.T) {
	scenarios := []testScenario{
		{
			name: "HigherForkEnabled",
			blockForks: map[constants.ForkId]uint64{
				constants.ForkID9Elderberry2: 900,
			},
			expectedCalls: map[constants.ForkId]uint64{
				constants.ForkID9Elderberry2: 900,
				constants.ForkID8Elderberry:  900,
				constants.ForkID7Etrog:       900,
				constants.ForkID6IncaBerry:   900,
				constants.ForkID5Dragonfruit: 900,
				constants.ForkID4:            900,
			},
		},
		{
			name: "MiddleForksExplicitlyEnabled",
			blockForks: map[constants.ForkId]uint64{
				constants.ForkID7Etrog:     700,
				constants.ForkID6IncaBerry: 600,
			},
			expectedCalls: map[constants.ForkId]uint64{
				constants.ForkID7Etrog:       700,
				constants.ForkID6IncaBerry:   600,
				constants.ForkID5Dragonfruit: 600,
				constants.ForkID4:            600,
			},
		},
		{
			name: "MissingEnablements",
			blockForks: map[constants.ForkId]uint64{
				constants.ForkID4:          100,
				constants.ForkID6IncaBerry: 600,
			},
			expectedCalls: map[constants.ForkId]uint64{
				constants.ForkID6IncaBerry:   600,
				constants.ForkID5Dragonfruit: 600,
				constants.ForkID4:            100,
			},
		},
	}

	for _, scenario := range scenarios {
		// t.Run(scenario.name, func(t *testing.T) {
		fmt.Printf("Running scenario: %s\n", scenario.name)

		cfg := NewTestConfig()
		reader, cleanup := NewTestHermezDb(scenario.blockForks)

		err := UpdateZkEVMBlockCfg(cfg, reader, "TestPrefix")
		assert.NoError(t, err, "should not return an error")

		assert.Equal(t, scenario.expectedCalls, cfg.setCalls, "SetForkIdBlock calls mismatch")

		hermez_db.ClearForkIdBlockMap()
		cleanup()
		// })
	}
}
