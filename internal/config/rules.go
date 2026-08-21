package config

import (
	"fmt"

	"github.com/Yojimboshi/wallet-intel/internal/enrich"
)

type ExecutionRules struct {
	MinBuyUsd         float64 `json:"minBuyUsd"`
	MaxMarketCapUsd   float64 `json:"maxMarketCapUsd"`
	MinLiquidityUsd   float64 `json:"minLiquidityUsd"`
	RequireMcLiq         bool `json:"requireMcLiq"`
	FirstBuyOnly         bool `json:"firstBuyOnly"`
	DedupeAcrossWallets  bool `json:"dedupeAcrossWallets"`
}

type SafetyFile struct {
	Enabled               bool    `json:"enabled"`
	MaxBuyTaxPct          float64 `json:"maxBuyTaxPct"`
	MaxSellTaxPct         float64 `json:"maxSellTaxPct"`
	BlockHoneypot         bool    `json:"blockHoneypot"`
	BlockMintable         bool    `json:"blockMintable"`
	BlockTransferPausable bool    `json:"blockTransferPausable"`
	BlockUnlockedLP       bool    `json:"blockUnlockedLP"`
	BlockCannotSell       bool    `json:"blockCannotSell"`
	FailClosed            bool    `json:"failClosed"`
}

func (r Rules) PassesAlert(side string, tradeUsd float64, info enrich.TokenInfo) (bool, string) {
	if !r.AlertsOn(side) {
		return false, "side not in alertOn"
	}
	if side == "buy" && r.MinBuyUsd > 0 {
		if tradeUsd <= 0 {
			return false, "buy size unknown, below minBuyUsd"
		}
		if tradeUsd < r.MinBuyUsd {
			return false, fmt.Sprintf("buy $%.0f below minBuyUsd $%.0f", tradeUsd, r.MinBuyUsd)
		}
	}
	if r.MaxMarketCapUsd > 0 && info.MarketCap > r.MaxMarketCapUsd {
		return false, fmt.Sprintf("MC $%.0f above max $%.0f", info.MarketCap, r.MaxMarketCapUsd)
	}
	if r.MinLiquidityUsd > 0 && info.Liquidity > 0 && info.Liquidity < r.MinLiquidityUsd {
		return false, fmt.Sprintf("liq $%.0f below min $%.0f", info.Liquidity, r.MinLiquidityUsd)
	}
	// Unknown liq (no DexScreener pair yet) is allowed — new launch, not a failed min-liq check.
	return true, ""
}

// BatchWindowSec is the rolling window to sum cluster buys of the same token.
func (r Rules) BatchWindowSec() int {
	if r.BatchBuyWindowSec > 0 {
		return r.BatchBuyWindowSec
	}
	return 120
}

// BatchMaxLegs is how many sequential legs to accumulate before resetting.
func (r Rules) BatchMaxLegs() int {
	if r.BatchBuyMaxLegs > 0 {
		return r.BatchBuyMaxLegs
	}
	return 5
}

// FlipGuardConfig builds store.FlipGuard settings from rules (with defaults).
func (r Rules) FlipRecentSellBlockSecOrDefault() int {
	if r.FlipRecentSellBlockSec > 0 {
		return r.FlipRecentSellBlockSec
	}
	return 900
}

func (r Rules) FlipCycleWindowSecOrDefault() int {
	if r.FlipCycleWindowSec > 0 {
		return r.FlipCycleWindowSec
	}
	return 1800
}

func (r Rules) FlipMuteAfterCyclesOrDefault() int {
	if r.FlipMuteAfterCycles > 0 {
		return r.FlipMuteAfterCycles
	}
	return 2
}

func (r Rules) FlipMuteSecOrDefault() int {
	if r.FlipMuteSec > 0 {
		return r.FlipMuteSec
	}
	return 1800
}

func (r ExecutionRules) withDefaults(from Rules) ExecutionRules {
	out := r
	if out.MinBuyUsd <= 0 && from.MinBuyUsd > 0 {
		out.MinBuyUsd = from.MinBuyUsd
	}
	if out.MaxMarketCapUsd <= 0 && from.MaxMarketCapUsd > 0 {
		out.MaxMarketCapUsd = from.MaxMarketCapUsd
	}
	if out.MinLiquidityUsd <= 0 && from.MinLiquidityUsd > 0 {
		out.MinLiquidityUsd = from.MinLiquidityUsd
	}
	return out
}

func (r ExecutionRules) PassesExecute(side string, tradeUsd float64, info enrich.TokenInfo) (bool, string) {
	if side != "buy" {
		return true, ""
	}
	if r.MinBuyUsd > 0 {
		if tradeUsd <= 0 {
			return false, "buy size unknown, below execution minBuyUsd"
		}
		if tradeUsd < r.MinBuyUsd {
			return false, fmt.Sprintf("buy $%.0f below minBuyUsd $%.0f", tradeUsd, r.MinBuyUsd)
		}
	}
	if r.MaxMarketCapUsd > 0 && info.MarketCap > r.MaxMarketCapUsd {
		return false, fmt.Sprintf("MC $%.0f above max $%.0f", info.MarketCap, r.MaxMarketCapUsd)
	}
	// Known thin pool: skip. Missing DexScreener pair (liq=0): allow as new launch.
	if r.MinLiquidityUsd > 0 && info.Liquidity > 0 && info.Liquidity < r.MinLiquidityUsd {
		return false, fmt.Sprintf("liq $%.0f below min $%.0f", info.Liquidity, r.MinLiquidityUsd)
	}
	return true, ""
}
