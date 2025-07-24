package l1infotree

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/iden3/go-iden3-crypto/keccak256"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/eth/stagedsync/stages"
	"github.com/ledgerwatch/erigon/zk/contracts"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	zkTypes "github.com/ledgerwatch/erigon/zk/types"
	"github.com/ledgerwatch/log/v3"
)

type Syncer interface {
	IsSyncStarted() bool
	RunQueryBlocks(lastCheckedBlock uint64)
	GetLogsChan() chan []types.Log
	GetProgressMessageChan() chan string
	IsDownloading() bool
	GetHeader(blockNumber uint64) (*types.Header, error)
	L1QueryHeaders(logs []types.Log) (map[uint64]*types.Header, error)
	StopQueryBlocks()
	ConsumeQueryBlocks()
	WaitQueryBlocksToFinish()
}

type Updater struct {
	cfg          *ethconfig.Zk
	syncer       Syncer
	progress     uint64
	latestUpdate *zkTypes.L1InfoTreeUpdate
}

func NewUpdater(cfg *ethconfig.Zk, syncer Syncer) *Updater {
	return &Updater{
		cfg:    cfg,
		syncer: syncer,
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
			u.syncer.StopQueryBlocks()
			u.syncer.ConsumeQueryBlocks()
			u.syncer.WaitQueryBlocksToFinish()
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
		u.syncer.RunQueryBlocks(u.progress)
	}

	return nil
}

func (u *Updater) CheckForInfoTreeUpdatesWithRetry(logPrefix string, tx kv.RwTx) (processed uint64, err error) {
	// loop until earlyReturn is false calling CheckForInfoTreeUpdates
	for {
		if err := u.WarmUp(tx); err != nil {
			return 0, fmt.Errorf("cfg.updater.WarmUp: %w", err)
		}

		processed, earlyReturn, err := u.CheckForInfoTreeUpdates(logPrefix, tx)
		if err != nil {
			return 0, fmt.Errorf("CheckForInfoTreeUpdates: %w", err)
		}
		if !earlyReturn {
			return processed, nil
		}
	}
}

func (u *Updater) CheckForInfoTreeUpdates(logPrefix string, tx kv.RwTx) (processed uint64, earlyReturn bool, err error) {
	defer func() {
		if err != nil {
			u.syncer.StopQueryBlocks()
			u.syncer.ConsumeQueryBlocks()
			u.syncer.WaitQueryBlocksToFinish()
		}
	}()

	hermezDb := hermez_db.NewHermezDb(tx)
	logChan := u.syncer.GetLogsChan()
	progressChan := u.syncer.GetProgressMessageChan()

	// first get all the logs we need to process
	allLogs := make([]types.Log, 0)
LOOP:
	for {
		select {
		case logs := <-logChan:
			allLogs = append(allLogs, logs...)
		case msg := <-progressChan:
			log.Info(fmt.Sprintf("[%s] %s", logPrefix, msg))
		default:
			if !u.syncer.IsDownloading() {
				break LOOP
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// sort the logs by block number - it is important that we process them in order to get the index correct
	// the v2 topic always appears after the v1 topic so we can rely on this ordering to process the logs correctly.
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

	// chunk the logs into batches, so we don't overload the RPC endpoints too much at once
	chunks := chunkLogs(allLogs, 50)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	processed = 0

	var tree *L1InfoTree
	if len(allLogs) > 0 {
		log.Info(fmt.Sprintf("[%s] Checking for L1 info tree updates, logs count:%v", logPrefix, len(allLogs)))
		tree, err = InitialiseL1InfoTree(hermezDb)
		if err != nil {
			return 0, true, fmt.Errorf("InitialiseL1InfoTree: %w", err)
		}
	}

	// process the logs in chunks
	for _, chunk := range chunks {
		select {
		case <-ticker.C:
			log.Info(fmt.Sprintf("[%s] Processed %d/%d logs, %d%% complete", logPrefix, processed, len(allLogs), int(processed)*100/len(allLogs)))
		default:
		}

		headersMap, err := u.syncer.L1QueryHeaders(chunk)
		if err != nil {
			return 0, true, fmt.Errorf("L1QueryHeaders: %w", err)
		}

		for _, l := range chunk {
			switch l.Topics[0] {
			case contracts.UpdateL1InfoTreeTopic:
				// calculate and store the info tree leaf / index
				if err := u.HandleL1InfoTreeUpdate(hermezDb, l, tree, headersMap); err != nil {
					return 0, true, fmt.Errorf("HandleL1InfoTreeUpdate: %w", err)
				}
				processed++
			case contracts.UpdateL1InfoTreeV2Topic:
				// here we can verify that the information we have stored about the info tree is indeed correct
				leafCount := l.Topics[1].Big().Uint64()
				root := l.Data[:32]
				expectedIndex, found, err := hermezDb.GetL1InfoTreeIndexByRoot(common.BytesToHash(root))
				if err != nil {
					return 0, true, fmt.Errorf("GetL1InfoTreeIndexByRoot: %w", err)
				}
				if !found {
					// some leaf must have been missed in this case as the v2 event from the info tree suggests
					// there is a root that we haven't calculated so we need to unwind the info tree locally
					// and try syncing again
					log.Error(fmt.Sprintf("[%s] Could not find an l1 info tree update by root", logPrefix), "root", common.BytesToHash(root).String())
					if err := u.RollbackL1InfoTree(hermezDb, tx); err != nil {
						return 0, true, fmt.Errorf("RollbackL1InfoTree: %w", err)
					}
					u.syncer.RunQueryBlocks(u.progress)

					return processed, true, nil
				}
				if expectedIndex == leafCount-1 {
					if err = hermezDb.WriteConfirmedL1InfoTreeUpdate(expectedIndex, l.BlockNumber); err != nil {
						return 0, true, fmt.Errorf("WriteConfirmedL1InfoTreeUpdate: %w", err)
					}
				} else {
					log.Error(fmt.Sprintf("[%s] Unexpected index for L1 info tree root", logPrefix), "expected", expectedIndex, "found", leafCount-1)
					if err := u.RollbackL1InfoTree(hermezDb, tx); err != nil {
						return 0, true, fmt.Errorf("RollbackL1InfoTree: %w", err)
					}
					u.syncer.RunQueryBlocks(u.progress)

					// early return as we need to re-start the syncing process here to get new
					// leaves from scratch
					return processed, true, nil
				}
				processed++
			default:
				log.Warn("received unexpected topic from l1 info tree stage", "topic", l.Topics[0])
			}
		}
	}

	if len(allLogs) > 0 {
		// here we save progress at the exact block number of the last log.  The syncer will automatically add 1 to this value
		// when the node / syncing process is restarted.
		u.progress = allLogs[len(allLogs)-1].BlockNumber
	}
	if err = stages.SaveStageProgress(tx, stages.L1InfoTree, u.progress); err != nil {
		return 0, false, fmt.Errorf("SaveStageProgress: %w", err)
	}

	return processed, false, nil
}

func (u *Updater) HandleL1InfoTreeUpdate(hermezDb *hermez_db.HermezDb, l types.Log, tree *L1InfoTree, headersMap map[uint64]*types.Header) error {
	var err error
	header := headersMap[l.BlockNumber]
	if header == nil {
		header, err = u.syncer.GetHeader(l.BlockNumber)
		if err != nil {
			return fmt.Errorf("GetHeader: %w", err)
		}
	}

	tmpUpdate, err := createL1InfoTreeUpdate(l, header)
	if err != nil {
		return fmt.Errorf("createL1InfoTreeUpdate: %w", err)
	}

	leafHash := HashLeafData(tmpUpdate.GER, tmpUpdate.ParentHash, tmpUpdate.Timestamp)
	if tree.LeafExists(leafHash) {
		log.Warn("Skipping log as L1 Info Tree leaf already exists", "hash", common.BytesToHash(leafHash[:]).String())
		return nil
	}

	if u.latestUpdate != nil {
		tmpUpdate.Index = u.latestUpdate.Index + 1
	}
	// if latestUpdate is nil then Index = 0 which is the default value so no need to set it
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

	if err = writeL1InfoTreeUpdate(hermezDb, u.latestUpdate, leafHash, newRoot); err != nil {
		return fmt.Errorf("writeL1InfoTreeUpdate: %w", err)
	}

	return nil
}

func (u *Updater) RollbackL1InfoTree(hermezDb *hermez_db.HermezDb, tx kv.RwTx) error {
	// unexpected confirmedIndex means we missed a leaf somewhere so we need to rollback our data and start re-syncing
	confirmedIndex, confirmedL1BlockNumber, err := hermezDb.GetConfirmedL1InfoTreeUpdate()
	if err != nil {
		return fmt.Errorf("GetConfirmedL1InfoTreeUpdate: %w", err)
	}

	if confirmedIndex == 0 && confirmedL1BlockNumber == 0 {
		// so we know there are no confirmed leaves on the tree at this point
		// now we check if we have an index in the DB or not.  If we do then we
		// are in a state where we've detected fork 12 events but our previously
		// synced info tree is invalid with no way of knowing where / how it became
		// invalid.  Not good, this needs intervention so we just return an error
		// to stop further indexes being used in the network
		latestUpdate, err := hermezDb.GetLatestL1InfoTreeUpdate()
		if err != nil {
			return fmt.Errorf("GetLatestL1InfoTreeUpdate: %w", err)
		}
		if latestUpdate.Index > 0 {
			// oh dear
			return errors.New("first fork12 info tree update v2 transaction shows our info tree cannot be verified")
		}

		confirmedL1BlockNumber = u.cfg.L1FirstBlock - 1

	} else {
		// we have some data already so we need to truncate the tables down
		// and reset the latest update data
		if err = truncateL1InfoTreeData(hermezDb, confirmedIndex+1); err != nil {
			return fmt.Errorf("truncateL1InfoTreeData: %w", err)
		}

		// now read the latest confirmed update and set the latest update to this value
		latestUpdate, err := hermezDb.GetL1InfoTreeUpdate(confirmedIndex)
		if err != nil {
			return fmt.Errorf("GetL1InfoTreeUpdate: %w", err)
		}
		u.latestUpdate = latestUpdate
	}

	// stop the syncer
	u.syncer.StopQueryBlocks()
	u.syncer.ConsumeQueryBlocks()
	u.syncer.WaitQueryBlocksToFinish()

	// reset progress for the next iteration
	u.progress = confirmedL1BlockNumber - 1

	if err = stages.SaveStageProgress(tx, stages.L1InfoTree, u.progress); err != nil {
		return fmt.Errorf("SaveStageProgress: %w", err)
	}

	return nil
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

func writeL1InfoTreeUpdate(
	hermezDb *hermez_db.HermezDb,
	update *zkTypes.L1InfoTreeUpdate,
	leafHash [32]byte,
	newRoot [32]byte,
) error {
	var err error

	if err = hermezDb.WriteL1InfoTreeUpdate(update); err != nil {
		return fmt.Errorf("WriteL1InfoTreeUpdate: %w", err)
	}
	if err = hermezDb.WriteL1InfoTreeUpdateToGer(update); err != nil {
		return fmt.Errorf("WriteL1InfoTreeUpdateToGer: %w", err)
	}
	if err = hermezDb.WriteL1InfoTreeLeaf(update.Index, leafHash); err != nil {
		return fmt.Errorf("WriteL1InfoTreeLeaf: %w", err)
	}
	if err = hermezDb.WriteL1InfoTreeRoot(common.BytesToHash(newRoot[:]), update.Index); err != nil {
		return fmt.Errorf("WriteL1InfoTreeRoot: %w", err)
	}
	return nil
}

func truncateL1InfoTreeData(hermezDb *hermez_db.HermezDb, fromIndex uint64) error {
	if err := hermezDb.TruncateL1InfoTreeUpdates(fromIndex); err != nil {
		return fmt.Errorf("TruncateL1InfoTreeUpdates: %w", err)
	}
	if err := hermezDb.TruncateL1InfoTreeUpdatesByGer(fromIndex); err != nil {
		return fmt.Errorf("TruncateL1InfoTreeUpdatesByGer: %w", err)
	}
	if err := hermezDb.TruncateL1InfoTreeLeaves(fromIndex); err != nil {
		return fmt.Errorf("TruncateL1InfoTreeLeaves: %w", err)
	}
	if err := hermezDb.TruncateL1InfoTreeRoots(fromIndex); err != nil {
		return fmt.Errorf("TruncateL1InfoTreeRoots: %w", err)
	}
	return nil
}
