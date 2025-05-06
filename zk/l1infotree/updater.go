package l1infotree

import (
	"errors"
	"fmt"
	"golang.org/x/net/context"
	"sort"
	"sync/atomic"
	"time"

	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/core/types"
	ethTypes "github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/eth/ethconfig"
	"github.com/erigontech/erigon/eth/stagedsync/stages"
	"github.com/erigontech/erigon/zk/contracts"
	"github.com/erigontech/erigon/zk/hermez_db"
	zkTypes "github.com/erigontech/erigon/zk/types"
	"github.com/iden3/go-iden3-crypto/keccak256"
)

type Syncer interface {
	IsSyncStarted() bool
	RunQueryBlocks(logPrefix string, lastCheckedBlock uint64, logsCh chan<- []ethTypes.Log, errCh chan<- error)
	//GetLogsChan() chan []types.Log
	// GetProgressMessageChan() chan string
	IsDownloading() bool
	GetHeader(blockNumber uint64) (*types.Header, error)
	L1QueryHeaders(logs []types.Log) (map[uint64]*types.Header, error)
	StopSyncer()
	QueryForRootLog(to uint64) (*types.Log, error)
}

type L2InfoReaderRpc interface {
	GetExitRootTable(endpoint string) ([]zkTypes.L1InfoTreeUpdate, error)
}

type L2Syncer interface {
	IsSyncStarted() bool
	IsSyncFinished() bool
	GetInfoTreeChan() chan []zkTypes.L1InfoTreeUpdate
	RunSyncInfoTree()
	ConsumeInfoTree()
}

type Updater struct {
	cfg          *ethconfig.Zk
	syncer       Syncer
	logsL1Ch     chan []ethTypes.Log
	errL1Ch      chan error
	progress     uint64
	latestUpdate *zkTypes.L1InfoTreeUpdate
	l2Syncer     L2Syncer
}

func NewUpdater(cfg *ethconfig.Zk, syncer Syncer, l2Syncer L2Syncer) *Updater {
	return &Updater{
		cfg: cfg,
		//	updater collects and sorting logs, buffering isn't required
		logsL1Ch: make(chan []ethTypes.Log),
		errL1Ch:  make(chan error),
		syncer:   syncer,
		l2Syncer: l2Syncer,
	}
}

func (u *Updater) GetProgress() uint64 {
	return u.progress
}

func (u *Updater) GetLatestUpdate() *zkTypes.L1InfoTreeUpdate {
	return u.latestUpdate
}

func (u *Updater) WarmUp(tx kv.RwTx) (err error) {
	defer func() {
		if err != nil {
			u.syncer.StopSyncer()
		}
	}()

	hermezDb := hermez_db.NewHermezDb(tx)

	progress, err := stages.GetStageProgress(tx, stages.L1InfoTree)
	if err != nil {
		return err
	}
	if progress == 0 {
		progress = u.cfg.L1FirstBlock - 1
	}

	u.progress = progress

	latestUpdate, err := hermezDb.GetLatestL1InfoTreeUpdate()
	if err != nil {
		return err
	}

	u.latestUpdate = latestUpdate

	if !u.syncer.IsSyncStarted() {
		go u.syncer.RunQueryBlocks("Updater WarmUp", u.progress, u.logsL1Ch, u.errL1Ch)
	}

	return nil
}

func (u *Updater) CheckForInfoTreeUpdates(logPrefix string, tx kv.RwTx) (logsCount int, err error) {
	defer func() {
		if err != nil {
			u.syncer.StopSyncer()
		}
	}()

	log.Info(fmt.Sprintf("[%s] Starting L1 Info Tree CheckForInfoTreeUpdates", logPrefix))

	var allLogs = make([]types.Log, 0)

	hermezDb := hermez_db.NewHermezDb(tx)

	logsTicker := time.NewTicker(10 * time.Millisecond)

LOOP:
	for {
		select {
		case logs, ok := <-u.logsL1Ch:
			if !ok {
				log.Info(fmt.Sprintf("[%s]  CheckForInfoTreeUpdates logs channel closed", logPrefix))
				break LOOP
			}
			allLogs = append(allLogs, logs...)
		case errVal := <-u.errL1Ch:
			if errVal != nil {
				log.Info(fmt.Sprintf("[%s] CheckForInfoTreeUpdates syncer error: %s", logPrefix, errVal))
			}
		case <-logsTicker.C:
			if !u.syncer.IsDownloading() {
				break LOOP
			}
		}
	}

	logsTicker.Stop()

	logsCount = len(allLogs)

	if logsCount == 0 {
		return 0, nil
	}

	// sort the logs by block number - it is important that we process them in order to get the index correct
	sort.Slice(allLogs, func(i, j int) bool {
		l1 := allLogs[i]
		l2 := allLogs[j]
		// first sort by block number and if equal then by tx index
		if l1.BlockNumber != l2.BlockNumber {
			return l1.BlockNumber < l2.BlockNumber
		}
		if l1.TxIndex != l2.TxIndex {
			return l1.TxIndex < l2.TxIndex
		}
		return l1.Index < l2.Index
	})

	log.Info(fmt.Sprintf("[%s] Checking for L1 info tree updates, logs count:%v", logPrefix, logsCount))

	commitBlockNumber := allLogs[logsCount-1].BlockNumber + 1

	// chunk the logs into batches, so we don't overload the RPC endpoints too much at once
	chunks := chunkLogs(allLogs, 50)

	// mark for gc for free memory
	allLogs = nil

	processed := atomic.Uint32{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go logsProcessingStatus(ctx, logPrefix, &processed, logsCount)

	tree, err := InitialiseL1InfoTree(hermezDb)
	if err != nil {
		return 0, fmt.Errorf("InitialiseL1InfoTree: %w", err)
	}

	// process the logs in chunks
	for _, chunk := range chunks {
		err = u.processChunks(tree, hermezDb, &chunk, &processed)
		if err != nil {
			return 0, err
		}
	}

	// save the progress - we add one here so that we don't cause overlap on the next run.  We don't want to duplicate an info tree update in the db
	u.progress = commitBlockNumber
	if err = stages.SaveStageProgress(tx, stages.L1InfoTree, u.progress); err != nil {
		return 0, fmt.Errorf("SaveStageProgress: %w", err)
	}

	return logsCount, nil
}

func (u *Updater) ProcessInfoTreeUpdates(logPrefix string, tx kv.RwTx, allLogs []types.Log) (logsCount int, err error) {
	hermezDb := hermez_db.NewHermezDb(tx)

	logsCount = len(allLogs)

	if logsCount == 0 {
		return 0, nil
	}

	// sort the logs by block number - it is important that we process them in order to get the index correct
	sort.Slice(allLogs, func(i, j int) bool {
		l1 := allLogs[i]
		l2 := allLogs[j]
		// first sort by block number and if equal then by tx index
		if l1.BlockNumber != l2.BlockNumber {
			return l1.BlockNumber < l2.BlockNumber
		}
		if l1.TxIndex != l2.TxIndex {
			return l1.TxIndex < l2.TxIndex
		}
		return l1.Index < l2.Index
	})

	log.Info(fmt.Sprintf("[%s] Checking for L1 info tree updates, logs count:%v", logPrefix, logsCount))

	commitBlockNumber := allLogs[logsCount-1].BlockNumber + 1

	// chunk the logs into batches, so we don't overload the RPC endpoints too much at once
	chunks := chunkLogs(allLogs, 50)

	// mark for gc for free memory
	allLogs = nil

	processed := atomic.Uint32{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go logsProcessingStatus(ctx, logPrefix, &processed, logsCount)

	tree, err := InitialiseL1InfoTree(hermezDb)
	if err != nil {
		return 0, fmt.Errorf("InitialiseL1InfoTree: %w", err)
	}

	// process the logs in chunks
	for _, chunk := range chunks {
		err = u.processChunks(tree, hermezDb, &chunk, &processed)
		if err != nil {
			return 0, err
		}
	}

	// save the progress - we add one here so that we don't cause overlap on the next run.  We don't want to duplicate an info tree update in the db
	u.progress = commitBlockNumber
	if err = stages.SaveStageProgress(tx, stages.L1InfoTree, u.progress); err != nil {
		return 0, fmt.Errorf("SaveStageProgress: %w", err)
	}

	return logsCount, nil
}

// processChunks processes a batch of logs to update the L1 Info Tree and store the results in the database.
// It handles each chunk, log entry based on its topic, performs updates, and tracks processed log count.
func (u *Updater) processChunks(tree *L1InfoTree, db *hermez_db.HermezDb, chunk *[]types.Log, processed *atomic.Uint32) (err error) {
	headersMap, err := u.syncer.L1QueryHeaders(*chunk)
	if err != nil {
		return fmt.Errorf("L1QueryHeaders: %w", err)
	}

	for _, logEntry := range *chunk {
		if logEntry.Topics[0] != contracts.UpdateL1InfoTreeTopic {
			log.Warn("received unexpected topic from l1 info tree stage", "topic", logEntry.Topics[0])
			continue
		}

		header := headersMap[logEntry.BlockNumber]
		if header == nil {
			header, err = u.syncer.GetHeader(logEntry.BlockNumber)
			if err != nil {
				return fmt.Errorf("GetHeader: %w", err)
			}
		}

		tmpUpdate, err := createL1InfoTreeUpdate(logEntry, header)
		if err != nil {
			return fmt.Errorf("createL1InfoTreeUpdate: %w", err)
		}

		leafHash := HashLeafData(tmpUpdate.GER, tmpUpdate.ParentHash, tmpUpdate.Timestamp)
		if tree.LeafExists(leafHash) {
			log.Warn(fmt.Sprintf("Skipping log as L1 Info Tree leaf already exists: hash=%s block=%d", leafHash, logEntry.BlockNumber))
			continue
		}

		if u.latestUpdate != nil {
			tmpUpdate.Index = u.latestUpdate.Index + 1
		} // if latestUpdate is nil then Index = 0 which is the default value so no need to set it
		u.latestUpdate = tmpUpdate

		newRoot, err := tree.AddLeaf(uint32(u.latestUpdate.Index), leafHash)
		if err != nil {
			return fmt.Errorf("tree.AddLeaf: %w", err)
		}

		log.Debug("New L1 Index",
			"index", u.latestUpdate.Index,
			"root", newRoot.String(),
			"mainnet", u.latestUpdate.MainnetExitRoot.String(),
			"rollup", u.latestUpdate.RollupExitRoot.String(),
			"ger", u.latestUpdate.GER.String(),
			"parent", u.latestUpdate.ParentHash.String(),
		)

		if err = handleL1InfoTreeUpdate(db, u.latestUpdate); err != nil {
			return fmt.Errorf("handleL1InfoTreeUpdate: %w", err)
		}
		if err = db.WriteL1InfoTreeLeaf(u.latestUpdate.Index, leafHash); err != nil {
			return fmt.Errorf("WriteL1InfoTreeLeaf: %w", err)
		}
		if err = db.WriteL1InfoTreeRoot(common.BytesToHash(newRoot[:]), u.latestUpdate.Index); err != nil {
			return fmt.Errorf("WriteL1InfoTreeRoot: %w", err)
		}

		processed.Add(1)

	}
	return nil
}

// logsProcessingStatus continuously logs the progress of logs processing at periodic intervals and upon context cancellation.
func logsProcessingStatus(ctx context.Context, logPrefix string, processed *atomic.Uint32, logsCount int) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	printStatus := func() {
		log.Info(fmt.Sprintf("[%s] Processed %d/%d logs, %d%% complete", logPrefix, processed.Load(), logsCount, processed.Load()*100/uint32(logsCount)))
	}

	for {
		select {
		case <-ticker.C:
			printStatus()
		case <-ctx.Done():
			printStatus()
			return
		}
	}
}

func (u *Updater) CheckL2RpcForInfoTreeUpdates(logPrefix string, tx kv.RwTx) (infoTrees []zkTypes.L1InfoTreeUpdate, err error) {
	u.l2Syncer.RunSyncInfoTree()
	// TODO: Fix, this method do nothing
	go u.l2Syncer.ConsumeInfoTree()

	infoTreeChan := u.l2Syncer.GetInfoTreeChan()

LOOP:
	for {
		select {
		case infoTree := <-infoTreeChan:
			infoTrees = append(infoTrees, infoTree...)
		default:
			if u.l2Syncer.IsSyncFinished() {
				log.Info(fmt.Sprintf("[%s] Received %v L2 Info Tree updates", logPrefix, len(infoTrees)))
				break LOOP
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// ok we have all the info tree updates from the l2, now we need to process them
	sort.Slice(infoTrees, func(i, j int) bool {
		return infoTrees[i].Index < infoTrees[j].Index
	})

	hermezDb := hermez_db.NewHermezDb(tx)
	tree, err := InitialiseL1InfoTree(hermezDb)
	if err != nil {
		return nil, fmt.Errorf("InitialiseL1InfoTree: %w", err)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	processed := 0

	for _, update := range infoTrees {
		select {
		case <-ticker.C:
			log.Info(fmt.Sprintf("[%s] Processed %d/%d info tree updates from L2 RPC, %d%% complete", logPrefix, processed, len(infoTrees), processed*100/len(infoTrees)))
		default:
		}

		// create root
		if u.latestUpdate == nil {
			// query for root log by:
			// (from: 0) > (to: update index 1 block number)
			// then return logs[0]
			rootLog, err := u.syncer.QueryForRootLog(update.BlockNumber)
			if err != nil {
				return nil, fmt.Errorf("QueryForRootLog: %w", err)
			}

			switch rootLog.Topics[0] {
			case contracts.UpdateL1InfoTreeTopic:
				header, err := u.syncer.GetHeader(rootLog.BlockNumber)
				if err != nil {
					return nil, fmt.Errorf("GetHeader: %w", err)
				}

				tmpUpdate, err := createL1InfoTreeUpdate(*rootLog, header)
				if err != nil {
					return nil, fmt.Errorf("createL1InfoTreeUpdate: %w", err)
				}
				tmpUpdate.Index = 0

				leafHash := HashLeafData(tmpUpdate.GER, tmpUpdate.ParentHash, tmpUpdate.Timestamp)
				if tree.LeafExists(leafHash) {
					log.Warn("Skipping log as L1 Info Tree leaf already exists", "hash", leafHash)
					continue
				}

				newRoot, err := tree.AddLeaf(uint32(tmpUpdate.Index), leafHash)
				if err != nil {
					return nil, fmt.Errorf("tree.AddLeaf: %w", err)
				}

				if err = handleL1InfoTreeUpdate(hermezDb, tmpUpdate); err != nil {
					return nil, fmt.Errorf("handleL1InfoTreeUpdate: %w", err)
				}

				if err = hermezDb.WriteL1InfoTreeLeaf(tmpUpdate.Index, leafHash); err != nil {
					return nil, fmt.Errorf("WriteL1InfoTreeLeaf: %w", err)
				}

				if err = hermezDb.WriteL1InfoTreeRoot(common.BytesToHash(newRoot[:]), tmpUpdate.Index); err != nil {
					return nil, fmt.Errorf("WriteL1InfoTreeRoot: %w", err)
				}

				processed++
			}
		}

		u.latestUpdate = &update

		leafHash := HashLeafData(u.latestUpdate.GER, u.latestUpdate.ParentHash, u.latestUpdate.Timestamp)
		if tree.LeafExists(leafHash) {
			log.Warn("Skipping log as L1 Info Tree leaf already exists", "hash", leafHash)
			continue
		}

		newRoot, err := tree.AddLeaf(uint32(u.latestUpdate.Index), leafHash)
		if err != nil {
			return nil, fmt.Errorf("tree.AddLeaf: %w", err)
		}

		if err = handleL1InfoTreeUpdate(hermezDb, u.latestUpdate); err != nil {
			return nil, fmt.Errorf("handleL1InfoTreeUpdate: %w", err)
		}

		if err = hermezDb.WriteL1InfoTreeLeaf(u.latestUpdate.Index, leafHash); err != nil {
			return nil, fmt.Errorf("WriteL1InfoTreeLeaf: %w", err)
		}

		if err = hermezDb.WriteL1InfoTreeRoot(common.BytesToHash(newRoot[:]), u.latestUpdate.Index); err != nil {
			return nil, fmt.Errorf("WriteL1InfoTreeRoot: %w", err)
		}

		processed++
	}

	if len(infoTrees) > 0 {
		u.progress = infoTrees[len(infoTrees)-1].BlockNumber + 1
	}

	if err = stages.SaveStageProgress(tx, stages.L1InfoTree, u.progress); err != nil {
		return nil, fmt.Errorf("SaveStageProgress: %w", err)
	}

	return infoTrees, nil
}

func chunkLogs(slice []types.Log, chunkSize int) [][]types.Log {
	var chunks [][]types.Log
	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize

		// If end is greater than the length of the slice, reassign it to the length of the slice
		if end > len(slice) {
			end = len(slice)
		}

		chunks = append(chunks, slice[i:end])
	}
	return chunks
}

func InitialiseL1InfoTree(hermezDb *hermez_db.HermezDb) (*L1InfoTree, error) {
	leaves, err := hermezDb.GetAllL1InfoTreeLeaves()
	if err != nil {
		return nil, fmt.Errorf("GetAllL1InfoTreeLeaves: %w", err)
	}

	allLeaves := make([][32]byte, len(leaves))
	for i, l := range leaves {
		allLeaves[i] = l
	}

	tree, err := NewL1InfoTree(32, allLeaves)
	if err != nil {
		return nil, fmt.Errorf("NewL1InfoTree: %w", err)
	}

	return tree, nil
}

func createL1InfoTreeUpdate(l types.Log, header *types.Header) (*zkTypes.L1InfoTreeUpdate, error) {
	if len(l.Topics) != 3 {
		return nil, errors.New("received log for info tree that did not have 3 topics")
	}

	if l.BlockNumber != header.Number.Uint64() {
		return nil, errors.New("received log for info tree that did not match the block number")
	}

	mainnetExitRoot := l.Topics[1]
	rollupExitRoot := l.Topics[2]
	combined := append(mainnetExitRoot.Bytes(), rollupExitRoot.Bytes()...)
	ger := keccak256.Hash(combined)
	update := &zkTypes.L1InfoTreeUpdate{
		GER:             common.BytesToHash(ger),
		MainnetExitRoot: mainnetExitRoot,
		RollupExitRoot:  rollupExitRoot,
		BlockNumber:     l.BlockNumber,
		Timestamp:       header.Time,
		ParentHash:      header.ParentHash,
	}

	return update, nil
}

func handleL1InfoTreeUpdate(
	hermezDb *hermez_db.HermezDb,
	update *zkTypes.L1InfoTreeUpdate,
) error {
	var err error
	if err = hermezDb.WriteL1InfoTreeUpdate(update); err != nil {
		return fmt.Errorf("WriteL1InfoTreeUpdate: %w", err)
	}
	if err = hermezDb.WriteL1InfoTreeUpdateToGer(update); err != nil {
		return fmt.Errorf("WriteL1InfoTreeUpdateToGer: %w", err)
	}
	return nil
}
