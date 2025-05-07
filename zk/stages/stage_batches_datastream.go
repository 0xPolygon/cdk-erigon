package stages

import (
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
)

type DatastreamClientRunner struct {
	dsClient   DatastreamClient
	logPrefix  string
	stopRunner atomic.Bool
	isReading  atomic.Bool
}

func NewDatastreamClientRunner(dsClient DatastreamClient, logPrefix string) *DatastreamClientRunner {
	return &DatastreamClientRunner{
		dsClient:  dsClient,
		logPrefix: logPrefix,
	}
}

func (r *DatastreamClientRunner) StartRead(errorChan chan struct{}) error {
	if r.isReading.Load() {
		return fmt.Errorf("tried starting datastream client runner thread while another is running")
	}

	r.stopRunner.Store(false)

	go func() {
		routineId := rand.Intn(1000000)

		log.Info(fmt.Sprintf("[%s] Started downloading L2Blocks routine ID: %d", r.logPrefix, routineId))
		defer log.Info(fmt.Sprintf("[%s] Ended downloading L2Blocks routine ID: %d", r.logPrefix, routineId))

		r.isReading.Store(true)
		defer r.isReading.Store(false)

		if err := r.dsClient.ReadAllEntriesToChannel(); err != nil {
			log.Warn("Start to waiting for all entries to be processed before stopping...")
			for len(*r.dsClient.GetEntryChan()) > 0 {
				time.Sleep(1 * time.Second)
			}
			errorChan <- struct{}{}
			log.Warn(fmt.Sprintf("[%s] Error downloading blocks from datastream", r.logPrefix), "error", err)
		}
	}()

	return nil
}

func (r *DatastreamClientRunner) StartRangeRead(
	errorChan chan struct{},
	highestDSL2Block uint64,
	blockRange uint64,
) error {
	if r.isReading.Load() {
		return fmt.Errorf("tried starting datastream client runner thread while another is running")
	}

	r.stopRunner.Store(false)

	entryChan := r.dsClient.GetEntryChan()

	go func() {
		routineId := rand.Intn(1000000)

		log.Info(fmt.Sprintf("[%s] Started downloading L2Blocks routine ID: %d", r.logPrefix, routineId))
		defer log.Info(fmt.Sprintf("[%s] Ended downloading L2Blocks routine ID: %d", r.logPrefix, routineId))

		r.isReading.Store(true)
		defer r.isReading.Store(false)

		// first load up the header of the stream
		if _, err := r.dsClient.GetHeader(); err != nil {
			errorChan <- struct{}{}
			log.Warn(fmt.Sprintf("[%s] Error getting block header from datastream", r.logPrefix), "error", err)
			return
		}

		errorFlag := false
		progress := r.dsClient.GetProgressAtomic()
		for !r.stopRunner.Load() {
			// Wait until all entries in entryChan is consumed
			for len(*entryChan) > 0 {
				time.Sleep(100 * time.Millisecond)
				if r.stopRunner.Load() {
					return
				}
			}

			// Check for conn health
			if err := r.dsClient.HandleStart(); err != nil {
				time.Sleep(1 * time.Second)
				errorChan <- struct{}{}
				log.Warn(fmt.Sprintf("[%s] Error on handle start datastream connection", r.logPrefix), "error", err)
				return
			}

			from := progress.Load()
			if from >= highestDSL2Block {
				return
			}

			to := min(from+blockRange, highestDSL2Block)
			if err := r.dsClient.ReadRangeEntriesToChannel(to); err != nil {
				if !errorFlag {
					// Try to reconnect and get range again
					errorFlag = true
					continue
				} else {
					time.Sleep(1 * time.Second)
					errorChan <- struct{}{}
					log.Warn(fmt.Sprintf("[%s] Error downloading blocks from datastream", r.logPrefix), "error", err)
					return
				}
			} else {
				errorFlag = false
			}
		}

		// Send stop signal
		if err := r.dsClient.TrySendStopSignal(); err != nil {
			time.Sleep(1 * time.Second)
			errorChan <- struct{}{}
			log.Warn(fmt.Sprintf("[%s] Error sending stop signal", r.logPrefix), "error", err)
			return
		}
	}()

	return nil
}

func (r *DatastreamClientRunner) StopRead() {
	r.stopRunner.Swap(true)
	r.dsClient.StopReadingToChannel()
}
