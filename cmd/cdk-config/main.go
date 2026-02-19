package main

import (
	"fmt"
	"os"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/urfave/cli/v2"
)

var (
	ConfigFlag = &cli.StringFlag{
		Name:    "config",
		Aliases: []string{"c"},
		Usage:   "Path to the configuration file (YAML/TOML)",
	}
	DataDirFlag = &cli.StringFlag{
		Name:  "datadir",
		Usage: "Path to the node data directory",
	}
)

func main() {
	app := &cli.App{
		Name:    "cdk-config",
		Usage:   "CDK-Erigon configuration assistant and diagnostic tool",
		Version: "1.0.0",
		Commands: []*cli.Command{
			{
				Name:  "check",
				Usage: "Check configuration file for syntax and basic readiness",
				Flags: []cli.Flag{
					ConfigFlag,
					DataDirFlag,
					&cli.StringFlag{
						Name:  "format",
						Usage: "Output format (text, json)",
						Value: "text",
					},
				},
				Action: RunConfigCheck,
			},
			{
				Name:  "migrate",
				Usage: "Migrate deprecated flags and modernize configuration",
				Flags: []cli.Flag{
					ConfigFlag,
					DataDirFlag,
					&cli.StringFlag{
						Name:  "to",
						Usage: "Target profile/mode to migrate to (e.g. Type-1)",
					},
					&cli.StringFlag{
						Name:  "format",
						Usage: "Output format (text, json)",
						Value: "text",
					},
				},
				Action: RunConfigMigrate,
			},
			{
				Name:  "list-migrations",
				Usage: "List possible migration paths based on DB state",
				Flags: []cli.Flag{
					DataDirFlag,
					&cli.StringFlag{
						Name:  "format",
						Usage: "Output format (text, json)",
						Value: "text",
					},
				},
				Action: RunConfigListMigrations,
			},
			{
				Name:  "doctor",
				Usage: "Deep state-aware configuration diagnostics",
				Flags: []cli.Flag{
					ConfigFlag,
					DataDirFlag,
					&cli.StringFlag{
						Name:  "format",
						Usage: "Output format (text, json)",
						Value: "text",
					},
				},
				Action: RunConfigDoctor,
			},
			{
				Name:  "verify-evm",
				Usage: "Audit block headers for integrity and fork compliance",
				Flags: []cli.Flag{
					ConfigFlag,
					DataDirFlag,
					&cli.StringFlag{
						Name:  "format",
						Usage: "Output format (text, json)",
						Value: "text",
					},
					&cli.Uint64Flag{
						Name:  "block",
						Usage: "Starting block number to verify",
						Value: 0,
					},
					&cli.Uint64Flag{
						Name:  "to-block",
						Usage: "End block number (inclusive)",
						Value: 0,
					},
				},
				Action: RunConfigVerifyEVM,
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// Global logger setup
func init() {
	log.Root().SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StderrHandler))
}
