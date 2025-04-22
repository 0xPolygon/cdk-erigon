package smt

import (
	"github.com/erigontech/erigon/smt/pkg/utils"
	"math/rand"
	"testing"
)

func BenchmarkSetNodeKeyMapValue(b *testing.B) {
	var nodes []*utils.NodeKey
	for i := 0; i < 10000; i++ {
		nodes = append(nodes, &utils.NodeKey{
			rand.Uint64(),
			rand.Uint64(),
			rand.Uint64(),
			rand.Uint64(),
		})
	}

	var dummy uint64

	b.Run("NestedNodeKeyMap", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			nodeHashesForDelete := make(map[uint64]map[uint64]map[uint64]map[uint64]*utils.NodeKey)
			for _, visitedNodeHash := range nodes {
				if visitedNodeHash != nil {
					setNodeKeyMapValue(nodeHashesForDelete, visitedNodeHash, visitedNodeHash)
				}
			}

			for _, mapLevel0 := range nodeHashesForDelete {
				for _, mapLevel1 := range mapLevel0 {
					for _, mapLevel2 := range mapLevel1 {
						for _, nodeHash := range mapLevel2 {
							dummy = nodeHash[0]
						}
					}
				}
			}
		}
	})

	b.Run("DirectNodeKeyMap", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			nodeHashesForDelete := make(map[utils.NodeKey]struct{})
			updateNodeHashesForDelete(nodeHashesForDelete, nodes)

			for nodeHash, _ := range nodeHashesForDelete {
				dummy = nodeHash[0]
			}
		}
	})

	_ = dummy
}
