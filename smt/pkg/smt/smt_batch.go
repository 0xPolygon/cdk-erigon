package smt

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/dgravesa/go-parallel/parallel"
	"github.com/ledgerwatch/erigon/smt/pkg/utils"
	"github.com/ledgerwatch/erigon/zk"
	"github.com/ledgerwatch/erigon/zk/metrics"
)

type InsertBatchConfig struct {
	ctx                 context.Context
	logPrefix           string
	shouldPrintProgress bool
}

func NewInsertBatchConfig(ctx context.Context, logPrefix string, shouldPrintProgress bool) InsertBatchConfig {
	return InsertBatchConfig{
		ctx:                 ctx,
		logPrefix:           logPrefix,
		shouldPrintProgress: shouldPrintProgress,
	}
}

func getProgressPrinterPre(logPrefix string, progressType string, size uint64, shouldPrintProgress bool) (progressChanPre *chan uint64, stopProgressPrinterPre func()) {
	var newChan chan uint64
	if shouldPrintProgress {
		newChan, stopProgressPrinterPre = zk.ProgressPrinter(fmt.Sprintf("[%s] SMT incremental progress (%s)", logPrefix, progressType), size, false)
	} else {
		newChan = make(chan uint64, size)
		var once sync.Once

		stopProgressPrinterPre = func() {
			once.Do(func() { close(newChan) })
		}
	}

	return &newChan, stopProgressPrinterPre
}

func (s *SMT) InsertBatch(cfg InsertBatchConfig, nodeKeys []*utils.NodeKey, nodeValues []*utils.NodeValue8, nodeValuesHashes []*[4]uint64, rootNodeHash *utils.NodeKey) (r *SMTResponse, err error) {
	s.clearUpMutex.Lock()
	defer s.clearUpMutex.Unlock()

	var (
		maxInsertingNodePathLevel = 0
		size                      = len(nodeKeys)
		smtBatchNodeRoot          *smtBatchNode
		nodeHashesForDelete       = make(map[uint64]map[uint64]map[uint64]map[uint64]*utils.NodeKey)
	)

	//BE CAREFUL: modifies the arrays
	if err := s.preprocessBatchedNodeValues(
		cfg.logPrefix,
		cfg.shouldPrintProgress,
		&nodeKeys,
		&nodeValues,
		&nodeValuesHashes,
		&rootNodeHash,
	); err != nil {
		return nil, fmt.Errorf("preprocessBatchedNodeValues: %w", err)
	}

	//DO NOT MOVE ABOVE PREPROCESS
	size = len(nodeKeys)

	progressChan, stopProgressPrinter := getProgressPrinterPre(cfg.logPrefix, "process", uint64(size), cfg.shouldPrintProgress)
	defer stopProgressPrinter()

	for i := 0; i < size; i++ {
		select {
		case <-cfg.ctx.Done():
			return nil, fmt.Errorf("context done")
		case *progressChan <- uint64(1):
		default:
		}

		insertingNodeKey := nodeKeys[i]
		insertingNodeValue := nodeValues[i]
		insertingNodeValueHash := nodeValuesHashes[i]
		insertingNodePath := insertingNodeKey.GetPath()
		insertingNodePathLevel, insertingPointerToSmtBatchNode, visitedNodeHashes, err := s.findInsertingPoint(insertingNodePath, rootNodeHash, &smtBatchNodeRoot, insertingNodeValue.IsZero())
		if err != nil {
			return nil, err
		}
		updateNodeHashesForDelete(nodeHashesForDelete, visitedNodeHashes)

		// special case if root does not exists yet
		if (*insertingPointerToSmtBatchNode) == nil {
			if !insertingNodeValue.IsZero() {
				*insertingPointerToSmtBatchNode = newSmtBatchNodeLeaf(insertingNodeKey, (*utils.NodeKey)(insertingNodeValueHash), nil)
			}
			// else branch would be for deleting a value but the root does not exists => there is nothing to delete
			continue
		}

		insertingRemainingKey := utils.RemoveKeyBits(*insertingNodeKey, insertingNodePathLevel)
		if !insertingNodeValue.IsZero() {
			if !((*insertingPointerToSmtBatchNode).isLeaf()) {
				if insertingPointerToSmtBatchNode, err = (*insertingPointerToSmtBatchNode).createALeafInEmptyDirection(insertingNodePath, insertingNodePathLevel, insertingNodeKey); err != nil {
					return nil, err
				}
				insertingRemainingKey = *((*insertingPointerToSmtBatchNode).nodeLeftHashOrRemainingKey)
				insertingNodePathLevel++
			}

			if !((*insertingPointerToSmtBatchNode).nodeLeftHashOrRemainingKey.IsEqualTo(insertingRemainingKey)) {
				currentTreeNodePath := utils.JoinKey(insertingNodePath[:insertingNodePathLevel], *(*insertingPointerToSmtBatchNode).nodeLeftHashOrRemainingKey).GetPath()
				for insertingNodePath[insertingNodePathLevel] == currentTreeNodePath[insertingNodePathLevel] {
					insertingPointerToSmtBatchNode = (*insertingPointerToSmtBatchNode).expandLeafByAddingALeafInDirection(insertingNodePath, insertingNodePathLevel)
					insertingNodePathLevel++
				}

				(*insertingPointerToSmtBatchNode).expandLeafByAddingALeafInDirection(currentTreeNodePath, insertingNodePathLevel)

				if insertingPointerToSmtBatchNode, err = (*insertingPointerToSmtBatchNode).createALeafInEmptyDirection(insertingNodePath, insertingNodePathLevel, insertingNodeKey); err != nil {
					return nil, err
				}
				// EXPLAIN THE LINE BELOW: there is no need to update insertingRemainingKey because it is not needed anymore therefore its value is incorrect if used after this line
				// insertingRemainingKey = *((*insertingPointerToSmtBatchNode).nodeLeftKeyOrRemainingKey)
				insertingNodePathLevel++
			}

			// EXPLAIN THE LINE BELOW: cannot delete the old values because it might be used as a value of an another node
			// updateNodeHashesForDelete(nodeHashesForDelete, []*utils.NodeKey{(*insertingPointerToSmtBatchNode).nodeRightHashOrValueHash})
			(*insertingPointerToSmtBatchNode).nodeRightHashOrValueHash = (*utils.NodeKey)(insertingNodeValueHash)
		} else {
			if (*insertingPointerToSmtBatchNode).nodeLeftHashOrRemainingKey.IsEqualTo(insertingRemainingKey) {
				// EXPLAIN THE LINE BELOW: cannot delete the old values because it might be used as a value of an another node
				// updateNodeHashesForDelete(nodeHashesForDelete, []*utils.NodeKey{(*insertingPointerToSmtBatchNode).nodeRightHashOrValueHash})

				parentAfterDelete := &((*insertingPointerToSmtBatchNode).parentNode)
				*insertingPointerToSmtBatchNode = nil
				insertingPointerToSmtBatchNode = parentAfterDelete
				if (*insertingPointerToSmtBatchNode) != nil {
					(*insertingPointerToSmtBatchNode).updateHashesAfterDelete()
				}
				insertingNodePathLevel--
				// EXPLAIN THE LINE BELOW: there is no need to update insertingRemainingKey because it is not needed anymore therefore its value is incorrect if used after this line
				// insertingRemainingKey = utils.RemoveKeyBits(*insertingNodeKey, insertingNodePathLevel)
			}

			for {
				// the root node has been deleted so we can safely break
				if *insertingPointerToSmtBatchNode == nil {
					break
				}

				// a leaf (with mismatching remaining key) => nothing to collapse
				if (*insertingPointerToSmtBatchNode).isLeaf() {
					break
				}

				// does not have a single leaf => nothing to collapse
				theSingleNodeLeaf, theSingleNodeLeafDirection := (*insertingPointerToSmtBatchNode).getTheSingleLeafAndDirectionIfAny()
				if theSingleNodeLeaf == nil {
					break
				}

				insertingPointerToSmtBatchNode = (*insertingPointerToSmtBatchNode).collapseLeafByRemovingTheSingleLeaf(insertingNodePath, insertingNodePathLevel, theSingleNodeLeaf, theSingleNodeLeafDirection)
				insertingNodePathLevel--
			}
		}

		if maxInsertingNodePathLevel < insertingNodePathLevel {
			maxInsertingNodePathLevel = insertingNodePathLevel
		}
	}
	select {
	case *progressChan <- uint64(1):
	default:
	}
	stopProgressPrinter()

	if err := s.updateDepth(maxInsertingNodePathLevel); err != nil {
		return nil, fmt.Errorf("updateDepth: %w", err)
	}

	if err := s.deleteBatchedNodeValues(cfg.logPrefix, nodeHashesForDelete); err != nil {
		return nil, fmt.Errorf("deleteBatchedNodeValues: %w", err)
	}

	if err := s.saveBatchedNodeValues(cfg.logPrefix, nodeValues, nodeValuesHashes); err != nil {
		return nil, fmt.Errorf("saveBatchedNodeValues: %w", err)
	}

	if smtBatchNodeRoot == nil {
		rootNodeHash = &utils.NodeKey{0, 0, 0, 0}
	} else {
		sdh := newSmtDfsHelper(s)

		go func() {
			defer sdh.destroy()

			calculateAndSaveHashesDfs(sdh, smtBatchNodeRoot, make([]int, 256), 0)
			rootNodeHash = (*utils.NodeKey)(smtBatchNodeRoot.hash)
		}()

		if !s.noSaveOnInsert {
			if err = sdh.startConsumersLoop(s); err != nil {
				return nil, fmt.Errorf("saving smt hashes dfs: %w", err)
			}
		}
		sdh.wg.Wait()
	}
	if err := s.setLastRoot(*rootNodeHash); err != nil {
		return nil, err
	}

	return &SMTResponse{
		Mode:          "batch insert",
		NewRootScalar: rootNodeHash,
	}, nil
}

// returns the new size of the values batch after removing duplicate entries
func (s *SMT) preprocessBatchedNodeValues(
	logPrefix string,
	shouldPrintProgress bool,
	nodeKeys *[]*utils.NodeKey,
	nodeValues *[]*utils.NodeValue8,
	nodeValuesHashes *[]*[4]uint64,
	rootNodeHash **utils.NodeKey,
) error {
	progressChanPre, stopProgressPrinterPre := getProgressPrinterPre(logPrefix, "pre-process", 4, shouldPrintProgress)
	defer stopProgressPrinterPre()

	if err := validateDataLengths(*nodeKeys, *nodeValues, nodeValuesHashes); err != nil {
		return fmt.Errorf("validateDataLengths: %w", err)
	}
	*progressChanPre <- uint64(1)

	if err := removeDuplicateEntriesByKeys(nodeKeys, nodeValues, nodeValuesHashes); err != nil {
		return fmt.Errorf("removeDuplicateEntriesByKeys: %w", err)
	}
	*progressChanPre <- uint64(1)

	if err := calculateNodeValueHashesIfMissing(*nodeValues, nodeValuesHashes); err != nil {
		return fmt.Errorf("calculateNodeValueHashesIfMissing: %w", err)
	}
	*progressChanPre <- uint64(1)

	if err := calculateRootNodeHashIfNil(s, rootNodeHash); err != nil {
		return fmt.Errorf("calculateRootNodeHashIfNil: %w", err)
	}
	*progressChanPre <- uint64(1)
	stopProgressPrinterPre()

	return nil
}

func (s *SMT) deleteBatchedNodeValues(
	logPrefix string,
	nodeHashesForDelete map[uint64]map[uint64]map[uint64]map[uint64]*utils.NodeKey,
) error {
	progressChanDel, stopProgressPrinterDel := getProgressPrinterPre(logPrefix, "deletes", uint64(len(nodeHashesForDelete)), false)
	defer stopProgressPrinterDel()

	for _, mapLevel0 := range nodeHashesForDelete {
		*progressChanDel <- uint64(1)
		for _, mapLevel1 := range mapLevel0 {
			for _, mapLevel2 := range mapLevel1 {
				for _, nodeHash := range mapLevel2 {
					metrics.GetLogStatistics().CumulativeValue(metrics.ZKHashSMTDeleteByNodeKey, 1)
					start := time.Now()
					if err := s.Db.DeleteByNodeKey(*nodeHash); err != nil {
						return fmt.Errorf("DeleteByNodeKey: %w", err)
					}
					metrics.GetLogStatistics().CumulativeMicroTiming(metrics.ZKHashSMTDeleteByNodeKeyTiming, time.Since(start))
					metrics.GetLogStatistics().CumulativeValue(metrics.ZKHashSMTDeleteHashKey, 1)
					start = time.Now()
					if err := s.Db.DeleteHashKey(*nodeHash); err != nil {
						return fmt.Errorf("DeleteHashKey: %w", err)
					}
					metrics.GetLogStatistics().CumulativeMicroTiming(metrics.ZKHashSMTDeleteHashKeyTiming, time.Since(start))
				}
			}
		}
	}
	stopProgressPrinterDel()

	return nil
}

func (s *SMT) saveBatchedNodeValues(
	logPrefix string,
	nodeValues []*utils.NodeValue8,
	nodeValuesHashes []*[4]uint64,
) error {
	progressChanFin, stopProgressPrinterFin := getProgressPrinterPre(logPrefix, "finalize", uint64(len(nodeValues)), false)
	defer stopProgressPrinterFin()

	for i, nodeValue := range nodeValues {
		select {
		case *progressChanFin <- uint64(1):
		default:
		}
		if !nodeValue.IsZero() {
			if err := s.hashSaveByPointers(nodeValue.ToUintArrayByPointer(), &utils.BranchCapacity, nodeValuesHashes[i]); err != nil {
				return err
			}
		}
	}
	stopProgressPrinterFin()
	return nil
}

func validateDataLengths(
	nodeKeys []*utils.NodeKey,
	nodeValues []*utils.NodeValue8,
	nodeValuesHashes *[]*[4]uint64,
) error {
	var size int = len(nodeKeys)

	if len(nodeValues) != size {
		return fmt.Errorf("mismatch nodeValues length, expected %d but got %d", size, len(nodeValues))
	}

	if (*nodeValuesHashes) == nil {
		(*nodeValuesHashes) = make([]*[4]uint64, size)
	}
	if len(*nodeValuesHashes) != size {
		return fmt.Errorf("mismatch nodeValuesHashes length, expected %d but got %d", size, len(*nodeValuesHashes))
	}

	return nil
}

func removeDuplicateEntriesByKeys(
	nodeKeys *[]*utils.NodeKey,
	nodeValues *[]*utils.NodeValue8,
	nodeValuesHashes *[]*[4]uint64,
) error {
	size := len(*nodeKeys)
	storage := make(map[uint64]map[uint64]map[uint64]map[uint64]int)

	resultNodeKeys := make([]*utils.NodeKey, 0, size)
	resultNodeValues := make([]*utils.NodeValue8, 0, size)
	resultNodeValuesHashes := make([]*[4]uint64, 0, size)

	for i, nodeKey := range *nodeKeys {
		setNodeKeyMapValue(storage, nodeKey, i)
	}

	for i, nodeKey := range *nodeKeys {
		latestIndex, found := getNodeKeyMapValue(storage, nodeKey)
		if !found {
			return fmt.Errorf("key not found")
		}

		if latestIndex == i {
			resultNodeKeys = append(resultNodeKeys, nodeKey)
			resultNodeValues = append(resultNodeValues, (*nodeValues)[i])
			resultNodeValuesHashes = append(resultNodeValuesHashes, (*nodeValuesHashes)[i])
		}
	}

	*nodeKeys = resultNodeKeys
	*nodeValues = resultNodeValues
	*nodeValuesHashes = resultNodeValuesHashes

	return nil
}

func calculateNodeValueHashesIfMissing(
	nodeValues []*utils.NodeValue8,
	nodeValuesHashes *[]*[4]uint64,
) error {
	var globalError error
	size := len(nodeValues)
	cpuNum := parallel.DefaultNumGoroutines()

	if size > (cpuNum << 2) {
		var wg sync.WaitGroup
		itemsPerCpu := (size + cpuNum - 1) / cpuNum

		wg.Add(cpuNum)

		for cpuIndex := 0; cpuIndex < cpuNum; cpuIndex++ {
			go func(cpuIndexInThread int) {
				defer wg.Done()
				startIndex := cpuIndexInThread * itemsPerCpu
				endIndex := startIndex + itemsPerCpu
				if endIndex > size {
					endIndex = size
				}
				err := calculateNodeValueHashesIfMissingInInterval(nodeValues, nodeValuesHashes, startIndex, endIndex)
				if err != nil {
					globalError = err
				}
			}(cpuIndex)
		}

		wg.Wait()
	} else {
		globalError = calculateNodeValueHashesIfMissingInInterval(nodeValues, nodeValuesHashes, 0, len(nodeValues))
	}

	return globalError
}

func calculateNodeValueHashesIfMissingInInterval(
	nodeValues []*utils.NodeValue8,
	nodeValuesHashes *[]*[4]uint64,
	startIndex,
	endIndex int,
) error {
	for i := startIndex; i < endIndex; i++ {
		if (*nodeValuesHashes)[i] != nil {
			continue
		}

		nodeValueHashObj := utils.Hash(nodeValues[i].ToUintArray(), utils.BranchCapacity)
		(*nodeValuesHashes)[i] = &nodeValueHashObj
	}

	return nil
}

func calculateRootNodeHashIfNil(s *SMT, root **utils.NodeKey) error {
	if (*root) == nil {
		oldRootObj, err := s.getLastRoot()
		if err != nil {
			return err
		}
		(*root) = &oldRootObj
	}
	return nil
}

func (s *SMT) findInsertingPoint(
	insertingNodePath []int,
	insertingPointerNodeHash *utils.NodeKey,
	insertingPointerToSmtBatchNode **smtBatchNode,
	fetchDirectSiblings bool,
) (
	insertingNodePathLevel int,
	nextInsertingPointerToSmtBatchNode **smtBatchNode,
	visitedNodeHashes []*utils.NodeKey,
	err error,
) {
	insertingNodePathLevel = -1
	visitedNodeHashes = make([]*utils.NodeKey, 0, 256)

	var (
		insertingPointerToSmtBatchNodeParent *smtBatchNode
		nextInsertingPointerNodeHash         *utils.NodeKey
	)

	for {
		if (*insertingPointerToSmtBatchNode) == nil { // update in-memory structure from db
			if !insertingPointerNodeHash.IsZero() {
				*insertingPointerToSmtBatchNode, err = s.fetchNodeDataFromDb(insertingPointerNodeHash, insertingPointerToSmtBatchNodeParent)
				if err != nil {
					return -2, insertingPointerToSmtBatchNode, visitedNodeHashes, err
				}
				visitedNodeHashes = append(visitedNodeHashes, insertingPointerNodeHash)
			} else {
				if insertingNodePathLevel != -1 {
					return -2, insertingPointerToSmtBatchNode, visitedNodeHashes, fmt.Errorf("nodekey is zero at non-root level")
				}
			}
		}

		if (*insertingPointerToSmtBatchNode) == nil {
			if insertingNodePathLevel != -1 {
				return -2, insertingPointerToSmtBatchNode, visitedNodeHashes, fmt.Errorf("working smt pointer is nil at non-root level")
			}
			break
		}

		insertingNodePathLevel++

		if (*insertingPointerToSmtBatchNode).isLeaf() {
			break
		}

		if fetchDirectSiblings {
			// load direct siblings of a non-leaf from the DB
			if (*insertingPointerToSmtBatchNode).leftNode == nil {
				(*insertingPointerToSmtBatchNode).leftNode, err = s.fetchNodeDataFromDb((*insertingPointerToSmtBatchNode).nodeLeftHashOrRemainingKey, (*insertingPointerToSmtBatchNode))
				if err != nil {
					return -2, insertingPointerToSmtBatchNode, visitedNodeHashes, err
				}
				visitedNodeHashes = append(visitedNodeHashes, (*insertingPointerToSmtBatchNode).nodeLeftHashOrRemainingKey)
			}
			if (*insertingPointerToSmtBatchNode).rightNode == nil {
				(*insertingPointerToSmtBatchNode).rightNode, err = s.fetchNodeDataFromDb((*insertingPointerToSmtBatchNode).nodeRightHashOrValueHash, (*insertingPointerToSmtBatchNode))
				if err != nil {
					return -2, insertingPointerToSmtBatchNode, visitedNodeHashes, err
				}
				visitedNodeHashes = append(visitedNodeHashes, (*insertingPointerToSmtBatchNode).nodeRightHashOrValueHash)
			}
		}

		insertDirection := insertingNodePath[insertingNodePathLevel]
		nextInsertingPointerNodeHash = (*insertingPointerToSmtBatchNode).getNextNodeHashInDirection(insertDirection)
		nextInsertingPointerToSmtBatchNode = (*insertingPointerToSmtBatchNode).getChildInDirection(insertDirection)
		if nextInsertingPointerNodeHash.IsZero() && (*nextInsertingPointerToSmtBatchNode) == nil {
			break
		}

		insertingPointerNodeHash = nextInsertingPointerNodeHash
		insertingPointerToSmtBatchNodeParent = *insertingPointerToSmtBatchNode
		insertingPointerToSmtBatchNode = nextInsertingPointerToSmtBatchNode
	}

	return insertingNodePathLevel, insertingPointerToSmtBatchNode, visitedNodeHashes, nil
}

func updateNodeHashesForDelete(
	nodeHashesForDelete map[uint64]map[uint64]map[uint64]map[uint64]*utils.NodeKey,
	visitedNodeHashes []*utils.NodeKey,
) {
	for _, visitedNodeHash := range visitedNodeHashes {
		if visitedNodeHash == nil {
			continue
		}

		setNodeKeyMapValue(nodeHashesForDelete, visitedNodeHash, visitedNodeHash)
	}
}

// =========== op1 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	initialLevel int,
//) {
//	type stackFrame struct {
//		node  *smtBatchNode
//		level uint16 // 使用更小的类型
//		state byte   // 使用 byte 替代 int
//	}
//
//	// 预分配栈并尽量减少扩容
//	stack := make([]stackFrame, 0, 1024)
//	stack = append(stack, stackFrame{node: smtBatchRootNode, level: uint16(initialLevel), state: 0})
//	noSave := sdh.s.noSaveOnInsert
//	dataChan := sdh.dataChan // 缓存通道引用
//
//	for len(stack) > 0 {
//		frameIdx := len(stack) - 1
//		frame := &stack[frameIdx]
//
//		node := frame.node
//		if node.isLeaf() {
//			concat := utils.ConcatArrays4ByPointers(
//				node.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				node.nodeRightHashOrValueHash.AsUint64Pointer(),
//			)
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(concat, &utils.LeafCapacity)
//			node.hash = hashObj
//
//			if !noSave {
//				leafKey := utils.JoinKey(path[:frame.level], *node.nodeLeftHashOrRemainingKey)
//				buffer := newSmtDfsHelperDataStruct(hashObj, hashValue)
//				dataChan <- buffer
//				dataChan <- newSmtDfsHelperDataStruct(hashObj, leafKey)
//			}
//			stack = stack[:frameIdx]
//			continue
//		}
//
//		var totalHash utils.NodeValue8
//
//		switch frame.state {
//		case 0:
//			path[frame.level] = 0
//			if left := node.leftNode; left != nil {
//				frame.state = 1
//				stack = append(stack, stackFrame{node: left, level: frame.level + 1})
//			} else {
//				totalHash.SetHalfValue(*node.nodeLeftHashOrRemainingKey, 0)
//				frame.state = 1
//			}
//
//		case 1:
//			path[frame.level] = 1
//			if right := node.rightNode; right != nil {
//				frame.state = 2
//				stack = append(stack, stackFrame{node: right, level: frame.level + 1})
//			} else {
//				totalHash.SetHalfValue(*node.nodeRightHashOrValueHash, 1)
//				frame.state = 2
//			}
//
//		case 2:
//			if left := node.leftNode; left != nil {
//				totalHash.SetHalfValue(*left.hash, 0)
//			} else {
//				totalHash.SetHalfValue(*node.nodeLeftHashOrRemainingKey, 0)
//			}
//			if right := node.rightNode; right != nil {
//				totalHash.SetHalfValue(*right.hash, 1)
//			} else {
//				totalHash.SetHalfValue(*node.nodeRightHashOrValueHash, 1)
//			}
//
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//			node.hash = hashObj
//
//			if !noSave {
//				dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//			}
//			stack = stack[:frameIdx]
//		}
//	}
//}

// =========== op2 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	initialLevel int,
//) {
//	type stackFrame struct {
//		node  *smtBatchNode
//		level int
//		state int // 0: initial, 1: left processed, 2: right processed
//	}
//
//	// 使用栈替代递归
//	stack := make([]stackFrame, 0, 1024) // 预分配一些空间
//	stack = append(stack, stackFrame{node: smtBatchRootNode, level: initialLevel, state: 0})
//	noSave := sdh.s.noSaveOnInsert
//
//	for len(stack) > 0 {
//		// 获取当前栈顶元素
//		current := &stack[len(stack)-1]
//
//		if current.node.isLeaf() {
//			// 处理叶子节点
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(
//				utils.ConcatArrays4ByPointers(
//					current.node.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//					current.node.nodeRightHashOrValueHash.AsUint64Pointer(),
//				),
//				&utils.LeafCapacity,
//			)
//			current.node.hash = hashObj
//
//			if !noSave {
//				leafKey := utils.JoinKey(path[:current.level], *current.node.nodeLeftHashOrRemainingKey)
//				buffer := newSmtDfsHelperDataStruct(hashObj, hashValue)
//				sdh.dataChan <- buffer
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, leafKey)
//			}
//			stack = stack[:len(stack)-1] // 出栈
//			continue
//		}
//
//		var totalHash utils.NodeValue8
//
//		switch current.state {
//		case 0: // 初始状态，处理左子节点
//			path[current.level] = 0
//			if current.node.leftNode != nil {
//				current.state = 1
//				stack = append(stack, stackFrame{node: current.node.leftNode, level: current.level + 1, state: 0})
//			} else {
//				totalHash.SetHalfValue(*current.node.nodeLeftHashOrRemainingKey, 0)
//				current.state = 1
//			}
//
//		case 1: // 处理右子节点
//			path[current.level] = 1
//			if current.node.rightNode != nil {
//				current.state = 2
//				stack = append(stack, stackFrame{node: current.node.rightNode, level: current.level + 1, state: 0})
//			} else {
//				totalHash.SetHalfValue(*current.node.nodeRightHashOrValueHash, 1)
//				current.state = 2
//			}
//
//		case 2: // 所有子节点处理完成，计算哈希
//			// 如果左右子节点都已处理过，则从子节点获取哈希值
//			if current.node.leftNode != nil {
//				totalHash.SetHalfValue(*current.node.leftNode.hash, 0)
//			} else {
//				totalHash.SetHalfValue(*current.node.nodeLeftHashOrRemainingKey, 0)
//			}
//			if current.node.rightNode != nil {
//				totalHash.SetHalfValue(*current.node.rightNode.hash, 1)
//			} else {
//				totalHash.SetHalfValue(*current.node.nodeRightHashOrValueHash, 1)
//			}
//
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//			current.node.hash = hashObj
//
//			if !noSave {
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//			}
//			stack = stack[:len(stack)-1] // 出栈
//		}
//	}
//}

// =========== op3 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	// 内联叶子节点处理，减少函数调用开销
//	if smtBatchRootNode.isLeaf() {
//		// 直接组合左节点和右节点的哈希值
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(
//			utils.ConcatArrays4ByPointers(
//				smtBatchRootNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				smtBatchRootNode.nodeRightHashOrValueHash.AsUint64Pointer(),
//			),
//			&utils.LeafCapacity,
//		)
//		smtBatchRootNode.hash = hashObj
//
//		// 仅当未设置 noSaveOnInsert 标志时，才保存数据
//		if !sdh.s.noSaveOnInsert {
//			// 复用内存缓冲区，减少内存分配
//			leafKey := utils.JoinKey(path[:level], *smtBatchRootNode.nodeLeftHashOrRemainingKey)
//			buffer := newSmtDfsHelperDataStruct(hashObj, hashValue)
//			sdh.dataChan <- buffer
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, leafKey)
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//	noSave := sdh.s.noSaveOnInsert // 缓存标志，避免每次循环都访问字段
//
//	// 遍历子节点，避免不必要的数组分配
//	for i, child := range []*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} {
//		path[level] = i
//		if child != nil {
//			// 递归处理子节点并合并哈希值
//			calculateAndSaveHashesDfs(sdh, child, path, level+1)
//			// 仅在遍历完所有子节点后，更新哈希值
//			totalHash.SetHalfValue(*child.hash, i)
//		} else {
//			// 子节点为空时，直接使用默认哈希值
//			defaultHash := smtBatchRootNode.nodeLeftHashOrRemainingKey
//			if i == 1 {
//				defaultHash = smtBatchRootNode.nodeRightHashOrValueHash
//			}
//			totalHash.SetHalfValue(*defaultHash, i)
//		}
//	}
//
//	// 一次性计算当前节点的总哈希值
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	smtBatchRootNode.hash = hashObj
//
//	// 如果 noSaveOnInsert 标志未设置，则保存数据
//	if !noSave {
//		sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//	}
//}

// =========== op4 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	// Inline leaf node processing to reduce function call overhead
//	if smtBatchRootNode.isLeaf() {
//		// Directly combine the left and right hash for the leaf
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(
//			utils.ConcatArrays4ByPointers(
//				smtBatchRootNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				smtBatchRootNode.nodeRightHashOrValueHash.AsUint64Pointer(),
//			),
//			&utils.LeafCapacity,
//		)
//		smtBatchRootNode.hash = hashObj
//
//		// Only store if not set to skip saving
//		if !sdh.s.noSaveOnInsert {
//			// Reuse memory buffer, write once
//			leafKey := utils.JoinKey(path[:level], *smtBatchRootNode.nodeLeftHashOrRemainingKey)
//			buffer := newSmtDfsHelperDataStruct(hashObj, hashValue)
//			sdh.dataChan <- buffer
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, leafKey)
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//	noSave := sdh.s.noSaveOnInsert // Cache flag to avoid frequent access
//
//	// Iterate directly over child nodes to minimize allocation of arrays
//	for i, child := range []*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} {
//		path[level] = i
//		if child != nil {
//			// Recursively process child and accumulate hash
//			calculateAndSaveHashesDfs(sdh, child, path, level+1)
//			// Accumulate the hash from child node
//			totalHash.SetHalfValue(*child.hash, i)
//		} else {
//			// Handle missing child by using the default hash directly
//			defaultHash := smtBatchRootNode.nodeLeftHashOrRemainingKey
//			if i == 1 {
//				defaultHash = smtBatchRootNode.nodeRightHashOrValueHash
//			}
//			totalHash.SetHalfValue(*defaultHash, i)
//		}
//	}
//
//	// Compute the final hash for the current node
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	smtBatchRootNode.hash = hashObj
//
//	// Avoid redundant checks by using cached noSave value
//	if !noSave {
//		sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//	}
//}

// =========== op5 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	// 内联叶子节点处理，避免函数调用开销
//	if smtBatchRootNode.isLeaf() {
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(
//			utils.ConcatArrays4ByPointers(
//				smtBatchRootNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				smtBatchRootNode.nodeRightHashOrValueHash.AsUint64Pointer(),
//			),
//			&utils.LeafCapacity,
//		)
//		smtBatchRootNode.hash = hashObj
//
//		// 避免频繁内存分配，当 noSaveOnInsert 为 true 时不存储
//		if !sdh.s.noSaveOnInsert {
//			// 将两个数据项写入缓冲区一次性处理，减少内存分配
//			buffer := newSmtDfsHelperDataStruct(hashObj, hashValue)
//			// 通过直接修改 path 来减少内存分配
//			leafKey := utils.JoinKey(path[:level], *smtBatchRootNode.nodeLeftHashOrRemainingKey)
//			sdh.dataChan <- buffer
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, leafKey)
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//	noSave := sdh.s.noSaveOnInsert // 缓存标志，避免每次循环都访问字段
//
//	// 直接遍历左右子节点，避免创建额外的数组
//	for i, child := range []*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} {
//		path[level] = i
//		if child != nil {
//			// 递归调用并合并哈希值
//			calculateAndSaveHashesDfs(sdh, child, path, level+1)
//			totalHash.SetHalfValue(*child.hash, i)
//		} else {
//			// 如果子节点为空，使用默认哈希值
//			defaultHash := smtBatchRootNode.nodeLeftHashOrRemainingKey
//			if i == 1 {
//				defaultHash = smtBatchRootNode.nodeRightHashOrValueHash
//			}
//			totalHash.SetHalfValue(*defaultHash, i)
//		}
//	}
//
//	// 计算并存储总哈希值
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	smtBatchRootNode.hash = hashObj
//
//	// 避免重复判断，当 noSaveOnInsert 为 true 时不进行存储
//	if !noSave {
//		sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//	}
//}

// =========== op6 ================

func calculateAndSaveHashesDfs(
	sdh *smtDfsHelper,
	smtBatchRootNode *smtBatchNode,
	path []int,
	level int,
) {
	const maxConcurrencyFactor = 2
	dataChan := sdh.dataChan
	noSave := sdh.s.noSaveOnInsert

	// Handle leaf node inline
	if smtBatchRootNode.isLeaf() {
		hashObj, hashValue := utils.HashKeyAndValueByPointers(
			utils.ConcatArrays4ByPointers(
				smtBatchRootNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
				smtBatchRootNode.nodeRightHashOrValueHash.AsUint64Pointer(),
			),
			&utils.LeafCapacity,
		)
		smtBatchRootNode.hash = hashObj

		if !noSave && len(dataChan) < cap(dataChan) {
			buffer1 := newSmtDfsHelperDataStruct(hashObj, hashValue)
			buffer2 := newSmtDfsHelperDataStruct(hashObj, utils.JoinKey(path[:level], *smtBatchRootNode.nodeLeftHashOrRemainingKey))
			select {
			case dataChan <- buffer1:
				dataChan <- buffer2
			default:
			}
		}
		return
	}

	// Initialize path pool and semaphore
	var pathPool = sync.Pool{
		New: func() interface{} {
			return make([]int, 0, cap(path)) // Use original capacity
		},
	}
	maxConcurrency := runtime.NumCPU() * maxConcurrencyFactor
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var totalHash utils.NodeValue8

	// Process children
	children := [2]*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode}
	for i, child := range children {
		if child == nil {
			defaultHash := smtBatchRootNode.nodeLeftHashOrRemainingKey
			if i == 1 {
				defaultHash = smtBatchRootNode.nodeRightHashOrValueHash
			}
			totalHash.SetHalfValue(*defaultHash, i)
			continue
		}

		// Ensure path has enough length for the new level
		if level >= len(path) {
			path = append(path, 0) // Grow path if needed
		}
		path[level] = i

		if level < 3 { // Serial processing for shallow levels
			calculateAndSaveHashesDfs(sdh, child, path, level+1)
			totalHash.SetHalfValue(*child.hash, i)
		} else { // Parallel processing for deeper levels
			wg.Add(1)
			sem <- struct{}{}
			childPath := append(pathPool.Get().([]int), path[:level+1]...)

			go func(c *smtBatchNode, p []int) {
				defer func() {
					<-sem
					pathPool.Put(p[:0])
					wg.Done()
				}()
				calculateAndSaveHashesDfs(sdh, c, p, level+1)
			}(child, childPath)
		}
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Update totalHash with child hashes after all are computed
	for i, child := range children {
		if child != nil && level >= 3 {
			totalHash.SetHalfValue(*child.hash, i)
		}
	}

	// Compute and set the current node's hash
	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
	smtBatchRootNode.hash = hashObj

	if !noSave && len(dataChan) < cap(dataChan) {
		dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
	}
}

// =========== op7 ================

//type stackFrame struct {
//	node  *smtBatchNode
//	level int
//	state uint8 // 0: 未处理, 1: 左子树处理完, 2: 两边处理完
//}
//
//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	stack := make([]stackFrame, 0, 1024)
//	stack = append(stack, stackFrame{node: smtBatchRootNode, level: level})
//	var totalHash utils.NodeValue8
//	var wg sync.WaitGroup
//	var bufferWg sync.WaitGroup
//	maxGoroutineLevel := runtime.NumCPU() / 2
//	if maxGoroutineLevel < 2 {
//		maxGoroutineLevel = 2
//	}
//	const bufferCapacity = 256
//
//	dataBuffer := make([]*smtDfsHelperDataStruct, 0, bufferCapacity)
//
//	flushBuffer := func(buffer []*smtDfsHelperDataStruct) {
//		defer bufferWg.Done()
//		for _, data := range buffer {
//			select {
//			case sdh.dataChan <- data:
//			default:
//				return
//			}
//		}
//	}
//
//	for len(stack) > 0 {
//		currIdx := len(stack) - 1
//		frame := &stack[currIdx]
//		node := frame.node
//		currentLevel := frame.level
//
//		if node.isLeaf() {
//			leftHash := node.nodeLeftHashOrRemainingKey.AsUint64Pointer()
//			rightHash := node.nodeRightHashOrValueHash.AsUint64Pointer()
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(
//				utils.ConcatArrays4ByPointers(leftHash, rightHash),
//				&utils.LeafCapacity,
//			)
//			node.hash = hashObj
//			if !sdh.s.noSaveOnInsert {
//				dataBuffer = append(dataBuffer, newSmtDfsHelperDataStruct(hashObj, hashValue))
//				nodeKey := utils.JoinKey(path[:currentLevel], *node.nodeLeftHashOrRemainingKey)
//				dataBuffer = append(dataBuffer, newSmtDfsHelperDataStruct(hashObj, nodeKey))
//
//				if len(dataBuffer) >= bufferCapacity {
//					bufferCopy := make([]*smtDfsHelperDataStruct, len(dataBuffer))
//					copy(bufferCopy, dataBuffer)
//					bufferWg.Add(1)
//					go flushBuffer(bufferCopy)
//					dataBuffer = dataBuffer[:0]
//				}
//			}
//			stack = stack[:currIdx]
//			continue
//		}
//
//		switch frame.state {
//		case 0:
//			if node.leftNode != nil {
//				path[currentLevel] = 0
//				processChild(sdh, node.leftNode, path, currentLevel, &stack, &wg, maxGoroutineLevel)
//				frame.state = 1
//				continue
//			}
//			fallthrough
//		case 1:
//			if node.rightNode != nil {
//				path[currentLevel] = 1
//				processChild(sdh, node.rightNode, path, currentLevel, &stack, &wg, maxGoroutineLevel)
//				frame.state = 2
//				continue
//			}
//			fallthrough
//		case 2:
//			if currentLevel < maxGoroutineLevel {
//				wg.Wait()
//			}
//			if node.leftNode != nil {
//				totalHash.SetHalfValue(*node.leftNode.hash, 0)
//			} else {
//				totalHash.SetHalfValue(*node.nodeLeftHashOrRemainingKey, 0)
//			}
//			if node.rightNode != nil {
//				totalHash.SetHalfValue(*node.rightNode.hash, 1)
//			} else {
//				totalHash.SetHalfValue(*node.nodeRightHashOrValueHash, 1)
//			}
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//			if !sdh.s.noSaveOnInsert {
//				dataBuffer = append(dataBuffer, newSmtDfsHelperDataStruct(hashObj, hashValue))
//
//				if len(dataBuffer) >= bufferCapacity {
//					bufferCopy := make([]*smtDfsHelperDataStruct, len(dataBuffer))
//					copy(bufferCopy, dataBuffer)
//					bufferWg.Add(1)
//					go flushBuffer(bufferCopy)
//					dataBuffer = dataBuffer[:0]
//				}
//			}
//			node.hash = hashObj
//			stack = stack[:currIdx]
//		}
//	}
//
//	if len(dataBuffer) > 0 && !sdh.s.noSaveOnInsert {
//		bufferCopy := make([]*smtDfsHelperDataStruct, len(dataBuffer))
//		copy(bufferCopy, dataBuffer)
//		bufferWg.Add(1)
//		go flushBuffer(bufferCopy)
//	}
//	bufferWg.Wait()
//}
//
//func processChild(
//	sdh *smtDfsHelper,
//	child *smtBatchNode,
//	path []int,
//	level int,
//	stack *[]stackFrame,
//	wg *sync.WaitGroup,
//	maxGoroutineLevel int,
//) {
//	nextLevel := level + 1
//
//	if level < maxGoroutineLevel {
//		pathCopy := make([]int, len(path))
//		copy(pathCopy, path)
//		wg.Add(1)
//		go func(n *smtBatchNode, p []int, l int) {
//			defer wg.Done()
//			calculateAndSaveHashesDfs(sdh, n, p, l)
//		}(child, pathCopy, nextLevel)
//	} else {
//		*stack = append(*stack, stackFrame{node: child, level: nextLevel})
//	}
//}

// =========== op8 ================

//type stackFrame struct {
//	node  *smtBatchNode
//	level int
//	state uint8 // 0: 未处理, 1: 左子树处理完, 2: 两边处理完
//}
//
//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	stack := make([]stackFrame, 0, 1024)
//	stack = append(stack, stackFrame{node: smtBatchRootNode, level: level})
//	var totalHash utils.NodeValue8
//	var wg sync.WaitGroup
//	var bufferWg sync.WaitGroup
//	maxGoroutineLevel := runtime.NumCPU() / 2
//	if maxGoroutineLevel < 2 {
//		maxGoroutineLevel = 2
//	}
//	const bufferCapacity = 256
//
//	dataBuffer := make([]*smtDfsHelperDataStruct, 0, bufferCapacity)
//
//	flushBuffer := func(buffer []*smtDfsHelperDataStruct) {
//		defer bufferWg.Done()
//		for _, data := range buffer {
//			select {
//			case sdh.dataChan <- data:
//			default:
//				return
//			}
//		}
//	}
//
//	for len(stack) > 0 {
//		currIdx := len(stack) - 1
//		frame := &stack[currIdx]
//		node := frame.node
//		currentLevel := frame.level
//
//		if node.isLeaf() {
//			leftHash := node.nodeLeftHashOrRemainingKey.AsUint64Pointer()
//			rightHash := node.nodeRightHashOrValueHash.AsUint64Pointer()
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(
//				utils.ConcatArrays4ByPointers(leftHash, rightHash),
//				&utils.LeafCapacity,
//			)
//			node.hash = hashObj
//			if !sdh.s.noSaveOnInsert {
//				dataBuffer = append(dataBuffer, newSmtDfsHelperDataStruct(hashObj, hashValue))
//				nodeKey := utils.JoinKey(path[:currentLevel], *node.nodeLeftHashOrRemainingKey)
//				dataBuffer = append(dataBuffer, newSmtDfsHelperDataStruct(hashObj, nodeKey))
//
//				if len(dataBuffer) >= bufferCapacity {
//					bufferCopy := make([]*smtDfsHelperDataStruct, len(dataBuffer))
//					copy(bufferCopy, dataBuffer)
//					bufferWg.Add(1)
//					go flushBuffer(bufferCopy)
//					dataBuffer = dataBuffer[:0]
//				}
//			}
//			stack = stack[:currIdx]
//			continue
//		}
//
//		switch frame.state {
//		case 0:
//			if node.leftNode != nil {
//				path[currentLevel] = 0
//				processChild(sdh, node.leftNode, path, currentLevel, &stack, &wg, maxGoroutineLevel)
//				frame.state = 1
//				continue
//			}
//			fallthrough
//		case 1:
//			if node.rightNode != nil {
//				path[currentLevel] = 1
//				processChild(sdh, node.rightNode, path, currentLevel, &stack, &wg, maxGoroutineLevel)
//				frame.state = 2
//				continue
//			}
//			fallthrough
//		case 2:
//			if currentLevel < maxGoroutineLevel {
//				wg.Wait()
//			}
//			if node.leftNode != nil {
//				totalHash.SetHalfValue(*node.leftNode.hash, 0)
//			} else {
//				totalHash.SetHalfValue(*node.nodeLeftHashOrRemainingKey, 0)
//			}
//			if node.rightNode != nil {
//				totalHash.SetHalfValue(*node.rightNode.hash, 1)
//			} else {
//				totalHash.SetHalfValue(*node.nodeRightHashOrValueHash, 1)
//			}
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//			if !sdh.s.noSaveOnInsert {
//				dataBuffer = append(dataBuffer, newSmtDfsHelperDataStruct(hashObj, hashValue))
//
//				if len(dataBuffer) >= bufferCapacity {
//					bufferCopy := make([]*smtDfsHelperDataStruct, len(dataBuffer))
//					copy(bufferCopy, dataBuffer)
//					bufferWg.Add(1)
//					go flushBuffer(bufferCopy)
//					dataBuffer = dataBuffer[:0]
//				}
//			}
//			node.hash = hashObj
//			stack = stack[:currIdx]
//		}
//	}
//
//	if len(dataBuffer) > 0 && !sdh.s.noSaveOnInsert {
//		bufferCopy := make([]*smtDfsHelperDataStruct, len(dataBuffer))
//		copy(bufferCopy, dataBuffer)
//		bufferWg.Add(1)
//		go flushBuffer(bufferCopy)
//	}
//	bufferWg.Wait()
//}
//
//func processChild(
//	sdh *smtDfsHelper,
//	child *smtBatchNode,
//	path []int,
//	level int,
//	stack *[]stackFrame, // Now uses the package-level stackFrame type
//	wg *sync.WaitGroup,
//	maxGoroutineLevel int,
//) {
//	nextLevel := level + 1
//
//	pathCopy := make([]int, len(path))
//	copy(pathCopy, path)
//
//	if level < maxGoroutineLevel {
//		wg.Add(1)
//		go func(n *smtBatchNode, p []int, l int) {
//			defer wg.Done()
//			calculateAndSaveHashesDfs(sdh, n, p, l)
//		}(child, pathCopy, nextLevel)
//	} else {
//		*stack = append(*stack, stackFrame{node: child, level: nextLevel})
//	}
//}

// =========== op9 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	type stackFrame struct {
//		node  *smtBatchNode
//		level int
//		state uint8 // 0: 未处理, 1: 左子树处理完, 2: 两边处理完
//	}
//
//	stack := make([]stackFrame, 0, 2048)
//	stack = append(stack, stackFrame{node: smtBatchRootNode, level: level})
//	var totalHash utils.NodeValue8
//	var wg sync.WaitGroup
//
//	for len(stack) > 0 {
//		currIdx := len(stack) - 1
//		frame := &stack[currIdx]
//		node := frame.node
//		currentLevel := frame.level
//
//		if node.isLeaf() {
//			leftHash := node.nodeLeftHashOrRemainingKey.AsUint64Pointer()
//			rightHash := node.nodeRightHashOrValueHash.AsUint64Pointer()
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(
//				utils.ConcatArrays4ByPointers(leftHash, rightHash),
//				&utils.LeafCapacity,
//			)
//			node.hash = hashObj
//			if !sdh.s.noSaveOnInsert {
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//				nodeKey := utils.JoinKey(path[:currentLevel], *node.nodeLeftHashOrRemainingKey)
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, nodeKey)
//			}
//			stack = stack[:currIdx]
//			continue
//		}
//
//		switch frame.state {
//		case 0: // 处理左子树
//			if node.leftNode != nil {
//				path[currentLevel] = 0
//				if currentLevel < 4 {
//					wg.Add(1)
//					go func(n *smtBatchNode, p []int, l int) {
//						defer wg.Done()
//						calculateAndSaveHashesDfs(sdh, n, p, l)
//					}(node.leftNode, append([]int{}, path...), currentLevel+1)
//				} else {
//					stack = append(stack, stackFrame{node: node.leftNode, level: currentLevel + 1})
//				}
//				frame.state = 1
//				continue
//			}
//			fallthrough
//		case 1: // 处理右子树
//			if node.rightNode != nil {
//				path[currentLevel] = 1
//				if currentLevel < 4 {
//					wg.Add(1)
//					go func(n *smtBatchNode, p []int, l int) {
//						defer wg.Done()
//						calculateAndSaveHashesDfs(sdh, n, p, l)
//					}(node.rightNode, append([]int{}, path...), currentLevel+1)
//				} else {
//					stack = append(stack, stackFrame{node: node.rightNode, level: currentLevel + 1})
//				}
//				frame.state = 2
//				continue
//			}
//			fallthrough
//		case 2: // 计算哈希
//			if currentLevel < 4 {
//				wg.Wait()
//			}
//			if node.leftNode != nil {
//				totalHash.SetHalfValue(*node.leftNode.hash, 0)
//			} else {
//				totalHash.SetHalfValue(*node.nodeLeftHashOrRemainingKey, 0)
//			}
//			if node.rightNode != nil {
//				totalHash.SetHalfValue(*node.rightNode.hash, 1)
//			} else {
//				totalHash.SetHalfValue(*node.nodeRightHashOrValueHash, 1)
//			}
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//			if !sdh.s.noSaveOnInsert {
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//			}
//			node.hash = hashObj
//			stack = stack[:currIdx]
//		}
//	}
//}

// =========== op10 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	type stackFrame struct {
//		node           *smtBatchNode
//		level          int
//		leftProcessed  bool
//		rightProcessed bool
//	}
//
//	// 预分配栈容量为1024，减少动态扩容
//	stack := make([]stackFrame, 0, 1024)
//	stack = append(stack, stackFrame{node: smtBatchRootNode, level: level})
//	var totalHash utils.NodeValue8
//	var wg sync.WaitGroup
//
//	for len(stack) > 0 {
//		// 使用索引访问栈顶元素
//		currIdx := len(stack) - 1
//		frame := &stack[currIdx]
//		node := frame.node
//		currentLevel := frame.level
//
//		if node.isLeaf() {
//			leftHash := node.nodeLeftHashOrRemainingKey.AsUint64Pointer()
//			rightHash := node.nodeRightHashOrValueHash.AsUint64Pointer()
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(
//				utils.ConcatArrays4ByPointers(leftHash, rightHash),
//				&utils.LeafCapacity,
//			)
//			node.hash = hashObj
//			if !sdh.s.noSaveOnInsert {
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//				nodeKey := utils.JoinKey(path[:currentLevel], *node.nodeLeftHashOrRemainingKey)
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, nodeKey)
//			}
//			stack = stack[:currIdx]
//			continue
//		}
//
//		// 处理左子树
//		if node.leftNode != nil && !frame.leftProcessed {
//			path[currentLevel] = 0
//			if currentLevel < 4 {
//				wg.Add(1)
//				go func(n *smtBatchNode, p []int, l int) {
//					defer wg.Done()
//					calculateAndSaveHashesDfs(sdh, n, p, l)
//				}(node.leftNode, append([]int{}, path...), currentLevel+1)
//			} else {
//				stack = append(stack, stackFrame{node: node.leftNode, level: currentLevel + 1})
//			}
//			frame.leftProcessed = true
//			continue
//		}
//
//		// 处理右子树
//		if node.rightNode != nil && !frame.rightProcessed {
//			path[currentLevel] = 1
//			if currentLevel < 4 {
//				wg.Add(1)
//				go func(n *smtBatchNode, p []int, l int) {
//					defer wg.Done()
//					calculateAndSaveHashesDfs(sdh, n, p, l)
//				}(node.rightNode, append([]int{}, path...), currentLevel+1)
//			} else {
//				stack = append(stack, stackFrame{node: node.rightNode, level: currentLevel + 1})
//			}
//			frame.rightProcessed = true
//			continue
//		}
//
//		if currentLevel < 4 {
//			wg.Wait()
//		}
//
//		if node.leftNode != nil {
//			totalHash.SetHalfValue(*node.leftNode.hash, 0)
//		} else {
//			totalHash.SetHalfValue(*node.nodeLeftHashOrRemainingKey, 0)
//		}
//		if node.rightNode != nil {
//			totalHash.SetHalfValue(*node.rightNode.hash, 1)
//		} else {
//			totalHash.SetHalfValue(*node.nodeRightHashOrValueHash, 1)
//		}
//
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//		if !sdh.s.noSaveOnInsert {
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//		}
//		node.hash = hashObj
//		stack = stack[:currIdx]
//	}
//}

// =========== op11 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	type stackFrame struct {
//		node           *smtBatchNode
//		level          int
//		leftProcessed  bool
//		rightProcessed bool
//	}
//
//	// Use a stack for DFS traversal
//	stack := []stackFrame{{node: smtBatchRootNode, level: level}}
//	var totalHash utils.NodeValue8
//	var wg sync.WaitGroup
//
//	// To reduce slice copying, modify the stack traversal
//	for len(stack) > 0 {
//		frame := &stack[len(stack)-1]
//		node := frame.node
//		currentLevel := frame.level
//
//		if node.isLeaf() {
//			// Handle leaf node
//			leftHash := node.nodeLeftHashOrRemainingKey.AsUint64Pointer()
//			rightHash := node.nodeRightHashOrValueHash.AsUint64Pointer()
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(utils.ConcatArrays4ByPointers(leftHash, rightHash), &utils.LeafCapacity)
//			node.hash = hashObj
//			if !sdh.s.noSaveOnInsert {
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//				nodeKey := utils.JoinKey(path[:currentLevel], *node.nodeLeftHashOrRemainingKey)
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, nodeKey)
//			}
//			stack = stack[:len(stack)-1]
//			continue
//		}
//
//		// Handle left subtree processing
//		if node.leftNode != nil && !frame.leftProcessed {
//			path[currentLevel] = 0
//			if currentLevel < 4 { // Limit Goroutine creation
//				wg.Add(1)
//				go func(n *smtBatchNode, p []int, l int) {
//					defer wg.Done()
//					calculateAndSaveHashesDfs(sdh, n, p, l)
//				}(node.leftNode, append([]int{}, path...), currentLevel+1)
//			} else {
//				stack = append(stack, stackFrame{node: node.leftNode, level: currentLevel + 1})
//			}
//			frame.leftProcessed = true
//			continue
//		}
//
//		// Handle right subtree processing
//		if node.rightNode != nil && !frame.rightProcessed {
//			path[currentLevel] = 1
//			if currentLevel < 4 { // Limit Goroutine creation
//				wg.Add(1)
//				go func(n *smtBatchNode, p []int, l int) {
//					defer wg.Done()
//					calculateAndSaveHashesDfs(sdh, n, p, l)
//				}(node.rightNode, append([]int{}, path...), currentLevel+1)
//			} else {
//				stack = append(stack, stackFrame{node: node.rightNode, level: currentLevel + 1})
//			}
//			frame.rightProcessed = true
//			continue
//		}
//
//		// If we've created Goroutines, wait for them to finish
//		if currentLevel < 4 {
//			wg.Wait()
//		}
//
//		// Both left and right child processed, calculate current node's hash
//		if node.leftNode != nil {
//			totalHash.SetHalfValue(*node.leftNode.hash, 0)
//		} else {
//			totalHash.SetHalfValue(*node.nodeLeftHashOrRemainingKey, 0)
//		}
//
//		if node.rightNode != nil {
//			totalHash.SetHalfValue(*node.rightNode.hash, 1)
//		} else {
//			totalHash.SetHalfValue(*node.nodeRightHashOrValueHash, 1)
//		}
//
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//		if !sdh.s.noSaveOnInsert {
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//		}
//		node.hash = hashObj
//
//		stack = stack[:len(stack)-1]
//	}
//}

// =========== op12 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	type stackFrame struct {
//		node           *smtBatchNode
//		level          int
//		leftProcessed  bool
//		rightProcessed bool
//	}
//
//	stack := []stackFrame{{node: smtBatchRootNode, level: level}}
//	var totalHash utils.NodeValue8
//	var wg sync.WaitGroup
//
//	for len(stack) > 0 {
//		frame := &stack[len(stack)-1] // 获取栈顶元素的引用
//		node := frame.node
//		currentLevel := frame.level
//
//		if node.isLeaf() {
//			// 处理叶子节点
//			leftHash := node.nodeLeftHashOrRemainingKey.AsUint64Pointer()
//			rightHash := node.nodeRightHashOrValueHash.AsUint64Pointer()
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(utils.ConcatArrays4ByPointers(leftHash, rightHash), &utils.LeafCapacity)
//			node.hash = hashObj
//			if !sdh.s.noSaveOnInsert {
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//				nodeKey := utils.JoinKey(path[:currentLevel], *node.nodeLeftHashOrRemainingKey)
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, nodeKey)
//			}
//			// 弹出栈顶元素
//			stack = stack[:len(stack)-1]
//			continue
//		}
//
//		// 控制 Goroutine 的创建：只在特定深度启动 Goroutine
//		useGoroutine := currentLevel < 4 // 例如，在前 4 层启动 Goroutine
//
//		// 处理左子树
//		if node.leftNode != nil && !frame.leftProcessed {
//			path[currentLevel] = 0
//			if useGoroutine {
//				wg.Add(1)
//				go func(n *smtBatchNode, p []int, l int) {
//					defer wg.Done()
//					calculateAndSaveHashesDfs(sdh, n, p, l)
//				}(node.leftNode, append([]int{}, path...), currentLevel+1)
//			} else {
//				stack = append(stack, stackFrame{node: node.leftNode, level: currentLevel + 1})
//			}
//			frame.leftProcessed = true
//			continue
//		}
//
//		// 处理右子树
//		if node.rightNode != nil && !frame.rightProcessed {
//			path[currentLevel] = 1
//			if useGoroutine {
//				wg.Add(1)
//				go func(n *smtBatchNode, p []int, l int) {
//					defer wg.Done()
//					calculateAndSaveHashesDfs(sdh, n, p, l)
//				}(node.rightNode, append([]int{}, path...), currentLevel+1)
//			} else {
//				stack = append(stack, stackFrame{node: node.rightNode, level: currentLevel + 1})
//			}
//			frame.rightProcessed = true
//			continue
//		}
//
//		// 等待 Goroutine 完成（如果有）
//		if useGoroutine {
//			wg.Wait()
//		}
//
//		// 左右子树都处理完毕，计算当前节点的哈希值
//		if node.leftNode != nil {
//			totalHash.SetHalfValue(*node.leftNode.hash, 0)
//		} else {
//			totalHash.SetHalfValue(*node.nodeLeftHashOrRemainingKey, 0)
//		}
//
//		if node.rightNode != nil {
//			totalHash.SetHalfValue(*node.rightNode.hash, 1)
//		} else {
//			totalHash.SetHalfValue(*node.nodeRightHashOrValueHash, 1)
//		}
//
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//		if !sdh.s.noSaveOnInsert {
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//		}
//		node.hash = hashObj
//
//		// 弹出栈顶元素
//		stack = stack[:len(stack)-1]
//	}
//}

// =========== op13 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	type stackFrame struct {
//		node           *smtBatchNode
//		level          int
//		leftProcessed  bool
//		rightProcessed bool
//	}
//
//	stack := []stackFrame{{node: smtBatchRootNode, level: level}}
//	var totalHash utils.NodeValue8
//	var wg sync.WaitGroup
//
//	for len(stack) > 0 {
//		frame := &stack[len(stack)-1] // 获取栈顶元素的引用
//		node := frame.node
//		currentLevel := frame.level
//
//		if node.isLeaf() {
//			// 处理叶子节点
//			leftHash := node.nodeLeftHashOrRemainingKey.AsUint64Pointer()
//			rightHash := node.nodeRightHashOrValueHash.AsUint64Pointer()
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(utils.ConcatArrays4ByPointers(leftHash, rightHash), &utils.LeafCapacity)
//			node.hash = hashObj
//			if !sdh.s.noSaveOnInsert {
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//				nodeKey := utils.JoinKey(path[:currentLevel], *node.nodeLeftHashOrRemainingKey)
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, nodeKey)
//			}
//			// 弹出栈顶元素
//			stack = stack[:len(stack)-1]
//			continue
//		}
//
//		// 处理左子树
//		if node.leftNode != nil && !frame.leftProcessed {
//			path[currentLevel] = 0
//			wg.Add(1)
//			go func(n *smtBatchNode, p []int, l int) {
//				defer wg.Done()
//				calculateAndSaveHashesDfs(sdh, n, p, l)
//			}(node.leftNode, append([]int{}, path...), currentLevel+1)
//			frame.leftProcessed = true
//			continue
//		}
//
//		// 处理右子树
//		if node.rightNode != nil && !frame.rightProcessed {
//			path[currentLevel] = 1
//			wg.Add(1)
//			go func(n *smtBatchNode, p []int, l int) {
//				defer wg.Done()
//				calculateAndSaveHashesDfs(sdh, n, p, l)
//			}(node.rightNode, append([]int{}, path...), currentLevel+1)
//			frame.rightProcessed = true
//			continue
//		}
//
//		// 等待左右子树的 Goroutine 完成
//		wg.Wait()
//
//		// 左右子树都处理完毕，计算当前节点的哈希值
//		if node.leftNode != nil {
//			totalHash.SetHalfValue(*node.leftNode.hash, 0)
//		} else {
//			totalHash.SetHalfValue(*node.nodeLeftHashOrRemainingKey, 0)
//		}
//
//		if node.rightNode != nil {
//			totalHash.SetHalfValue(*node.rightNode.hash, 1)
//		} else {
//			totalHash.SetHalfValue(*node.nodeRightHashOrValueHash, 1)
//		}
//
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//		if !sdh.s.noSaveOnInsert {
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//		}
//		node.hash = hashObj
//
//		// 弹出栈顶元素
//		stack = stack[:len(stack)-1]
//	}
//}

// =========== op14 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	type stackFrame struct {
//		node           *smtBatchNode
//		level          int
//		leftProcessed  bool
//		rightProcessed bool
//	}
//
//	stack := []stackFrame{{node: smtBatchRootNode, level: level}}
//	var totalHash utils.NodeValue8
//
//	for len(stack) > 0 {
//		frame := &stack[len(stack)-1] // 获取栈顶元素的引用
//		node := frame.node
//		currentLevel := frame.level
//
//		if node.isLeaf() {
//			// 处理叶子节点
//			leftHash := node.nodeLeftHashOrRemainingKey.AsUint64Pointer()
//			rightHash := node.nodeRightHashOrValueHash.AsUint64Pointer()
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(utils.ConcatArrays4ByPointers(leftHash, rightHash), &utils.LeafCapacity)
//			node.hash = hashObj
//			if !sdh.s.noSaveOnInsert {
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//				nodeKey := utils.JoinKey(path[:currentLevel], *node.nodeLeftHashOrRemainingKey)
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, nodeKey)
//			}
//			// 弹出栈顶元素
//			stack = stack[:len(stack)-1]
//			continue
//		}
//
//		// 处理左子树
//		if node.leftNode != nil && !frame.leftProcessed {
//			path[currentLevel] = 0
//			stack = append(stack, stackFrame{node: node.leftNode, level: currentLevel + 1})
//			frame.leftProcessed = true
//			continue
//		}
//
//		// 处理右子树
//		if node.rightNode != nil && !frame.rightProcessed {
//			path[currentLevel] = 1
//			stack = append(stack, stackFrame{node: node.rightNode, level: currentLevel + 1})
//			frame.rightProcessed = true
//			continue
//		}
//
//		// 左右子树都处理完毕，计算当前节点的哈希值
//		if node.leftNode != nil {
//			totalHash.SetHalfValue(*node.leftNode.hash, 0)
//		} else {
//			totalHash.SetHalfValue(*node.nodeLeftHashOrRemainingKey, 0)
//		}
//
//		if node.rightNode != nil {
//			totalHash.SetHalfValue(*node.rightNode.hash, 1)
//		} else {
//			totalHash.SetHalfValue(*node.nodeRightHashOrValueHash, 1)
//		}
//
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//		if !sdh.s.noSaveOnInsert {
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//		}
//		node.hash = hashObj
//
//		// 弹出栈顶元素
//		stack = stack[:len(stack)-1]
//	}
//}

// =========== op15 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	type stackFrame struct {
//		node  *smtBatchNode
//		path  []int
//		level int
//		// 标记是否已经处理过左子树或右子树
//		leftProcessed  bool
//		rightProcessed bool
//		// 用于保存中间计算的哈希值
//		leftHash  *utils.NodeValue8
//		rightHash *utils.NodeValue8
//	}
//
//	stack := []stackFrame{{node: smtBatchRootNode, path: path, level: level}}
//
//	var totalHash utils.NodeValue8
//
//	for len(stack) > 0 {
//		frame := &stack[len(stack)-1] // 获取栈顶元素的引用
//		node := frame.node
//		path := frame.path
//		level := frame.level
//
//		if node.isLeaf() {
//			// 处理叶子节点
//			hashObj, hashValue := utils.HashKeyAndValueByPointers(
//				utils.ConcatArrays4ByPointers(node.nodeLeftHashOrRemainingKey.AsUint64Pointer(), node.nodeRightHashOrValueHash.AsUint64Pointer()),
//				&utils.LeafCapacity,
//			)
//			node.hash = hashObj
//			if !sdh.s.noSaveOnInsert {
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//
//				nodeKey := utils.JoinKey(path[:level], *node.nodeLeftHashOrRemainingKey)
//				sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, nodeKey)
//			}
//			// 弹出栈顶元素
//			stack = stack[:len(stack)-1]
//			continue
//		}
//
//		// 处理左子树
//		if node.leftNode != nil && !frame.leftProcessed {
//			newPath := make([]int, len(path))
//			copy(newPath, path)
//			newPath[level] = 0
//			stack = append(stack, stackFrame{node: node.leftNode, path: newPath, level: level + 1})
//			frame.leftProcessed = true
//			continue
//		}
//
//		// 处理右子树
//		if node.rightNode != nil && !frame.rightProcessed {
//			newPath := make([]int, len(path))
//			copy(newPath, path)
//			newPath[level] = 1
//			stack = append(stack, stackFrame{node: node.rightNode, path: newPath, level: level + 1})
//			frame.rightProcessed = true
//			continue
//		}
//
//		// 左右子树都处理完毕，计算当前节点的哈希值
//		if node.leftNode != nil {
//			totalHash.SetHalfValue(*node.leftNode.hash, 0)
//		} else {
//			totalHash.SetHalfValue(*node.nodeLeftHashOrRemainingKey, 0)
//		}
//
//		if node.rightNode != nil {
//			totalHash.SetHalfValue(*node.rightNode.hash, 1)
//		} else {
//			totalHash.SetHalfValue(*node.nodeRightHashOrValueHash, 1)
//		}
//
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//		if !sdh.s.noSaveOnInsert {
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//		}
//		node.hash = hashObj
//
//		// 弹出栈顶元素
//		stack = stack[:len(stack)-1]
//	}
//}

// =========== op16  good ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	noSave := sdh.s.noSaveOnInsert
//	dataChan := sdh.dataChan
//
//	// 使用 sync.Pool 复用 path 切片
//	var pathPool = sync.Pool{
//		New: func() interface{} {
//			return make([]int, cap(path)) // 使用 cap 避免长度不足
//		},
//	}
//
//	// 控制最大并发 goroutine 数量，动态适应 CPU
//	maxConcurrency := runtime.GOMAXPROCS(0) * 2 // 动态调整并发数
//	var wg sync.WaitGroup
//	sem := make(chan struct{}, maxConcurrency)
//
//	// 内联叶子节点处理，减少嵌套
//	if smtBatchRootNode.isLeaf() {
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(
//			utils.ConcatArrays4ByPointers(
//				smtBatchRootNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				smtBatchRootNode.nodeRightHashOrValueHash.AsUint64Pointer(),
//			),
//			&utils.LeafCapacity,
//		)
//		smtBatchRootNode.hash = hashObj
//
//		if !noSave {
//			buffer1 := newSmtDfsHelperDataStruct(hashObj, hashValue)
//			buffer2 := newSmtDfsHelperDataStruct(hashObj, utils.JoinKey(path[:level], *smtBatchRootNode.nodeLeftHashOrRemainingKey))
//			// 单次检查通道状态，减少开销
//			if len(dataChan) < cap(dataChan) {
//				dataChan <- buffer1
//				dataChan <- buffer2
//			}
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//
//	// 内联 processChild 逻辑，减少闭包开销
//	for i, child := range [2]*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} { // 使用数组替代切片
//		if child != nil {
//			if level < 3 { // 浅层串行处理
//				path[level] = i
//				calculateAndSaveHashesDfs(sdh, child, path, level+1)
//				totalHash.SetHalfValue(*child.hash, i)
//			} else { // 深层并行处理
//				wg.Add(1)
//				sem <- struct{}{}
//				childPath := pathPool.Get().([]int)[:len(path)] // 调整长度
//				copy(childPath, path)
//				childPath[level] = i
//				go func(c *smtBatchNode, p []int) {
//					defer func() { <-sem; pathPool.Put(p); wg.Done() }()
//					calculateAndSaveHashesDfs(sdh, c, p, level+1)
//				}(child, childPath)
//			}
//		} else {
//			defaultHash := smtBatchRootNode.nodeLeftHashOrRemainingKey
//			if i == 1 {
//				defaultHash = smtBatchRootNode.nodeRightHashOrValueHash
//			}
//			totalHash.SetHalfValue(*defaultHash, i)
//		}
//	}
//
//	// 等待所有 goroutine 完成
//	wg.Wait()
//
//	// 更新当前节点哈希，内联循环
//	if smtBatchRootNode.leftNode != nil {
//		totalHash.SetHalfValue(*smtBatchRootNode.leftNode.hash, 0)
//	}
//	if smtBatchRootNode.rightNode != nil {
//		totalHash.SetHalfValue(*smtBatchRootNode.rightNode.hash, 1)
//	}
//
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	smtBatchRootNode.hash = hashObj
//
//	if !noSave && len(dataChan) < cap(dataChan) {
//		dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//	}
//}

// =========== op17 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	noSave := sdh.s.noSaveOnInsert
//	dataChan := sdh.dataChan
//
//	// 使用 sync.Pool 复用 path 切片
//	var pathPool = sync.Pool{
//		New: func() interface{} {
//			return make([]int, cap(path)) // 使用 cap 避免长度不足
//		},
//	}
//
//	// 控制最大并发 goroutine 数量，动态适应 CPU
//	maxConcurrency := runtime.GOMAXPROCS(0) * 2 // 动态调整并发数
//	sem := make(chan struct{}, maxConcurrency)
//
//	// 内联叶子节点处理，减少嵌套
//	if smtBatchRootNode.isLeaf() {
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(
//			utils.ConcatArrays4ByPointers(
//				smtBatchRootNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				smtBatchRootNode.nodeRightHashOrValueHash.AsUint64Pointer(),
//			),
//			&utils.LeafCapacity,
//		)
//		smtBatchRootNode.hash = hashObj
//
//		if !noSave {
//			buffer1 := newSmtDfsHelperDataStruct(hashObj, hashValue)
//			buffer2 := newSmtDfsHelperDataStruct(hashObj, utils.JoinKey(path[:level], *smtBatchRootNode.nodeLeftHashOrRemainingKey))
//			// 单次检查通道状态，减少开销
//			if len(dataChan) < cap(dataChan) {
//				dataChan <- buffer1
//				dataChan <- buffer2
//			}
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//
//	// 内联 processChild 逻辑，减少闭包开销
//	for i, child := range [2]*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} { // 使用数组替代切片
//		if child != nil {
//			if level < 3 { // 浅层串行处理
//				path[level] = i
//				calculateAndSaveHashesDfs(sdh, child, path, level+1)
//				totalHash.SetHalfValue(*child.hash, i)
//			} else { // 深层并行处理
//				sem <- struct{}{}
//				childPath := pathPool.Get().([]int)[:len(path)] // 调整长度
//				copy(childPath, path)
//				childPath[level] = i
//				go func(c *smtBatchNode, p []int) {
//					defer func() { <-sem; pathPool.Put(p) }()
//					calculateAndSaveHashesDfs(sdh, c, p, level+1)
//				}(child, childPath)
//			}
//		} else {
//			defaultHash := smtBatchRootNode.nodeLeftHashOrRemainingKey
//			if i == 1 {
//				defaultHash = smtBatchRootNode.nodeRightHashOrValueHash
//			}
//			totalHash.SetHalfValue(*defaultHash, i)
//		}
//	}
//
//	// 等待所有 goroutine 完成
//	for i := 0; i < maxConcurrency; i++ {
//		sem <- struct{}{}
//	}
//	close(sem)
//
//	// 更新当前节点哈希，内联循环
//	if smtBatchRootNode.leftNode != nil {
//		totalHash.SetHalfValue(*smtBatchRootNode.leftNode.hash, 0)
//	}
//	if smtBatchRootNode.rightNode != nil {
//		totalHash.SetHalfValue(*smtBatchRootNode.rightNode.hash, 1)
//	}
//
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	smtBatchRootNode.hash = hashObj
//
//	if !noSave && len(dataChan) < cap(dataChan) {
//		dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//	}
//}

// =========== op18 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	var wg sync.WaitGroup
//	noSave := sdh.s.noSaveOnInsert
//	dataChan := sdh.dataChan
//
//	// 使用 sync.Pool 复用 path 切片
//	var pathPool = sync.Pool{
//		New: func() interface{} {
//			return make([]int, cap(path)) // 使用 cap 避免长度不足
//		},
//	}
//
//	// 控制最大并发 goroutine 数量，动态适应 CPU
//	maxConcurrency := runtime.NumCPU() * 2 // 可调整，或用 runtime.NumCPU()
//	sem := make(chan struct{}, maxConcurrency)
//
//	// 内联叶子节点处理，减少嵌套
//	if smtBatchRootNode.isLeaf() {
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(
//			utils.ConcatArrays4ByPointers(
//				smtBatchRootNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				smtBatchRootNode.nodeRightHashOrValueHash.AsUint64Pointer(),
//			),
//			&utils.LeafCapacity,
//		)
//		smtBatchRootNode.hash = hashObj
//
//		if !noSave {
//			buffer1 := newSmtDfsHelperDataStruct(hashObj, hashValue)
//			buffer2 := newSmtDfsHelperDataStruct(hashObj, utils.JoinKey(path[:level], *smtBatchRootNode.nodeLeftHashOrRemainingKey))
//			// 单次检查通道状态，减少开销
//			select {
//			case dataChan <- buffer1:
//				dataChan <- buffer2 // 如果第一个成功，第二个直接发送
//			default:
//				// 通道阻塞或关闭，退出
//			}
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//
//	// 内联 processChild 逻辑，减少闭包开销
//	for i, child := range [2]*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} { // 使用数组替代切片
//		if child != nil {
//			if level < 3 { // 浅层串行处理
//				path[level] = i
//				calculateAndSaveHashesDfs(sdh, child, path, level+1)
//				totalHash.SetHalfValue(*child.hash, i)
//			} else { // 深层并行处理
//				wg.Add(1)
//				sem <- struct{}{}
//				childPath := pathPool.Get().([]int)[:len(path)] // 调整长度
//				copy(childPath, path)
//				childPath[level] = i
//				go func(c *smtBatchNode, p []int) {
//					defer wg.Done()
//					defer func() { <-sem }()
//					defer pathPool.Put(p)
//					calculateAndSaveHashesDfs(sdh, c, p, level+1)
//				}(child, childPath)
//			}
//		} else {
//			defaultHash := smtBatchRootNode.nodeLeftHashOrRemainingKey
//			if i == 1 {
//				defaultHash = smtBatchRootNode.nodeRightHashOrValueHash
//			}
//			totalHash.SetHalfValue(*defaultHash, i)
//		}
//	}
//
//	wg.Wait()
//
//	// 更新当前节点哈希，内联循环
//	if smtBatchRootNode.leftNode != nil {
//		totalHash.SetHalfValue(*smtBatchRootNode.leftNode.hash, 0)
//	}
//	if smtBatchRootNode.rightNode != nil {
//		totalHash.SetHalfValue(*smtBatchRootNode.rightNode.hash, 1)
//	}
//
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	smtBatchRootNode.hash = hashObj
//
//	if !noSave {
//		select {
//		case dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue):
//		default:
//			// 通道阻塞或关闭，退出
//		}
//	}
//}

// =========== op19 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	var wg sync.WaitGroup
//	noSave := sdh.s.noSaveOnInsert
//	dataChan := sdh.dataChan
//
//	// 使用 sync.Pool 复用 path 切片，减少内存分配
//	var pathPool = sync.Pool{
//		New: func() interface{} {
//			return make([]int, len(path))
//		},
//	}
//
//	// 控制最大并发 goroutine 数量
//	const maxConcurrency = 8 // 可根据 CPU 核心数调整
//	sem := make(chan struct{}, maxConcurrency)
//
//	// 内联叶子节点处理
//	if smtBatchRootNode.isLeaf() {
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(
//			utils.ConcatArrays4ByPointers(
//				smtBatchRootNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				smtBatchRootNode.nodeRightHashOrValueHash.AsUint64Pointer(),
//			),
//			&utils.LeafCapacity,
//		)
//		smtBatchRootNode.hash = hashObj
//
//		if !noSave {
//			buffer := newSmtDfsHelperDataStruct(hashObj, hashValue)
//			leafKey := utils.JoinKey(path[:level], *smtBatchRootNode.nodeLeftHashOrRemainingKey)
//			select {
//			case dataChan <- buffer:
//				select {
//				case dataChan <- newSmtDfsHelperDataStruct(hashObj, leafKey):
//				default:
//					// 如果第二个发送失败，仍然退出
//				}
//			default:
//				// 如果第一个发送失败，直接退出
//			}
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//
//	// 处理子节点的闭包函数
//	processChild := func(child *smtBatchNode, index int, childPath []int) {
//		defer wg.Done()
//		defer func() { <-sem }()      // 释放信号量
//		defer pathPool.Put(childPath) // 归还 path 切片
//
//		childPath[level] = index
//		calculateAndSaveHashesDfs(sdh, child, childPath, level+1)
//	}
//
//	// 并行处理子节点
//	for i, child := range []*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} {
//		if child != nil {
//			// 优先尝试串行处理小型子树
//			if level < 3 { // 深度阈值，可调整
//				path[level] = i
//				calculateAndSaveHashesDfs(sdh, child, path, level+1)
//				totalHash.SetHalfValue(*child.hash, i)
//			} else {
//				wg.Add(1)
//				sem <- struct{}{} // 获取信号量
//				childPath := pathPool.Get().([]int)
//				copy(childPath, path)
//				go processChild(child, i, childPath)
//			}
//		} else {
//			defaultHash := smtBatchRootNode.nodeLeftHashOrRemainingKey
//			if i == 1 {
//				defaultHash = smtBatchRootNode.nodeRightHashOrValueHash
//			}
//			totalHash.SetHalfValue(*defaultHash, i)
//		}
//	}
//
//	wg.Wait()
//
//	// 更新当前节点哈希
//	for i, child := range []*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} {
//		if child != nil {
//			totalHash.SetHalfValue(*child.hash, i)
//		}
//	}
//
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	smtBatchRootNode.hash = hashObj
//
//	if !noSave {
//		select {
//		case dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue):
//		default:
//			// 通道阻塞或关闭，退出
//		}
//	}
//}

// =========== op20 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	var wg sync.WaitGroup // 用于同步子节点处理
//	noSave := sdh.s.noSaveOnInsert
//	dataChan := sdh.dataChan // 缓存通道引用
//
//	// 内联叶子节点处理
//	if smtBatchRootNode.isLeaf() {
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(
//			utils.ConcatArrays4ByPointers(
//				smtBatchRootNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				smtBatchRootNode.nodeRightHashOrValueHash.AsUint64Pointer(),
//			),
//			&utils.LeafCapacity,
//		)
//		smtBatchRootNode.hash = hashObj
//
//		if !noSave {
//			buffer := newSmtDfsHelperDataStruct(hashObj, hashValue)
//			select {
//			case dataChan <- buffer:
//			default:
//				return // 通道阻塞或关闭，提前退出
//			}
//			select {
//			case dataChan <- newSmtDfsHelperDataStruct(hashObj, utils.JoinKey(path[:level], *smtBatchRootNode.nodeLeftHashOrRemainingKey)):
//			default:
//				return
//			}
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//
//	// 并行处理子节点
//	for i, child := range []*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} {
//		path[level] = i
//		if child != nil {
//			wg.Add(1)
//			// 为每个子节点创建独立的路径副本，避免并发修改
//			childPath := make([]int, len(path))
//			copy(childPath, path)
//			go func(c *smtBatchNode, l int, p []int) {
//				defer wg.Done()
//				calculateAndSaveHashesDfs(sdh, c, p, l)
//			}(child, level+1, childPath)
//		} else {
//			defaultHash := smtBatchRootNode.nodeLeftHashOrRemainingKey
//			if i == 1 {
//				defaultHash = smtBatchRootNode.nodeRightHashOrValueHash
//			}
//			totalHash.SetHalfValue(*defaultHash, i)
//		}
//	}
//
//	// 等待所有子节点处理完成
//	wg.Wait()
//
//	// 更新当前节点的哈希值
//	for i, child := range []*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} {
//		if child != nil {
//			totalHash.SetHalfValue(*child.hash, i)
//		}
//	}
//
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	smtBatchRootNode.hash = hashObj
//
//	if !noSave {
//		select {
//		case dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue):
//		default:
//			return // 通道阻塞或关闭，提前退出
//		}
//	}
//}

// =========== op21 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	// 内联叶子节点处理，避免函数调用开销
//	if smtBatchRootNode.isLeaf() {
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(
//			utils.ConcatArrays4ByPointers(
//				smtBatchRootNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				smtBatchRootNode.nodeRightHashOrValueHash.AsUint64Pointer(),
//			),
//			&utils.LeafCapacity,
//		)
//		smtBatchRootNode.hash = hashObj
//
//		// 如果 noSaveOnInsert 标志设置为 true，避免内存分配
//		if !sdh.s.noSaveOnInsert {
//			// 使用单一缓冲区减少内存分配
//			buffer := newSmtDfsHelperDataStruct(hashObj, hashValue)
//			sdh.dataChan <- buffer
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, utils.JoinKey(path[:level], *smtBatchRootNode.nodeLeftHashOrRemainingKey))
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//	noSave := sdh.s.noSaveOnInsert // 缓存 noSave 标志，避免多次访问字段
//
//	// 直接遍历子节点，避免创建临时数组
//	for i, child := range []*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} {
//		path[level] = i
//		if child != nil {
//			// 直接传递 path 和 level 参数，避免重新计算
//			calculateAndSaveHashesDfs(sdh, child, path, level+1)
//			totalHash.SetHalfValue(*child.hash, i)
//		} else {
//			// 仅在必要时使用 nodeLeftHashOrRemainingKey
//			defaultHash := smtBatchRootNode.nodeLeftHashOrRemainingKey
//			if i == 1 {
//				defaultHash = smtBatchRootNode.nodeRightHashOrValueHash
//			}
//			totalHash.SetHalfValue(*defaultHash, i)
//		}
//	}
//
//	// 计算并存储哈希
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	smtBatchRootNode.hash = hashObj
//
//	// 如果 noSaveOnInsert 标志设置为 true，则避免存储
//	if !noSave {
//		sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//	}
//}

// =========== op22 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchRootNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	// 内联叶子节点处理，减少函数调用
//	if smtBatchRootNode.isLeaf() {
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(
//			utils.ConcatArrays4ByPointers(
//				smtBatchRootNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				smtBatchRootNode.nodeRightHashOrValueHash.AsUint64Pointer(),
//			),
//			&utils.LeafCapacity,
//		)
//		smtBatchRootNode.hash = hashObj
//
//		if !sdh.s.noSaveOnInsert {
//			// 使用缓冲区减少内存分配
//			buffer := [2]*smtDfsHelperDataStruct{
//				newSmtDfsHelperDataStruct(hashObj, hashValue),
//				newSmtDfsHelperDataStruct(hashObj, utils.JoinKey(path[:level], *smtBatchRootNode.nodeLeftHashOrRemainingKey)),
//			}
//			sdh.dataChan <- buffer[0]
//			sdh.dataChan <- buffer[1]
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//	noSave := sdh.s.noSaveOnInsert // 缓存字段访问结果
//
//	// 处理左右子节点，合并相似逻辑
//	for i, child := range []*smtBatchNode{smtBatchRootNode.leftNode, smtBatchRootNode.rightNode} {
//		path[level] = i
//		if child != nil {
//			calculateAndSaveHashesDfs(sdh, child, path, level+1)
//			totalHash.SetHalfValue(*child.hash, i)
//		} else {
//			defaultHash := smtBatchRootNode.nodeLeftHashOrRemainingKey
//			if i == 1 {
//				defaultHash = smtBatchRootNode.nodeRightHashOrValueHash
//			}
//			totalHash.SetHalfValue(*defaultHash, i)
//		}
//	}
//
//	// 计算并存储哈希
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	smtBatchRootNode.hash = hashObj
//	if !noSave {
//		sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//	}
//}

// =========== op23 ================

//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	if smtBatchNode.isLeaf() {
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(
//			utils.ConcatArrays4ByPointers(
//				smtBatchNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(),
//				smtBatchNode.nodeRightHashOrValueHash.AsUint64Pointer(),
//			),
//			&utils.LeafCapacity,
//		)
//		smtBatchNode.hash = hashObj
//		if !sdh.s.noSaveOnInsert {
//			buffer := make([]*smtDfsHelperDataStruct, 0, 512) // 存指针
//			buffer = append(buffer, newSmtDfsHelperDataStruct(hashObj, hashValue))
//
//			nodeKey := utils.JoinKey(path[:level], *smtBatchNode.nodeLeftHashOrRemainingKey)
//			buffer = append(buffer, newSmtDfsHelperDataStruct(hashObj, nodeKey))
//
//			flushBuffer(sdh, buffer) // 统一发送
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//	buffer := make([]*smtDfsHelperDataStruct, 0, 512) // 预分配 buffer
//
//	if smtBatchNode.leftNode != nil {
//		path[level] = 0
//		calculateAndSaveHashesDfs(sdh, smtBatchNode.leftNode, path, level+1)
//		totalHash.SetHalfValue(*smtBatchNode.leftNode.hash, 0)
//	} else {
//		totalHash.SetHalfValue(*smtBatchNode.nodeLeftHashOrRemainingKey, 0)
//	}
//
//	if smtBatchNode.rightNode != nil {
//		path[level] = 1
//		calculateAndSaveHashesDfs(sdh, smtBatchNode.rightNode, path, level+1)
//		totalHash.SetHalfValue(*smtBatchNode.rightNode.hash, 1)
//	} else {
//		totalHash.SetHalfValue(*smtBatchNode.nodeRightHashOrValueHash, 1)
//	}
//
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	if !sdh.s.noSaveOnInsert {
//		buffer = append(buffer, newSmtDfsHelperDataStruct(hashObj, hashValue))
//		flushBuffer(sdh, buffer) // 统一发送
//	}
//
//	smtBatchNode.hash = hashObj
//}
//
//// 统一发送 buffer
//func flushBuffer(sdh *smtDfsHelper, buffer []*smtDfsHelperDataStruct) {
//	for _, data := range buffer {
//		sdh.dataChan <- data // 解引用后发送
//	}
//}

// =========== op24 ================

//// no point to parallelize this function because db consumer is slower than this producer
//func calculateAndSaveHashesDfs(
//	sdh *smtDfsHelper,
//	smtBatchNode *smtBatchNode,
//	path []int,
//	level int,
//) {
//	if smtBatchNode.isLeaf() {
//		hashObj, hashValue := utils.HashKeyAndValueByPointers(utils.ConcatArrays4ByPointers(smtBatchNode.nodeLeftHashOrRemainingKey.AsUint64Pointer(), smtBatchNode.nodeRightHashOrValueHash.AsUint64Pointer()), &utils.LeafCapacity)
//		smtBatchNode.hash = hashObj
//		if !sdh.s.noSaveOnInsert {
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//
//			nodeKey := utils.JoinKey(path[:level], *smtBatchNode.nodeLeftHashOrRemainingKey)
//			sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, nodeKey)
//		}
//		return
//	}
//
//	var totalHash utils.NodeValue8
//
//	if smtBatchNode.leftNode != nil {
//		path[level] = 0
//		calculateAndSaveHashesDfs(sdh, smtBatchNode.leftNode, path, level+1)
//		totalHash.SetHalfValue(*smtBatchNode.leftNode.hash, 0) // no point to check for error because we used hardcoded 0 which ensures that no error will be returned
//	} else {
//		totalHash.SetHalfValue(*smtBatchNode.nodeLeftHashOrRemainingKey, 0) // no point to check for error because we used hardcoded 0 which ensures that no error will be returned
//	}
//
//	if smtBatchNode.rightNode != nil {
//		path[level] = 1
//		calculateAndSaveHashesDfs(sdh, smtBatchNode.rightNode, path, level+1)
//		totalHash.SetHalfValue(*smtBatchNode.rightNode.hash, 1) // no point to check for error because we used hardcoded 1 which ensures that no error will be returned
//	} else {
//		totalHash.SetHalfValue(*smtBatchNode.nodeRightHashOrValueHash, 1) // no point to check for error because we used hardcoded 1 which ensures that no error will be returned
//	}
//
//	hashObj, hashValue := utils.HashKeyAndValueByPointers(totalHash.ToUintArrayByPointer(), &utils.BranchCapacity)
//	if !sdh.s.noSaveOnInsert {
//		sdh.dataChan <- newSmtDfsHelperDataStruct(hashObj, hashValue)
//	}
//
//	smtBatchNode.hash = hashObj
//}

type smtBatchNode struct {
	nodeLeftHashOrRemainingKey *utils.NodeKey
	nodeRightHashOrValueHash   *utils.NodeKey
	leaf                       bool
	parentNode                 *smtBatchNode // if nil => this is the root node
	leftNode                   *smtBatchNode
	rightNode                  *smtBatchNode
	hash                       *[4]uint64
}

func newSmtBatchNodeLeaf(
	nodeLeftHashOrRemainingKey,
	nodeRightHashOrValueHash *utils.NodeKey,
	parentNode *smtBatchNode,
) *smtBatchNode {
	return &smtBatchNode{
		nodeLeftHashOrRemainingKey: nodeLeftHashOrRemainingKey,
		nodeRightHashOrValueHash:   nodeRightHashOrValueHash,
		leaf:                       true,
		parentNode:                 parentNode,
		leftNode:                   nil,
		rightNode:                  nil,
		hash:                       nil,
	}
}

func (s *SMT) fetchNodeDataFromDb(nodeHash *utils.NodeKey, parentNode *smtBatchNode) (*smtBatchNode, error) {
	if nodeHash.IsZero() {
		return nil, nil
	}
	metrics.GetLogStatistics().CumulativeValue(metrics.ZKHashSMTGetKey, 1)
	start := time.Now()
	dbNodeValue, err := s.Db.Get(*nodeHash)
	metrics.GetLogStatistics().CumulativeMicroTiming(metrics.ZKHashSMTGetKeyTiming, time.Since(start))
	if err != nil {
		return nil, err
	}

	nodeLeftHashOrRemainingKey := utils.NodeKeyFromBigIntArray(dbNodeValue[0:4])
	nodeRightHashOrValueHash := utils.NodeKeyFromBigIntArray(dbNodeValue[4:8])
	return &smtBatchNode{
		parentNode:                 parentNode,
		nodeLeftHashOrRemainingKey: &nodeLeftHashOrRemainingKey,
		nodeRightHashOrValueHash:   &nodeRightHashOrValueHash,
		leaf:                       dbNodeValue.IsFinalNode(),
	}, nil
}

func (sbn *smtBatchNode) isLeaf() bool {
	return sbn.leaf
}

func (sbn *smtBatchNode) getTheSingleLeafAndDirectionIfAny() (*smtBatchNode, int) {
	if sbn.leftNode != nil && sbn.rightNode == nil && sbn.leftNode.isLeaf() {
		return sbn.leftNode, 0
	}
	if sbn.leftNode == nil && sbn.rightNode != nil && sbn.rightNode.isLeaf() {
		return sbn.rightNode, 1
	}
	return nil, -1
}

func (sbn *smtBatchNode) getNextNodeHashInDirection(direction int) *utils.NodeKey {
	if direction == 0 {
		return sbn.nodeLeftHashOrRemainingKey
	} else {
		return sbn.nodeRightHashOrValueHash
	}
}

func (sbn *smtBatchNode) getChildInDirection(direction int) **smtBatchNode {
	if direction == 0 {
		return &sbn.leftNode
	} else {
		return &sbn.rightNode
	}
}

func (sbn *smtBatchNode) updateHashesAfterDelete() {
	if sbn.leftNode == nil {
		sbn.nodeLeftHashOrRemainingKey = &utils.NodeKey{0, 0, 0, 0}
	}
	if sbn.rightNode == nil {
		sbn.nodeRightHashOrValueHash = &utils.NodeKey{0, 0, 0, 0}
	}
}

func (sbn *smtBatchNode) createALeafInEmptyDirection(
	insertingNodePath []int,
	insertingNodePathLevel int,
	insertingNodeKey *utils.NodeKey,
) (**smtBatchNode, error) {
	direction := insertingNodePath[insertingNodePathLevel]
	childPointer := sbn.getChildInDirection(direction)
	if (*childPointer) != nil {
		return nil, fmt.Errorf("branch has already been taken")
	}
	remainingKey := utils.RemoveKeyBits(*insertingNodeKey, insertingNodePathLevel+1)
	*childPointer = newSmtBatchNodeLeaf(&remainingKey, nil, sbn)
	return childPointer, nil
}

func (sbn *smtBatchNode) expandLeafByAddingALeafInDirection(
	insertingNodeKey []int,
	insertingNodeKeyLevel int,
) **smtBatchNode {
	direction := insertingNodeKey[insertingNodeKeyLevel]
	insertingNodeKeyUpToLevel := insertingNodeKey[:insertingNodeKeyLevel]

	childPointer := sbn.getChildInDirection(direction)

	nodeKey := utils.JoinKey(insertingNodeKeyUpToLevel, *sbn.nodeLeftHashOrRemainingKey)
	remainingKey := utils.RemoveKeyBits(*nodeKey, insertingNodeKeyLevel+1)

	*childPointer = newSmtBatchNodeLeaf(&remainingKey, sbn.nodeRightHashOrValueHash, sbn)
	sbn.nodeLeftHashOrRemainingKey = &utils.NodeKey{0, 0, 0, 0}
	sbn.nodeRightHashOrValueHash = &utils.NodeKey{0, 0, 0, 0}
	sbn.leaf = false

	return childPointer
}

func (sbn *smtBatchNode) collapseLeafByRemovingTheSingleLeaf(
	insertingNodeKey []int,
	insertingNodeKeyLevel int,
	theSingleLeaf *smtBatchNode,
	theSingleNodeLeafDirection int,
) **smtBatchNode {
	insertingNodeKeyUpToLevel := insertingNodeKey[:insertingNodeKeyLevel+1]
	insertingNodeKeyUpToLevel[insertingNodeKeyLevel] = theSingleNodeLeafDirection
	nodeKey := utils.JoinKey(insertingNodeKeyUpToLevel, *theSingleLeaf.nodeLeftHashOrRemainingKey)
	remainingKey := utils.RemoveKeyBits(*nodeKey, insertingNodeKeyLevel)

	sbn.nodeLeftHashOrRemainingKey = &remainingKey
	sbn.nodeRightHashOrValueHash = theSingleLeaf.nodeRightHashOrValueHash
	sbn.leaf = true
	sbn.leftNode = nil
	sbn.rightNode = nil

	return &sbn.parentNode
}

type smtDfsHelperDataStruct struct {
	key   *[4]uint64
	value interface{}
}

func newSmtDfsHelperDataStruct(key *[4]uint64, value interface{}) *smtDfsHelperDataStruct {
	return &smtDfsHelperDataStruct{
		key:   key,
		value: value,
	}
}

type smtDfsHelper struct {
	s        *SMT
	dataChan chan *smtDfsHelperDataStruct
	wg       *sync.WaitGroup
	once     *sync.Once
}

func newSmtDfsHelper(s *SMT) *smtDfsHelper {
	sdh := &smtDfsHelper{
		s:        s,
		dataChan: make(chan *smtDfsHelperDataStruct, 1<<16),
		wg:       &sync.WaitGroup{},
		once:     &sync.Once{},
	}

	sdh.wg.Add(1)

	return sdh
}

func (sdh *smtDfsHelper) destroy() {
	sdh.once.Do(func() {
		close(sdh.dataChan)
		sdh.wg.Done()
	})
}

func (sdh *smtDfsHelper) startConsumersLoop(s *SMT) error {
	for {
		dataStruct, ok := <-sdh.dataChan
		if !ok {
			return nil
		}

		switch castedDataStruct := dataStruct.value.(type) {
		case *utils.NodeKey:
			start := time.Now()
			metrics.GetLogStatistics().CumulativeValue(metrics.ZKHashSMTInsertKey, 1)
			if err := s.Db.InsertHashKey(*dataStruct.key, *castedDataStruct); err != nil {
				return fmt.Errorf("calculating and saving hashes dfs: %w", err)
			}
			metrics.GetLogStatistics().CumulativeMicroTiming(metrics.ZKHashSMTInsertKeyTiming, time.Since(start))
		case *utils.NodeValue12:
			start := time.Now()
			metrics.GetLogStatistics().CumulativeValue(metrics.ZKHashSMTInsertKey, 1)
			if err := s.Db.Insert(*dataStruct.key, *castedDataStruct); err != nil {
				return fmt.Errorf("calculating and saving hashes dfs: %w", err)
			}
			metrics.GetLogStatistics().CumulativeMicroTiming(metrics.ZKHashSMTInsertKeyTiming, time.Since(start))
		}
	}
}

func setNodeKeyMapValue[T int | *utils.NodeKey](
	nodeKeyMap map[uint64]map[uint64]map[uint64]map[uint64]T,
	nodeKey *utils.NodeKey,
	value T,
) {
	mapLevel0, found := nodeKeyMap[nodeKey[0]]
	if !found {
		mapLevel0 = make(map[uint64]map[uint64]map[uint64]T)
		nodeKeyMap[nodeKey[0]] = mapLevel0
	}

	mapLevel1, found := mapLevel0[nodeKey[1]]
	if !found {
		mapLevel1 = make(map[uint64]map[uint64]T)
		mapLevel0[nodeKey[1]] = mapLevel1
	}

	mapLevel2, found := mapLevel1[nodeKey[2]]
	if !found {
		mapLevel2 = make(map[uint64]T)
		mapLevel1[nodeKey[2]] = mapLevel2
	}

	mapLevel2[nodeKey[3]] = value
}

func getNodeKeyMapValue[T int | *utils.NodeKey](
	nodeKeyMap map[uint64]map[uint64]map[uint64]map[uint64]T,
	nodeKey *utils.NodeKey,
) (T, bool) {
	var notExistingValue T

	mapLevel0, found := nodeKeyMap[nodeKey[0]]
	if !found {
		return notExistingValue, false
	}

	mapLevel1, found := mapLevel0[nodeKey[1]]
	if !found {
		return notExistingValue, false
	}

	mapLevel2, found := mapLevel1[nodeKey[2]]
	if !found {
		return notExistingValue, false
	}

	value, found := mapLevel2[nodeKey[3]]
	return value, found
}
