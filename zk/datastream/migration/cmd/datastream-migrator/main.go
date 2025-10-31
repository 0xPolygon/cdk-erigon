package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/migration"
	"github.com/erigontech/erigon/zk/datastream/natsstream"
)

func main() {
	var (
		tcpFile    = flag.String("tcp-file", "", "Path to TCP datastream file (required for migration and TCP export)")
		natsHost   = flag.String("nats-host", "127.0.0.1", "NATS server host")
		natsPort   = flag.Int("nats-port", 4222, "NATS server port")
		natsDir    = flag.String("nats-dir", "data/nats-storage", "NATS storage directory")
		batchSize  = flag.Int("batch-size", 100, "Number of entries to batch before publishing")
		startFrom  = flag.Uint64("start-from", 0, "Entry number to start migration from (for resuming)")
		dryRun     = flag.Bool("dry-run", false, "Perform dry run without publishing to NATS")
		verbose    = flag.Bool("verbose", false, "Enable verbose logging")
		exportMode = flag.Bool("export", false, "Export datastream to JSON instead of migrating")
		exportFile = flag.String("export-file", "datastream-export.json", "Output file for export mode")
		natsExport = flag.Bool("nats-export", false, "Export from NATS storage instead of TCP file")
	)

	flag.Parse()

	var absPath string
	var err error

	if !*exportMode || !*natsExport {
		if *tcpFile == "" {
			fmt.Fprintf(os.Stderr, "Error: --tcp-file is required\n")
			flag.Usage()
			os.Exit(1)
		}

		absPath, err = filepath.Abs(*tcpFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid file path: %v\n", err)
			os.Exit(1)
		}

		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: TCP datastream file does not exist: %s\n", absPath)
			os.Exit(1)
		}
	}

	logLevel := log.LvlInfo
	if *verbose {
		logLevel = log.LvlDebug
	}
	logger := log.New()
	logger.SetHandler(log.LvlFilterHandler(logLevel, log.StderrHandler))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Received interrupt signal, shutting down...")
		cancel()
	}()

	migrator := migration.NewMigrator(absPath, nil, logger, *batchSize, *dryRun)

	if *exportMode {
		if *natsExport {
			logger.Info("NATS export mode enabled", "outputFile", *exportFile, "natsDir", *natsDir)

			config := natsstream.Config{
				Host:             *natsHost,
				Port:             *natsPort,
				ServerName:       "export-nats",
				ClusterName:      "export-cluster",
				HTTPHost:         "127.0.0.1",
				HTTPPort:         0,
				JetStreamEnabled: true,
				StorageDir:       *natsDir,
				MaxMemory:        2 * 1024 * 1024 * 1024,
				MaxStorage:       100 * 1024 * 1024 * 1024,
				Debug:            *verbose,
				Trace:            false,
			}

			natsManager := natsstream.NewManager(config, logger)
			migrator.SetNATSManager(natsManager)

			logger.Info("Starting NATS server for export", "storageDir", *natsDir)
			if err := natsManager.Start(); err != nil {
				logger.Error("Failed to start NATS server", "error", err)
				os.Exit(1)
			}
			defer natsManager.Stop()

			if err := natsManager.InitStreams(ctx); err != nil {
				logger.Error("Failed to initialize NATS streams", "error", err)
				os.Exit(1)
			}

			if err := migrator.ExportNATS(ctx, *exportFile); err != nil {
				logger.Error("NATS export failed", "error", err)
				os.Exit(1)
			}
		} else {
			logger.Info("TCP export mode enabled", "outputFile", *exportFile)
			if err := migrator.Export(ctx, *exportFile); err != nil {
				logger.Error("Export failed", "error", err)
				os.Exit(1)
			}
		}
		logger.Info("Export completed successfully", "outputFile", *exportFile)
		return
	}

	config := natsstream.Config{
		Host:             *natsHost,
		Port:             *natsPort,
		ServerName:       "migration-nats",
		ClusterName:      "migration-cluster",
		HTTPHost:         "127.0.0.1",
		HTTPPort:         0,
		JetStreamEnabled: true,
		StorageDir:       *natsDir,
		MaxMemory:        2 * 1024 * 1024 * 1024,
		MaxStorage:       100 * 1024 * 1024 * 1024,
		Debug:            *verbose,
		Trace:            false,
	}

	natsManager := natsstream.NewManager(config, logger)
	migrator.SetNATSManager(natsManager)

	if !*dryRun {
		logger.Info("Starting NATS server", "host", *natsHost, "port", *natsPort)
		if err := natsManager.Start(); err != nil {
			logger.Error("Failed to start NATS server", "error", err)
			os.Exit(1)
		}
		defer natsManager.Stop()

		if err := natsManager.InitStreams(ctx); err != nil {
			logger.Error("Failed to initialize NATS streams", "error", err)
			os.Exit(1)
		}

		logger.Info("NATS server started successfully")
	}

	logger.Info("Starting migration",
		"tcpFile", absPath,
		"natsHost", *natsHost,
		"natsPort", *natsPort,
		"batchSize", *batchSize,
		"startFrom", *startFrom,
		"dryRun", *dryRun)

	stats, err := migrator.Migrate(ctx, *startFrom)
	if err != nil {
		logger.Error("Migration failed", "error", err)
		printStats(stats, logger)
		os.Exit(1)
	}

	printStats(stats, logger)
	logger.Info("Migration completed successfully")
}

func printStats(stats *migration.MigrationStats, logger log.Logger) {
	if stats == nil {
		return
	}

	duration := stats.EndTime.Sub(stats.StartTime)
	if duration == 0 {
		duration = time.Since(stats.StartTime)
	}

	rate := float64(0)
	if duration.Seconds() > 0 {
		rate = float64(stats.EntriesMigrated) / duration.Seconds()
	}

	logger.Info("Migration Statistics",
		"totalEntries", stats.TotalEntries,
		"migratedEntries", stats.EntriesMigrated,
		"bookmarksMigrated", stats.BookmarksMigrated,
		"duration", duration.String(),
		"rate", fmt.Sprintf("%.2f entries/sec", rate),
		"errors", len(stats.Errors))

	if len(stats.Errors) > 0 {
		logger.Warn("Errors encountered during migration", "count", len(stats.Errors))
		for i, err := range stats.Errors {
			if i < 10 {
				logger.Warn("Migration error", "index", i, "error", err)
			}
		}
		if len(stats.Errors) > 10 {
			logger.Warn("Additional errors not shown", "count", len(stats.Errors)-10)
		}
	}
}
