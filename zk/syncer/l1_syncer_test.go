package syncer

import (
	"reflect"
	"testing"
)

func TestMakeFetchJobs_RangeBased(t *testing.T) {
	var (
		from       uint64 = 1
		to         uint64 = 100
		blockRange uint64 = 20
	)

	jobs := makeFetchJobs(from, to, blockRange)

	coveredBlocks := make([]bool, to-from+1)

	for _, job := range jobs {
		if job.From > job.To {
			t.Errorf("Invalid job range: From %d > To %d", job.From, job.To)
		}
		if job.To-job.From+1 > blockRange {
			t.Errorf("Job range too large: %d blocks (max %d)", job.To-job.From+1, blockRange)
		}
		for i := job.From; i <= job.To; i++ {
			if i < from || i > to {
				t.Errorf("Block %d out of range [%d, %d]", i, from, to)
			}
			coveredBlocks[i-from] = true
		}
	}

	// Check that all blocks are covered exactly once
	for i, covered := range coveredBlocks {
		if !covered {
			t.Errorf("Block %d not covered", i+int(from))
		}
	}
}

func TestMakeFetchJobs(t *testing.T) {
	tests := []struct {
		from       uint64
		to         uint64
		blockRange uint64
		expected   []fetchJob
	}{
		{
			from:       100,
			to:         250,
			blockRange: 50,
			expected: []fetchJob{
				{From: 100, To: 149},
				{From: 150, To: 199},
				{From: 200, To: 249},
				{From: 250, To: 250},
			},
		},
		{
			from:       1,
			to:         99,
			blockRange: 100,
			expected: []fetchJob{
				{From: 1, To: 99},
			},
		},
	}

	for i, test := range tests {
		result := makeFetchJobs(test.from, test.to, test.blockRange)
		if !reflect.DeepEqual(result, test.expected) {
			t.Errorf("Test %d failed: expected %+v, got %+v", i, test.expected, result)
		}
	}
}
