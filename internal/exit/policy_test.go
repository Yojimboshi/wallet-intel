package exit

import (
	"testing"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/store"
)

func stagedPolicy() config.ExitPolicy {
	return config.ExitPolicy{
		TakeProfit1x:             3,
		TakeProfit2x:             10,
		TakeProfit1SellBps:       5000,
		TakeProfit2SellBps:       5000,
		TakeProfit3CollectRemain: true,
	}
}

func TestEvaluate_takeProfit(t *testing.T) {
	pos := store.Position{EntryPriceUsd: 1.0, OpenedAt: time.Now().UTC()}
	info := enrich.TokenInfo{PriceUsd: 2.1, Liquidity: 50_000}
	policy := config.ExitPolicy{TakeProfitPct: 100, StopLossPct: 30}

	sig, ok := Evaluate(pos, info, 0, policy, time.Now().UTC())
	if !ok {
		t.Fatal("expected take profit")
	}
	if sig.PnLPct < 100 {
		t.Fatalf("pnl %f", sig.PnLPct)
	}
}

func TestEvaluate_stagedTP_flow(t *testing.T) {
	policy := stagedPolicy()
	pos := store.Position{EntryPriceUsd: 1.0, OpenedAt: time.Now().UTC()}

	// TP1 @ 3x: sell 50%, keep open
	sig, ok := Evaluate(pos, enrich.TokenInfo{PriceUsd: 3.0, Liquidity: 50_000}, 0, policy, time.Now().UTC())
	if !ok || sig.Stage != StageTP1 || !sig.KeepOpen || sig.SellFractionBps != 5000 {
		t.Fatalf("tp1: %+v ok=%v", sig, ok)
	}

	pos.TP1Taken = true
	// between TP1 and TP2
	if _, ok := Evaluate(pos, enrich.TokenInfo{PriceUsd: 5.0, Liquidity: 50_000}, 0, policy, time.Now().UTC()); ok {
		t.Fatal("should wait between tp1 and tp2")
	}

	// TP2 @ 10x: sell 50% of remainder
	sig, ok = Evaluate(pos, enrich.TokenInfo{PriceUsd: 10.0, Liquidity: 50_000}, 0, policy, time.Now().UTC())
	if !ok || sig.Stage != StageTP2 || !sig.KeepOpen || sig.SellFractionBps != 5000 {
		t.Fatalf("tp2: %+v ok=%v", sig, ok)
	}

	pos.TP2Taken = true
	// TP3: collector
	sig, ok = Evaluate(pos, enrich.TokenInfo{PriceUsd: 10.0, Liquidity: 50_000}, 0, policy, time.Now().UTC())
	if !ok || sig.Stage != StageTP3 || !sig.TransferToCollector {
		t.Fatalf("tp3: %+v ok=%v", sig, ok)
	}
}

func TestEvaluate_moonSkipTP1(t *testing.T) {
	policy := stagedPolicy()
	pos := store.Position{EntryPriceUsd: 1.0, OpenedAt: time.Now().UTC()}

	sig, ok := Evaluate(pos, enrich.TokenInfo{PriceUsd: 12.0, Liquidity: 50_000}, 0, policy, time.Now().UTC())
	if !ok || sig.Stage != StageTP2 || sig.SellFractionBps != 7500 || !sig.KeepOpen {
		t.Fatalf("moon: %+v ok=%v", sig, ok)
	}

	pos.TP1Taken = true
	pos.TP2Taken = true
	sig, ok = Evaluate(pos, enrich.TokenInfo{PriceUsd: 12.0, Liquidity: 50_000}, 0, policy, time.Now().UTC())
	if !ok || !sig.TransferToCollector {
		t.Fatalf("moon collect: %+v ok=%v", sig, ok)
	}
}

func TestMoonSellBps(t *testing.T) {
	if got := moonSellBps(stagedPolicy()); got != 7500 {
		t.Fatalf("moonSellBps=%d want 7500", got)
	}
}

func TestEvaluate_stopLoss(t *testing.T) {
	pos := store.Position{EntryPriceUsd: 1.0, OpenedAt: time.Now().UTC()}
	info := enrich.TokenInfo{PriceUsd: 0.65, Liquidity: 50_000}
	policy := config.ExitPolicy{TakeProfitPct: 100, StopLossPct: 30}

	_, ok := Evaluate(pos, info, 0, policy, time.Now().UTC())
	if !ok {
		t.Fatal("expected stop loss")
	}
}

func TestEvaluate_liquidityFloor(t *testing.T) {
	pos := store.Position{EntryPriceUsd: 1.0, OpenedAt: time.Now().UTC()}
	info := enrich.TokenInfo{PriceUsd: 1.0, Liquidity: 3000}
	policy := config.ExitPolicy{MinLiquidityUsd: 5000}

	_, ok := Evaluate(pos, info, 0, policy, time.Now().UTC())
	if !ok {
		t.Fatal("expected liq floor exit")
	}
}
