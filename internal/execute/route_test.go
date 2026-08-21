package execute

import (
	"math/big"
	"testing"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
)

func TestExecutionQuoteCandidates_prefersHint(t *testing.T) {
	cfg := chain.BSCMainnet
	usdt := common.HexToAddress("0x55d398326f99059fF775485246999027B3197955")
	cands := cfg.ExecutionQuoteCandidates(usdt)
	if len(cands) == 0 {
		t.Fatal("expected candidates")
	}
	if cands[0] != usdt {
		t.Fatalf("first=%s want USDT", cands[0].Hex())
	}
}

func TestUsdToTokenUnits(t *testing.T) {
	got := usdToTokenUnits(50, 18)
	want := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	want.Mul(want, big.NewInt(50))
	if got.Cmp(want) != 0 {
		t.Fatalf("got=%s want=%s", got, want)
	}
	got6 := usdToTokenUnits(100, 6)
	if got6.Cmp(big.NewInt(100_000_000)) != 0 {
		t.Fatalf("6dec=%s", got6)
	}
}
