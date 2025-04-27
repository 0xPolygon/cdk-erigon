package commands

import (
	"github.com/spf13/cobra"
)

var (
	// For X Layer, split db
	standaloneSmtDb bool   // true: SMT DB is separate from ChainDB
	smtDbPath       string // SMT DB path relative to the datadir
)

// For X Layer, split db
func withStandaloneSmtDb(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&standaloneSmtDb, "standalone-smt-db", false, "SMT DB is separate from ChainDB")
	cmd.Flags().StringVar(&smtDbPath, "smt-db-path", "/home/erigon/data/smt", "Absolute path to SMT DB")
}
