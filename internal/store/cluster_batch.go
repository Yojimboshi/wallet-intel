package store

import (
	"github.com/ethereum/go-ethereum/common"
)

// TxClusterBuy is the combined buy size for one token in a single tx.
type TxClusterBuy struct {
	TotalUsd float64
	Legs     int
}

// TxClusterLeg is one watched-wallet buy in a tx cluster summary.
type TxClusterLeg struct {
	Token    common.Address
	Wallet   common.Address
	Side     string
	TradeUsd float64
}

// BuildTxClusterBuys sums buy USD per token when 2+ distinct wallets buy the same token.
func BuildTxClusterBuys(legs []TxClusterLeg) map[common.Address]TxClusterBuy {
	type acc struct {
		total   float64
		wallets map[common.Address]struct{}
	}
	byToken := make(map[common.Address]*acc)
	for _, leg := range legs {
		if leg.Side != "buy" || leg.TradeUsd <= 0 {
			continue
		}
		a, ok := byToken[leg.Token]
		if !ok {
			a = &acc{wallets: make(map[common.Address]struct{})}
			byToken[leg.Token] = a
		}
		if _, seen := a.wallets[leg.Wallet]; !seen {
			a.wallets[leg.Wallet] = struct{}{}
		}
		a.total += leg.TradeUsd
	}

	out := make(map[common.Address]TxClusterBuy)
	for token, a := range byToken {
		if len(a.wallets) < 2 {
			continue
		}
		out[token] = TxClusterBuy{TotalUsd: a.total, Legs: len(a.wallets)}
	}
	return out
}
