package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon/core/rawdb"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v2"
)

var DeprecatedFlags = map[string]string{
	"zkevm.gasless":            "zkevm.allow-free-transactions",
	"zkevm.rpc-ratelimit":      "", // Removed without replacement
	"zkevm.datastream-version": "", // Removed without replacement
	"zkevm.l1-cache-port":      "", // Removed without replacement
	"zkevm.l1-cache-enabled":   "", // Removed without replacement
}

type ConfigViolation struct {
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ConfigResult struct {
	Rollup     string            `json:"rollup"`
	Status     string            `json:"status"`
	Violations []ConfigViolation `json:"violations,omitempty"`
}

func RunConfigCheck(ctx *cli.Context) error {
	filePath := ctx.String(ConfigFlag.Name)
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

	// Basic validation logic...
	// (Add more checks here if needed)

	printResult(res, format)
	return nil
}

func RunConfigMigrate(ctx *cli.Context) error {
	filePath := ctx.String(ConfigFlag.Name)
	targetMode := ctx.String("to")
	format := ctx.String("format")

	if filePath == "" {
		return fmt.Errorf("--config is required")
	}

	cfg, err := loadConfig(filePath)
	if err != nil {
		return err
	}

	migratedCfg, res, err := migrateConfig(cfg, filePath, targetMode)
	if err != nil {
		return err
	}

	if len(res.Violations) > 0 || targetMode != "" {
		// Create backup
		backupPath := fmt.Sprintf("%s.%s.bak", filePath, time.Now().Format("20060102-150405"))
		if err := copyFile(filePath, backupPath); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}

		// Save migrated config
		if err := saveConfig(filePath, migratedCfg); err != nil {
			return err
		}
		res.Status = "SUCCEEDED"
		if targetMode != "" {
			res.Violations = append(res.Violations, ConfigViolation{
				Level:   "info",
				Code:    "MIGRATION_COMPLETE",
				Message: fmt.Sprintf("Successfully migrated %s to %s. Backup at %s", filePath, targetMode, backupPath),
			})
		}
	} else {
		res.Status = "NO_CHANGES_NEEDED"
	}

	printResult(res, format)
	return nil
}

func RunConfigListMigrations(ctx *cli.Context) error {
	dataDir := ctx.String(DataDirFlag.Name)
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

	res := ConfigResult{Status: "OK"}
	detectMigrations(tx, &res)

	printResult(res, format)
	return nil
}

func detectMigrations(tx kv.Tx, res *ConfigResult) {
	genesisHash, err := rawdb.ReadCanonicalHash(tx, 0)
	if err != nil {
		res.Violations = append(res.Violations, ConfigViolation{
			Level:   "error",
			Code:    "DB_GENESIS_ERROR",
			Message: fmt.Sprintf("Failed to read genesis: %v", err),
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

	head, _ := getDBHead(tx)
	pendingFound := false

	// 1. Check for SMT -> PMT migration
	if cc.PmtEnabledBlock != nil && cc.PmtEnabledBlock.Uint64() > 0 {
		status := "AVAILABLE"
		if head >= cc.PmtEnabledBlock.Uint64() {
			status = "COMPLETED"
		} else {
			pendingFound = true
		}
		res.Violations = append(res.Violations, ConfigViolation{
			Level:   "info",
			Code:    fmt.Sprintf("MIGRATION_%s", status),
			Message: fmt.Sprintf("Path: SMT -> PMT (Type-1). Activation: %d (Current: %d)", cc.PmtEnabledBlock.Uint64(), head),
		})
	}

	// 2. Check for Sovereign Mode migration
	if cc.SovereignModeBlock != nil && cc.SovereignModeBlock.Uint64() > 0 {
		status := "AVAILABLE"
		if head >= cc.SovereignModeBlock.Uint64() {
			status = "COMPLETED"
		} else {
			pendingFound = true
		}
		res.Violations = append(res.Violations, ConfigViolation{
			Level:   "info",
			Code:    fmt.Sprintf("MIGRATION_%s", status),
			Message: fmt.Sprintf("Path: FEP -> Sovereign. Activation: %d (Current: %d)", cc.SovereignModeBlock.Uint64(), head),
		})
	}

	if len(res.Violations) == 0 {
		res.Violations = append(res.Violations, ConfigViolation{
			Level:   "info",
			Code:    "NO_MIGRATIONS",
			Message: "No upgrade paths discovered in ChainConfig for this network.",
		})
	} else if !pendingFound {
		res.Violations = append(res.Violations, ConfigViolation{
			Level:   "info",
			Code:    "ALL_MIGRATIONS_COMPLETED",
			Message: "No further pending upgrades discovered for the current chain state.",
		})
	}
}

func loadConfig(filePath string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func saveConfig(filePath string, cfg map[string]interface{}) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func migrateConfig(cfg map[string]interface{}, filePath string, targetMode string) (map[string]interface{}, ConfigResult, error) {
	migratedCfg := make(map[string]interface{})
	res := ConfigResult{Rollup: cfg["chain"].(string)}
	changed := false

	for k, v := range cfg {
		if replacement, discovered := DeprecatedFlags[k]; discovered {
			if replacement != "" {
				res.Violations = append(res.Violations, ConfigViolation{
					Level:   "info",
					Code:    "FLAG_RENAMED",
					Message: fmt.Sprintf("Migrating %s -> %s", k, replacement),
				})
				migratedCfg[replacement] = v
				changed = true
			} else {
				res.Violations = append(res.Violations, ConfigViolation{
					Level:   "info",
					Code:    "FLAG_REMOVED",
					Message: fmt.Sprintf("Removing deprecated flag: %s", k),
				})
				changed = true
			}
		} else {
			migratedCfg[k] = v
		}
	}

	if targetMode == "Type-1" {
		// Apply Type-1 Profile
		migratedCfg["zkevm.mode"] = "Type-1"
		migratedCfg["zkevm.skip-smt"] = true
		migratedCfg["zkevm.simultaneous-pmt-and-smt"] = true
		changed = true
	}

	_ = changed // Silence unused warning if needed
	return migratedCfg, res, nil
}

func printResult(res ConfigResult, format string) {
	if format == "json" {
		data, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(data))
	} else {
		if res.Status != "OK" {
			fmt.Printf("[%s] Status: %s\n", res.Rollup, res.Status)
		}
		for _, v := range res.Violations {
			icon := "ℹ️ "
			if v.Level == "warn" {
				icon = "⚠️ "
			} else if v.Level == "error" {
				icon = "❌ "
			}
			fmt.Printf("%s [%s] %s\n", icon, v.Code, v.Message)
		}
		if len(res.Violations) == 0 && res.Status == "OK" {
			fmt.Println("✅ No issues found.")
		}
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
