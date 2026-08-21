package pool

import (
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

func ethereumCall(to common.Address, data []byte) ethereum.CallMsg {
	return ethereum.CallMsg{To: &to, Data: data}
}

func getSqrtRatioAtTick(tick int) *big.Int {
	if tick == 0 {
		return new(big.Int).Set(q96)
	}
	price := math.Pow(1.0001, float64(tick))
	sqrt := math.Sqrt(price)
	f := new(big.Float).SetFloat64(sqrt)
	f.Mul(f, new(big.Float).SetInt(q96))
	out, _ := f.Int(nil)
	return out
}

// amountsForLiquidity matches dex-app v3Utils.getAmountsForLiquidity.
func amountsForLiquidity(sqrtPriceX96, sqrtPriceLowerX96, sqrtPriceUpperX96, liquidity *big.Int) (*big.Int, *big.Int) {
	if liquidity == nil || liquidity.Sign() <= 0 {
		return big.NewInt(0), big.NewInt(0)
	}
	var amount0, amount1 big.Int
	if sqrtPriceX96.Cmp(sqrtPriceLowerX96) <= 0 {
		// below range — token0 only
		num := new(big.Int).Mul(liquidity, q96)
		num.Mul(num, new(big.Int).Sub(sqrtPriceUpperX96, sqrtPriceLowerX96))
		den := new(big.Int).Mul(sqrtPriceLowerX96, sqrtPriceUpperX96)
		amount0.Div(num, den)
	} else if sqrtPriceX96.Cmp(sqrtPriceUpperX96) < 0 {
		// in range — both
		num0 := new(big.Int).Mul(liquidity, q96)
		num0.Mul(num0, new(big.Int).Sub(sqrtPriceUpperX96, sqrtPriceX96))
		den0 := new(big.Int).Mul(sqrtPriceX96, sqrtPriceUpperX96)
		amount0.Div(num0, den0)

		num1 := new(big.Int).Mul(liquidity, new(big.Int).Sub(sqrtPriceX96, sqrtPriceLowerX96))
		amount1.Div(num1, q96)
	} else {
		// above range — token1 only
		num := new(big.Int).Mul(liquidity, new(big.Int).Sub(sqrtPriceUpperX96, sqrtPriceLowerX96))
		amount1.Div(num, q96)
	}
	return &amount0, &amount1
}
