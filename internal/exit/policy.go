package exit

import (
	"fmt"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/store"
)

const (
	StageTP1 = 1 // sell partial at first multiple
	StageTP2 = 2 // sell partial at second multiple
	StageTP3 = 3 // transfer remainder to collector
)

type Signal struct {
	Reason               string
	CurrentUsd           float64
	PnLPct               float64
	Liquidity            float64
	MarketCap            float64
	SellFractionBps      int  // fraction of current wallet balance to sell
	KeepOpen             bool // stay open after partial exit
	TransferToCollector  bool // send full remaining balance to collector
	Stage                int  // 1=TP1, 2=TP2, 3=collector
}

// Evaluate checks open position against live market data and exit policy.
func Evaluate(pos store.Position, info enrich.TokenInfo, liqUsd float64, policy config.ExitPolicy, now time.Time) (Signal, bool) {
	policy = policy.WithDefaults()
	if liqUsd <= 0 {
		liqUsd = info.Liquidity
	}

	sig := Signal{
		CurrentUsd: info.PriceUsd,
		Liquidity:  liqUsd,
		MarketCap:  info.MarketCap,
	}

	var multiple float64
	if pos.EntryPriceUsd > 0 && info.PriceUsd > 0 {
		sig.PnLPct = (info.PriceUsd - pos.EntryPriceUsd) / pos.EntryPriceUsd * 100
		multiple = info.PriceUsd / pos.EntryPriceUsd
	}

	if pos.EntryPriceUsd > 0 && info.PriceUsd > 0 {
		if policy.StopLossPct > 0 && sig.PnLPct <= -policy.StopLossPct {
			sig.Reason = fmt.Sprintf("stop loss %.1f%% (limit -%.0f%%)", sig.PnLPct, policy.StopLossPct)
			return sig, true
		}

		if policy.StagedTakeProfit() {
			atTP2 := policy.TakeProfit2x > 0 && multiple >= policy.TakeProfit2x

			// Final slice → collector (after TP1+TP2 sells, or moon path).
			if atTP2 && pos.TP2Taken && policy.TakeProfit3CollectRemain {
				sig.TransferToCollector = true
				sig.Stage = StageTP3
				sig.Reason = fmt.Sprintf("take profit %.1fx — send remainder to collector", multiple)
				return sig, true
			}

			// TP2: sell half of what remains (= 25% of original after TP1).
			if atTP2 && pos.TP1Taken && !pos.TP2Taken {
				bps := policy.TakeProfit2SellBps
				sig.SellFractionBps = bps
				sig.KeepOpen = policy.TakeProfit3CollectRemain && bps > 0 && bps < 10000
				sig.Stage = StageTP2
				sig.Reason = fmt.Sprintf("take profit %.1fx sell %d%% of remainder (target %.1fx)", multiple, bps/100, policy.TakeProfit2x)
				return sig, true
			}

			// Moon: jumped straight to 10x without TP1 — sell 75%, then collector gets 25%.
			if atTP2 && !pos.TP1Taken && !pos.TP2Taken {
				sig.SellFractionBps = moonSellBps(policy)
				sig.KeepOpen = policy.TakeProfit3CollectRemain
				sig.Stage = StageTP2
				sig.Reason = fmt.Sprintf("take profit %.1fx sell %d%% (skipped TP1)", multiple, sig.SellFractionBps/100)
				return sig, true
			}

			// TP1: sell 50% at 3x.
			if !pos.TP1Taken && multiple >= policy.TakeProfit1x {
				bps := policy.TakeProfit1SellBps
				sig.SellFractionBps = bps
				sig.KeepOpen = bps > 0 && bps < 10000
				sig.Stage = StageTP1
				sig.Reason = fmt.Sprintf("take profit %.1fx sell %d%% (target %.1fx)", multiple, bps/100, policy.TakeProfit1x)
				return sig, true
			}
		} else if policy.TakeProfitPct > 0 && sig.PnLPct >= policy.TakeProfitPct {
			sig.Reason = fmt.Sprintf("take profit %.1f%% (target +%.0f%%)", sig.PnLPct, policy.TakeProfitPct)
			return sig, true
		}
	}

	if policy.MinLiquidityUsd > 0 && liqUsd > 0 && liqUsd < policy.MinLiquidityUsd {
		sig.Reason = fmt.Sprintf("liquidity $%.0f below floor $%.0f", liqUsd, policy.MinLiquidityUsd)
		return sig, true
	}
	if policy.MaxHoldHours > 0 {
		held := now.Sub(pos.OpenedAt)
		if held >= time.Duration(policy.MaxHoldHours*float64(time.Hour)) {
			sig.Reason = fmt.Sprintf("max hold %.1fh reached", held.Hours())
			return sig, true
		}
	}
	return Signal{}, false
}

// moonSellBps = TP1 + TP2 slices of original bag sold in one shot (default 50%+25%=75%).
func moonSellBps(policy config.ExitPolicy) int {
	policy = policy.WithDefaults()
	tp1 := policy.TakeProfit1SellBps
	tp2Remain := policy.TakeProfit2SellBps
	// remaining after tp1 * tp2Remain/10000 = tp1/10000 + (10000-tp1)/10000 * tp2Remain/10000 in bps
	afterTP1 := 10000 - tp1
	second := afterTP1 * tp2Remain / 10000
	total := tp1 + second
	if total > 10000 {
		return 10000
	}
	if total <= 0 {
		return 7500
	}
	return total
}
