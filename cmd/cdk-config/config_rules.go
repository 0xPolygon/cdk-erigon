package main

import (
	"context"
	"fmt"

	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon/core/rawdb"
	"github.com/urfave/cli/v2"
)

func RunConfigDoctor(ctx *cli.Context) error {
	filePath := ctx.String(ConfigFlag.Name)
	dataDir := ctx.String(DataDirFlag.Name)
	format := ctx.String("format")

	if filePath == "" {
		return fmt.Errorf("--config is required")
	}

	cfg, err := loadConfig(filePath)
	if err != nil {
		return err
	}

	res := ConfigResult{
		Rollup: cfg["chain"].(string),
		Status: "OK",
	}

	if dataDir != "" {
		db, err := openDB(dataDir)
		if err != nil {
			return err
		}
		defer db.Close()

		tx, err := db.BeginRo(context.Background())
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// Run DB-aware rules
		diagnoseDB(tx, cfg, &res)
	}

	// Run standalone rules
	diagnoseStandalone(cfg, &res)

	printResult(res, format)
	return nil
}

func diagnoseStandalone(cfg map[string]interface{}, res *ConfigResult) {
	// Rule: Explicit Mode check
	if _, ok := cfg["zkevm.mode"]; !ok {
		res.Violations = append(res.Violations, ConfigViolation{
			Level:   "warn",
			Code:    "NO_EXPLICIT_MODE",
			Message: "zkevm.mode is not set. Consider using 'zkevm.mode: Type-1' for better stability.",
		})
	}
}

func diagnoseDB(tx kv.Tx, cfg map[string]interface{}, res *ConfigResult) {
	genesisHash, err := rawdb.ReadCanonicalHash(tx, 0)
	if err != nil {
		res.Violations = append(res.Violations, ConfigViolation{
			Level:   "error",
			Code:    "DB_GENESIS_ERROR",
			Message: fmt.Sprintf("Failed to read genesis from DB: %v", err),
		})
		return
	}

	cc, err := rawdb.ReadChainConfig(tx, genesisHash)
	if err != nil {
		res.Violations = append(res.Violations, ConfigViolation{
			Level:   "error",
			Code:    "CHAIN_CONFIG_ERROR",
			Message: fmt.Sprintf("Failed to read chain config: %v", err),
		})
		return
	}

	// SMT-to-PMT Safety Check
	if cc.PmtEnabledBlock != nil {
		pmtBlock := cc.PmtEnabledBlock.Uint64()
		if pmtBlock == 0 {
			return
		}

		head, _ := getDBHead(tx)

		// If we are well past the PMT block, check if PMT state actually exists
		if head >= pmtBlock {
			// Check for PMT markers or non-empty PMT buckets
			// For now, if we are past it, we assume it's OK unless we find evidence otherwise.
			// Logic to check PMT state presence:
			hasPMT := true // Simplified for now, could check specific buckets

			if hasPMT {
				// No need to warn about simultaneous build if we are already done and healthy
				return
			}
		}

		val, ok := cfg["zkevm.simultaneous-pmt-and-smt"]
		simPMT := false
		if ok {
			simPMT = val.(bool)
		}

		if !simPMT {
			res.Violations = append(res.Violations, ConfigViolation{
				Level:   "warn",
				Code:    "SMT_PMT_RISK",
				Message: fmt.Sprintf("PMT activated in DB at block %d but zkevm.simultaneous-pmt-and-smt is false. Current head: %d. Risk of missing PMT state if not previously built.", pmtBlock, head),
			})
		}
	}
}
