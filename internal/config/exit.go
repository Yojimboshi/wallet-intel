package config

// ExitPolicy controls when and why we close copied positions.
type ExitPolicy struct {
	Enabled          bool    `json:"enabled"`
	MonitorOnBlock   bool    `json:"monitorOnBlock"`
	PollIntervalSec  int     `json:"pollIntervalSec"`
	TakeProfitPct    float64 `json:"takeProfitPct"` // legacy full-exit PnL% (used if TakeProfit1x unset)
	// Staged take-profit: 3x sell 50%, 10x sell 25% of original, remainder → collector.
	TakeProfit1x             float64 `json:"takeProfit1x"`
	TakeProfit2x             float64 `json:"takeProfit2x"`
	TakeProfit1SellBps       int     `json:"takeProfit1SellBps"`       // at TP1 (default 5000 = 50%)
	TakeProfit2SellBps       int     `json:"takeProfit2SellBps"`       // at TP2, fraction of remaining (default 5000 = half of bag left)
	TakeProfit3CollectRemain bool    `json:"takeProfit3CollectRemain"` // send final slice to collector (default true when staged)
	StopLossPct        float64 `json:"stopLossPct"`
	MinLiquidityUsd  float64 `json:"minLiquidityUsd"`
	MirrorWalletSell bool    `json:"mirrorWalletSell"`
	MaxHoldHours         float64 `json:"maxHoldHours"`
	SweepOnExit          bool    `json:"sweepOnExit"`
	MaxExitSlippagePolls int     `json:"maxExitSlippagePolls"`
}

func (e ExitPolicy) WithDefaults() ExitPolicy {
	if e.PollIntervalSec <= 0 {
		e.PollIntervalSec = 60
	}
	if e.MaxExitSlippagePolls <= 0 {
		e.MaxExitSlippagePolls = 20
	}
	if e.TakeProfit1x > 0 && e.TakeProfit1SellBps <= 0 {
		e.TakeProfit1SellBps = 5000
	}
	if e.TakeProfit1SellBps > 10000 {
		e.TakeProfit1SellBps = 10000
	}
	if e.TakeProfit2x > 0 && e.TakeProfit2SellBps <= 0 {
		e.TakeProfit2SellBps = 5000
	}
	if e.TakeProfit2SellBps > 10000 {
		e.TakeProfit2SellBps = 10000
	}
	if e.TakeProfit1x > 0 && e.TakeProfit2x > 0 && !e.TakeProfit3CollectRemain {
		// default: final 25% goes to collector when staged TP is configured
		e.TakeProfit3CollectRemain = true
	}
	return e
}

func (e ExitPolicy) UseBlockMonitor() bool {
	return e.MonitorOnBlock || e.PollIntervalSec <= 0
}

func (e ExitPolicy) EnabledForMonitor() bool {
	return e.Enabled && (e.TakeProfitPct > 0 || e.TakeProfit1x > 0 || e.TakeProfit2x > 0 ||
		e.StopLossPct > 0 || e.MinLiquidityUsd > 0 || e.MaxHoldHours > 0)
}

// StagedTakeProfit is true when multi-level TP multiples are configured.
func (e ExitPolicy) StagedTakeProfit() bool {
	return e.TakeProfit1x > 0
}
