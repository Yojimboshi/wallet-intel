package pool

import (
	"math/big"
	"testing"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
)

func TestAmountsForLiquidity_inRange(t *testing.T) {
	sqrtP := getSqrtRatioAtTick(0)
	sqrtL := getSqrtRatioAtTick(-10)
	sqrtU := getSqrtRatioAtTick(10)
	liquidity := big.NewInt(1_000_000_000_000)

	amount0, amount1 := amountsForLiquidity(sqrtP, sqrtL, sqrtU, liquidity)
	if amount0.Sign() <= 0 || amount1.Sign() <= 0 {
		t.Fatalf("expected both token amounts in range, got %s %s", amount0, amount1)
	}
}

func TestTokenBalanceUSD_weth(t *testing.T) {
	amount := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) // 1 WETH
	weth := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	usd := tokenBalanceUSD(chain.EthereumMainnet, weth, amount, 4000)
	if usd != 4000 {
		t.Fatalf("expected 4000, got %f", usd)
	}
}

func TestIsV3Hint(t *testing.T) {
	if !isV3Hint("uniswap-v3") {
		t.Fatal("expected v3 hint")
	}
	if isV3Hint("uniswap-v2") {
		t.Fatal("expected not v3")
	}
}
