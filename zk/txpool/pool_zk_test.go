package txpool

import (
	"fmt"
	"math/big"
	"testing"
)

func TestFeeCalc(t *testing.T) {
	gp := big.NewInt(0x112345678)
	for i := 0; i < 4; i++ {
		gpu64 := gp.Uint64()
		gpu64 *= gpu64
		gp.Mul(gp, gp)
		fmt.Println(gpu64)
		if gp.Uint64() != gpu64 {
			t.Errorf("Error in gas price calculation")
		}
	}
}
