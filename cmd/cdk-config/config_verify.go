package main

import (
	"context"
	"fmt"

	"github.com/erigontech/erigon-lib/chain"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon/core/rawdb"
	"github.com/urfave/cli/v2"
)

type BlockVerifyResult struct {
	BlockNumber      uint64   `json:"block"`
	Hash             string   `json:"hash"`
	HashIntegrity    bool     `json:"hash_integrity"`
	ComplianceOK     bool     `json:"compliance_ok"`
	ComplianceErrors []string `json:"compliance_errors,omitempty"`
}

func RunConfigVerifyEVM(ctx *cli.Context) error {
	dataDir := ctx.String(DataDirFlag.Name)
	startBlock := ctx.Uint64("block")
	toBlock := ctx.Uint64("to-block")
	format := ctx.String("format")

	if dataDir == "" {
		return fmt.Errorf("--datadir is required")
	}

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

	genesisHash, err := rawdb.ReadCanonicalHash(tx, 0)
	if err != nil {
		return fmt.Errorf("failed to read genesis: %v", err)
	}
	cc, err := rawdb.ReadChainConfig(tx, genesisHash)
	if err != nil {
		return fmt.Errorf("failed to read chain config: %v", err)
	}

	var blocksToVerify []uint64
	if startBlock == 0 && toBlock == 0 {
		blocksToVerify = harvestForks(tx, cc)
	} else {
		for i := startBlock; i <= toBlock; i++ {
			blocksToVerify = append(blocksToVerify, i)
		}
	}

	results := make([]BlockVerifyResult, 0)
	for _, b := range blocksToVerify {
		results = append(results, verifyBlock(tx, cc, b))
	}

	printVerifyResults(results, format)
	return nil
}

func harvestForks(tx kv.Tx, cc *chain.Config) []uint64 {
	forks := make([]uint64, 0)
	add := func(n *uint64) {
		if n != nil && *n > 0 {
			forks = append(forks, *n)
		}
	}
	// Simplified fork harvesting
	if cc.LondonBlock != nil {
		add(ptr(cc.LondonBlock.Uint64()))
	}
	if cc.PmtEnabledBlock != nil {
		add(ptr(cc.PmtEnabledBlock.Uint64()))
	}
	// (Add more forks as needed)
	return forks
}

func verifyBlock(tx kv.Tx, cc *chain.Config, b uint64) BlockVerifyResult {
	res := BlockVerifyResult{BlockNumber: b, ComplianceOK: true}

	canonHash, err := rawdb.ReadCanonicalHash(tx, b)
	if err != nil {
		res.ComplianceErrors = append(res.ComplianceErrors, fmt.Sprintf("Missing canonical hash: %v", err))
		res.ComplianceOK = false
		return res
	}
	res.Hash = fmt.Sprintf("%x", canonHash)

	header := rawdb.ReadHeader(tx, canonHash, b)
	if header == nil {
		res.ComplianceErrors = append(res.ComplianceErrors, "Missing header in DB")
		res.ComplianceOK = false
		return res
	}

	calcHash := header.Hash()
	if calcHash != canonHash {
		res.HashIntegrity = false
		res.ComplianceErrors = append(res.ComplianceErrors, "Hash mismatch (integrity failure)")
		res.ComplianceOK = false
	} else {
		res.HashIntegrity = true
	}

	return res
}

func printVerifyResults(results []BlockVerifyResult, format string) {
	// Implementation for printing results (similar to printResult)
	for _, r := range results {
		status := "✅"
		if !r.ComplianceOK {
			status = "❌"
		}
		fmt.Printf("%s Block %d: %s (Compliance: %v)\n", status, r.BlockNumber, r.Hash, r.ComplianceOK)
		for _, e := range r.ComplianceErrors {
			fmt.Printf("   - %s\n", e)
		}
	}
}

func ptr(v uint64) *uint64 { return &v }
