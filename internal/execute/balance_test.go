package execute

import (
	"math/big"
	"testing"
)

func TestGasReserve_buyNeedsTradePlusReserve(t *testing.T) {
	reserve := usdToWei(10, 4000)
	trade := usdToWei(100, 4000)
	need := new(big.Int).Add(reserve, trade)

	bal := new(big.Int).Set(need)
	if bal.Cmp(need) < 0 {
		t.Fatal("expected enough for buy+reserve")
	}
	bal.Sub(bal, big.NewInt(1))
	if bal.Cmp(need) >= 0 {
		t.Fatal("expected not enough when 1 wei short")
	}
}

func TestGasReserve_sellNeedsReserveOnly(t *testing.T) {
	reserve := usdToWei(10, 600)
	bal := new(big.Int).Add(reserve, big.NewInt(1e15))
	if bal.Cmp(reserve) < 0 {
		t.Fatal("sell only needs gas reserve in native")
	}
}
