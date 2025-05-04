package vm

import (
	"testing"
)

func TestSetBatchCounterLimitPercentage(t *testing.T) {
	tests := []struct {
		input       int
		expectError bool
	}{
		{50, false},
		{20, false},
		{100, false},
		{10, true},
		{110, true},
	}

	for _, tt := range tests {
		err := SetBatchCounterLimitPercentage(tt.input)
		if tt.expectError && err == nil {
			t.Errorf("expected error for input %d, got nil", tt.input)
		}
		if !tt.expectError && err != nil {
			t.Errorf("did not expect error for input %d, got %v", tt.input, err)
		}
	}
}

func TestRewriteTotalSteps(t *testing.T) {
	_ = SetBatchCounterLimitPercentage(50)
	total := 200
	expected := 100

	result := rewriteTotalSteps(total)
	if result != expected {
		t.Errorf("expected %d, got %d", expected, result)
	}

	_ = SetBatchCounterLimitPercentage(25)
	total = 400
	expected = 100

	result = rewriteTotalSteps(total)
	if result != expected {
		t.Errorf("expected %d, got %d", expected, result)
	}
}
