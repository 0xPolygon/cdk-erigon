package vm

import (
	"fmt"
)

const (
	MinPercentage = 20
	MaxPercentage = 100
)

var (
	gCounterLimitPercentage = 100
)

func rewriteTotalSteps(totalSteps int) int {
	percentage := float64(gCounterLimitPercentage) / 100.0
	return int(float64(totalSteps) * percentage)
}

func SetBatchCounterLimitPercentage(factor int) error {
	if factor < MinPercentage || factor > MaxPercentage {
		return fmt.Errorf("counter limits percentage should be between %d and %d", MinPercentage, MaxPercentage)
	}

	gCounterLimitPercentage = factor

	return nil
}
