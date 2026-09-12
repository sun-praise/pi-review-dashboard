package store

import (
	"math"
	"testing"
)

func TestDeepseekCostIncludesOutput(t *testing.T) {
	// Matches DEFAULT_DEEPSEEK_COST: in*0.14 + out*0.28 + cr*0.0028, /1e6.
	got := deepseekCost(1_000_000, 1_000_000, 1_000_000)
	want := 0.14 + 0.28 + 0.0028
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("deepseekCost = %v, want %v", got, want)
	}
	withoutOutput := float64(1_000_000)*0.14/1e6 + float64(1_000_000)*0.0028/1e6
	if got <= withoutOutput {
		t.Fatalf("cost %v should exceed no-output formula %v", got, withoutOutput)
	}
}
