package stages

import (
	"fmt"
	"time"

	"github.com/0xPolygonHermez/zkevm-data-streamer/datastreamer"
	dslog "github.com/0xPolygonHermez/zkevm-data-streamer/log"
	"github.com/ledgerwatch/erigon/zk/apollo"
	"github.com/ledgerwatch/erigon/zk/datastream/server"
	"github.com/ledgerwatch/log/v3"
)

func tryToSleepSequencer(localDuration time.Duration, logPrefix string) {
	fullBatchSleepDuration := apollo.GetFullBatchSleepDuration(localDuration)
	if fullBatchSleepDuration > 0 {
		log.Info(fmt.Sprintf("[%s] Slow down sequencer: %v", logPrefix, fullBatchSleepDuration))
		time.Sleep(fullBatchSleepDuration)
	}
}

func createExternalDataStreamServer(cfg SequenceBlockCfg) (server.DataStreamServer, error) {
	// Use hardcoded timeout values & port & datastream file
	writeTimeout := 20 * time.Second
	inactivityTimeout := 10 * time.Minute
	inactivityCheckInterval := 5 * time.Minute
	port := uint16(16900)
	datastreamFile := "/home/data-stream"

	logConfig := &dslog.Config{
		Environment: "production",
		Level:       "warn",
		Outputs:     nil,
	}

	factory := server.NewZkEVMDataStreamServerFactory()

	streamServer, err := factory.CreateStreamServer(
		port,
		uint8(cfg.zk.DatastreamVersion),
		1,
		datastreamer.StreamType(1),
		datastreamFile,
		writeTimeout,
		inactivityTimeout,
		inactivityCheckInterval,
		logConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream server: %v", err)
	}

	fmt.Printf("Successfully created external data stream server with file: %s\n", datastreamFile)

	dataStreamServer := factory.CreateDataStreamServer(streamServer, cfg.zk.L2ChainId)

	return dataStreamServer, nil
}
