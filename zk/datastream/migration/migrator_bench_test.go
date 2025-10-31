package migration

import (
	"context"
	"testing"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/types"
)

func BenchmarkMigrate_SmallDataset(b *testing.B) {
	ctx := context.Background()
	tempDir := b.TempDir()
	logger := log.New()

	tcpFile := createTestTCPDatastream(b, tempDir, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		natsManager := createTestNATSManager(b)

		err := natsManager.Start()
		if err != nil {
			b.Fatal(err)
		}

		err = natsManager.InitStreams(ctx)
		if err != nil {
			b.Fatal(err)
		}

		migrator := NewMigrator(tcpFile, natsManager, logger, 10, false)

		_, err = migrator.Migrate(ctx, 0)
		if err != nil {
			b.Fatal(err)
		}

		natsManager.Stop()
	}
}

func BenchmarkMigrate_MediumDataset(b *testing.B) {
	ctx := context.Background()
	tempDir := b.TempDir()
	logger := log.New()

	tcpFile := createTestTCPDatastream(b, tempDir, 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		natsManager := createTestNATSManager(b)

		err := natsManager.Start()
		if err != nil {
			b.Fatal(err)
		}

		err = natsManager.InitStreams(ctx)
		if err != nil {
			b.Fatal(err)
		}

		migrator := NewMigrator(tcpFile, natsManager, logger, 100, false)

		_, err = migrator.Migrate(ctx, 0)
		if err != nil {
			b.Fatal(err)
		}

		natsManager.Stop()
	}
}

func BenchmarkMigrate_DryRun(b *testing.B) {
	ctx := context.Background()
	tempDir := b.TempDir()
	logger := log.New()

	tcpFile := createTestTCPDatastream(b, tempDir, 1000)
	natsManager := createTestNATSManager(b)
	defer natsManager.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		migrator := NewMigrator(tcpFile, natsManager, logger, 100, true)

		_, err := migrator.Migrate(ctx, 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublishBatch_Small(b *testing.B) {
	ctx := context.Background()
	logger := log.New()

	natsManager := createTestNATSManager(b)
	defer natsManager.Stop()

	err := natsManager.Start()
	if err != nil {
		b.Fatal(err)
	}

	err = natsManager.InitStreams(ctx)
	if err != nil {
		b.Fatal(err)
	}

	migrator := NewMigrator("test.dat", natsManager, logger, 10, false)
	err = migrator.initializeNATSMetadata(ctx)
	if err != nil {
		b.Fatal(err)
	}

	batch := createTestBatch(10)
	currentBlock := uint64(0)
	js, _ := natsManager.GetOrCreateDataStream(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats := &MigrationStats{}
		err := migrator.publishBatch(ctx, js, batch, &currentBlock, stats)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublishBatch_Large(b *testing.B) {
	ctx := context.Background()
	logger := log.New()

	natsManager := createTestNATSManager(b)
	defer natsManager.Stop()

	err := natsManager.Start()
	if err != nil {
		b.Fatal(err)
	}

	err = natsManager.InitStreams(ctx)
	if err != nil {
		b.Fatal(err)
	}

	migrator := NewMigrator("test.dat", natsManager, logger, 100, false)
	err = migrator.initializeNATSMetadata(ctx)
	if err != nil {
		b.Fatal(err)
	}

	batch := createTestBatch(100)
	currentBlock := uint64(0)
	js, _ := natsManager.GetOrCreateDataStream(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats := &MigrationStats{}
		err := migrator.publishBatch(ctx, js, batch, &currentBlock, stats)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTCPIterator(b *testing.B) {
	tempDir := b.TempDir()
	tcpFile := createTestTCPDatastream(b, tempDir, 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger := log.New()
		natsManager := createTestNATSManager(b)
		migrator := NewMigrator(tcpFile, natsManager, logger, 100, false)

		streamServer, err := migrator.openTCPDatastream()
		if err != nil {
			b.Fatal(err)
		}

		header := streamServer.GetHeader()
		iterator := &tcpIterator{
			stream:       streamServer,
			curEntryNum:  0,
			totalEntries: header.TotalEntries,
		}

		for {
			entry, err := iterator.NextFileEntry()
			if err != nil {
				b.Fatal(err)
			}
			if entry == nil {
				break
			}
		}

		natsManager.Stop()
	}
}

func BenchmarkCreateNATSMessage(b *testing.B) {
	logger := log.New()
	natsManager := createTestNATSManager(b)
	defer natsManager.Stop()

	migrator := NewMigrator("test.dat", natsManager, logger, 100, false)

	entry := &types.FileEntry{
		EntryType: types.EntryTypeL2Block,
		Data:      make([]byte, 1024),
		EntryNum:  42,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, err := migrator.createNATSMessage(entry, 42)
		if err != nil {
			b.Fatal(err)
		}
	}
}
