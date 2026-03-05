package main

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/core"
	"github.com/erigontech/erigon/zk/zk_config"
	"github.com/erigontech/erigon/zk/zk_config/cfg_dynamic_genesis"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: genesisroot <config-dir> [chain-name]\n")
		fmt.Fprintf(os.Stderr, "  config-dir: directory containing <chain>-allocs.json, <chain>-chainspec.json, <chain>-conf.json\n")
		fmt.Fprintf(os.Stderr, "  chain-name: chain name (default: dynamic-hermez-dev)\n")
		os.Exit(1)
	}

	configDir := os.Args[1]
	chain := "dynamic-hermez-dev"
	if len(os.Args) > 2 {
		chain = os.Args[2]
	}

	absDir, err := filepath.Abs(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid config dir: %v\n", err)
		os.Exit(1)
	}

	zk_config.ZKDynamicConfigPath = absDir
	zk_config.ZkUnionConfigPath = ""

	dConf := cfg_dynamic_genesis.NewDynamicGenesisConfig(chain)

	genesis := core.DynamicGenesisBlock(chain)
	genesis.Timestamp = dConf.Timestamp
	genesis.GasLimit = dConf.GasLimit
	genesis.Difficulty = big.NewInt(dConf.Difficulty)

	tmpDir, err := os.MkdirTemp("", "genesisroot-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	block, _, _, err := core.GenesisToBlock(genesis, tmpDir, log.Root())
	if err != nil {
		fmt.Fprintf(os.Stderr, "GenesisToBlock failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("State Root:", block.Root().Hex())
	fmt.Println("Block Hash:", block.Hash().Hex())
}
