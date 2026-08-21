package execute

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/Yojimboshi/wallet-intel/internal/config"
)

func TestSellSlippageTiers(t *testing.T) {
	tiers := sellSlippageTiers(config.ExecutionConfig{})
	want := []int{500, 1500, 2500, 3500}
	if len(tiers) != len(want) {
		t.Fatalf("got tiers=%v want=%v", tiers, want)
	}
	for i := range want {
		if tiers[i] != want[i] {
			t.Fatalf("tiers[%d]=%d want=%d (full=%v)", i, tiers[i], want[i], tiers)
		}
	}
}

func TestBuySlippageTiers(t *testing.T) {
	tiers := buySlippageTiers(config.ExecutionConfig{SlippageBps: 500})
	if len(tiers) != 2 || tiers[0] != 500 || tiers[1] != 1000 {
		t.Fatalf("got %v", tiers)
	}
}

func TestIsSlippageBuyErr(t *testing.T) {
	if !IsSlippageBuyErr(fmt.Errorf("estimate gas: execution reverted")) {
		t.Fatal("expected buy slippage err")
	}
}

func TestIsSlippageSellErr(t *testing.T) {
	if !IsSlippageSellErr(fmt.Errorf("PancakeRouter: INSUFFICIENT_OUTPUT_AMOUNT")) {
		t.Fatal("expected slippage err")
	}
}

func TestApplySellFraction(t *testing.T) {
	bal := big.NewInt(1_000_000)
	half := applySellFraction(bal, 5000)
	if half.Cmp(big.NewInt(500_000)) != 0 {
		t.Fatalf("half=%s", half)
	}
	full := applySellFraction(bal, 0)
	if full.Cmp(bal) != 0 {
		t.Fatalf("full=%s", full)
	}
}
