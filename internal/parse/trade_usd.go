package parse

import (
	"math/big"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/ethereum/go-ethereum/common"
)

// TradeUsd estimates notional USD for a trade (rough; used for minBuyUsd filters).
// nativeUsdPrice is the cached on-chain ETH/BNB USD price when quote is wrapped native.
func TradeUsd(tr Trade, info enrich.TokenInfo, chainCfg chain.Config, nativeUsdPrice float64) float64 {
	if tr.Side == sideBuy && tr.QuoteAmount != nil {
		if tr.QuoteToken != (common.Address{}) {
			if q, ok := chainCfg.QuoteTokens[tr.QuoteToken]; ok {
				amt := amountFloat(tr.QuoteAmount, q.Decimals)
				switch q.Symbol {
				case "USDC", "USDT", "DAI", "BUSD":
					return amt
				case "WETH", "WBNB":
					if amt > 0 && nativeUsdPrice > 0 {
						return amt * nativeUsdPrice
					}
				}
			}
		} else if nativeUsdPrice > 0 {
			if _, ok := chainCfg.WrappedNative(); ok {
				return amountFloat(tr.QuoteAmount, 18) * nativeUsdPrice
			}
		}
	}
	if info.PriceUsd > 0 && tr.TokenAmount != nil {
		decimals := info.Decimals
		if decimals <= 0 {
			decimals = 18
		}
		return amountFloat(tr.TokenAmount, decimals) * info.PriceUsd
	}
	return 0
}

func amountFloat(amount *big.Int, decimals int) float64 {
	if amount == nil {
		return 0
	}
	f := new(big.Float).SetInt(amount)
	div := new(big.Float).SetFloat64(pow10(decimals))
	out, _ := new(big.Float).Quo(f, div).Float64()
	return out
}

func pow10(n int) float64 {
	out := 1.0
	for i := 0; i < n; i++ {
		out *= 10
	}
	return out
}
