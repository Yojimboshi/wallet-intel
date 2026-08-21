package parse

import (
	"math/big"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Cap on "real" swap size: 1e12 tokens at 18 decimals (or 1e24 at 6).
var maxSpendAmount = new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)

func enrichAggregatorQuotes(trades []Trade, logs []*types.Log, chainCfg chain.Config) {
	missing := make([]int, 0, len(trades))
	for i := range trades {
		if trades[i].Side != sideBuy {
			continue
		}
		if trades[i].QuoteAmount != nil && trades[i].QuoteAmount.Sign() > 0 {
			continue
		}
		missing = append(missing, i)
	}
	if len(missing) == 0 {
		return
	}

	token, amount := pickStableSpend(logs, chainCfg)
	if amount == nil || amount.Sign() <= 0 {
		return
	}

	if len(missing) == 1 {
		setQuote(&trades[missing[0]], token, amount, chainCfg)
		return
	}

	total := new(big.Int)
	for _, i := range missing {
		if trades[i].TokenAmount != nil {
			total.Add(total, trades[i].TokenAmount)
		}
	}
	if total.Sign() <= 0 {
		share := new(big.Int).Div(amount, big.NewInt(int64(len(missing))))
		if share.Sign() <= 0 {
			return
		}
		for _, i := range missing {
			setQuote(&trades[i], token, share, chainCfg)
		}
		return
	}

	assigned := new(big.Int)
	for n, i := range missing {
		var share *big.Int
		if n == len(missing)-1 {
			share = new(big.Int).Sub(amount, assigned)
		} else if trades[i].TokenAmount == nil || trades[i].TokenAmount.Sign() <= 0 {
			share = big.NewInt(0)
		} else {
			share = new(big.Int).Mul(amount, trades[i].TokenAmount)
			share.Div(share, total)
			assigned.Add(assigned, share)
		}
		if share.Sign() > 0 {
			setQuote(&trades[i], token, share, chainCfg)
		}
	}
}

func pickStableSpend(logs []*types.Log, chainCfg chain.Config) (common.Address, *big.Int) {
	var bestToken common.Address
	var bestAmt *big.Int
	for _, lg := range logs {
		if lg == nil || len(lg.Topics) < 3 || lg.Topics[0] != topicTransfer {
			continue
		}
		q, ok := chainCfg.QuoteTokens[lg.Address]
		if !ok {
			continue
		}
		switch q.Symbol {
		case "USDC", "USDT", "DAI", "BUSD":
		default:
			continue
		}
		amt := new(big.Int).SetBytes(lg.Data)
		if !isSpendSized(amt) {
			continue
		}
		if bestAmt == nil || amt.Cmp(bestAmt) > 0 {
			bestAmt = amt
			bestToken = lg.Address
		}
	}
	return bestToken, bestAmt
}

func isSpendSized(amount *big.Int) bool {
	if amount == nil || amount.Sign() <= 0 {
		return false
	}
	if amount.Cmp(maxSpendAmount) >= 0 {
		return false
	}
	return true
}

func setQuote(tr *Trade, token common.Address, amount *big.Int, chainCfg chain.Config) {
	if tr == nil || amount == nil || amount.Sign() <= 0 {
		return
	}
	tr.QuoteToken = token
	tr.QuoteAmount = new(big.Int).Set(amount)
	if q, ok := chainCfg.QuoteTokens[token]; ok {
		tr.QuoteSymbol = q.Symbol
	}
	if tr.DEX == "" {
		tr.DEX = "aggregator"
	}
}
