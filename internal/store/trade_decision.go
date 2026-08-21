package store

import (
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/parse"
)

// TradeDecision is one watched-wallet trade evaluation (alert + copy outcome).
type TradeDecision struct {
	Timestamp     time.Time
	Chain         string
	Wallet        string
	WalletLabel   string
	Side          string
	Token         string
	TokenSymbol   string
	TradeUsd      float64
	EffectiveUsd  float64
	BatchLegs     int
	MarketCapUsd  float64
	LiquidityUsd  float64
	TxHash        string
	BlockNumber   uint64
	AlertAction   string // skip, pending, follow
	AlertReason   string
	CopyAction    string // skip, follow, na
	CopyReason    string
	CopySizeUsd   float64
}

func TradeDecisionFrom(tr parse.Trade, info enrich.TokenInfo, chain string, tradeUsd, effectiveUsd float64, batchLegs int) TradeDecision {
	sym := info.Symbol
	if sym == "" {
		sym = tr.Token.Hex()
	}
	return TradeDecision{
		Timestamp:    time.Now().UTC(),
		Chain:        chain,
		Wallet:       tr.Wallet.Hex(),
		WalletLabel:  tr.WalletLabel,
		Side:         tr.Side,
		Token:        tr.Token.Hex(),
		TokenSymbol:  sym,
		TradeUsd:     tradeUsd,
		EffectiveUsd: effectiveUsd,
		BatchLegs:    batchLegs,
		MarketCapUsd: info.MarketCap,
		LiquidityUsd: info.Liquidity,
		TxHash:       tr.TxHash.Hex(),
		BlockNumber:  tr.BlockNumber,
		CopyAction:   "na",
	}
}
