package watch

import (
	"context"
	"fmt"
	"math/big"
	"path/filepath"
	"testing"

	"github.com/Yojimboshi/wallet-intel/internal/alerts"
	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/execute"
	"github.com/Yojimboshi/wallet-intel/internal/parse"
	"github.com/Yojimboshi/wallet-intel/internal/safety"
	"github.com/Yojimboshi/wallet-intel/internal/store"
	"github.com/ethereum/go-ethereum/common"
)

const (
	dummyWallet1 = "0x000000000000000000000000000000000000a001"
	dummyWallet2 = "0x000000000000000000000000000000000000a002"
	dummyToken   = "0x000000000000000000000000000000000000c0de"
)

func goodTokenInfo() enrich.TokenInfo {
	return enrich.TokenInfo{
		Symbol:    "TEST",
		MarketCap: 1_000_000,
		Liquidity: 50_000,
		PriceUsd:  0.01,
		Decimals:  18,
	}
}

func testHarness(t *testing.T, chainCfg chain.Config, execRules config.ExecutionRules) (*Watcher, *execute.DryRun, *store.SeenTokens) {
	t.Helper()
	dir := t.TempDir()
	tradeLog, err := store.NewJSONL(filepath.Join(dir, "trades.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	seen, err := store.NewSeenTokens(filepath.Join(dir, "seen.json"))
	if err != nil {
		t.Fatal(err)
	}

	w1 := common.HexToAddress(dummyWallet1)
	w2 := common.HexToAddress(dummyWallet2)
	wallets := []config.WatchedWallet{
		{Address: w1, Label: "w1", Copy: true},
		{Address: w2, Label: "w2", Copy: true},
	}

	execCfg := config.ExecutionConfig{
		AllowLiveExecution: true,
		MinExecuteUsd:      50,
		MaxExecuteUsd:      500,
		ExecRules:          execRules,
	}
	dry := execute.NewDryRun(execCfg, string(chainCfg.ID))

	w := New(
		nil,
		chainCfg,
		wallets,
		config.Rules{
			AlertOn:         []string{"buy", "sell"},
			MinBuyUsd:       500,
			MaxMarketCapUsd: 5_000_000,
			MinLiquidityUsd: 10_000,
		},
		execCfg,
		dry,
		safety.Nop{},
		seen,
		nil,
		nil,
		nil,
		enrich.StaticLookup{Info: goodTokenInfo()},
		alerts.Logger{},
		tradeLog,
		nil,
		store.NewBatchBuyTracker(),
	)
	return w, dry, seen
}

func buyTrade(wallet, label string, usd float64, chainCfg chain.Config) parse.Trade {
	w := common.HexToAddress(wallet)
	quote := big.NewInt(int64(usd * 1e6))
	quoteToken := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	quoteSym := "USDC"
	if chainCfg.ID == chain.BSC {
		quoteToken = common.HexToAddress("0x55d398326f99059fF775485246999027B3197955")
		quote = new(big.Int).Mul(big.NewInt(int64(usd)), big.NewInt(1e18))
		quoteSym = "USDT"
	}
	amount := big.NewInt(int64(usd * 100))
	amount.Mul(amount, big.NewInt(1e16))
	return parse.Trade{
		Wallet:      w,
		WalletLabel: label,
		Side:        "buy",
		Token:       common.HexToAddress(dummyToken),
		TokenAmount: amount,
		QuoteToken:  quoteToken,
		QuoteAmount: quote,
		QuoteSymbol: quoteSym,
	}
}

func TestScenario_BatchBuyFiresAlertAndCopy(t *testing.T) {
	rules := config.ExecutionRules{
		MinBuyUsd:    500,
		RequireMcLiq: true,
		FirstBuyOnly: true,
	}
	w, dry, _ := testHarness(t, chain.BSCMainnet, rules)
	ctx := context.Background()

	_ = w.HandleTrade(ctx, buyTrade(dummyWallet1, "w1", 150, chain.BSCMainnet))
	_ = w.HandleTrade(ctx, buyTrade(dummyWallet1, "w1", 150, chain.BSCMainnet))
	if dry.Count() != 0 {
		t.Fatalf("expected no copy before batch threshold, got %d", dry.Count())
	}

	if err := w.HandleTrade(ctx, buyTrade(dummyWallet1, "w1", 220, chain.BSCMainnet)); err != nil {
		t.Fatal(err)
	}
	if dry.Count() != 1 {
		t.Fatalf("expected 1 copy after batch total >= 500, got %d", dry.Count())
	}
}

func TestScenario_BatchBuyDoesNotSumAcrossWallets(t *testing.T) {
	rules := config.ExecutionRules{MinBuyUsd: 500, RequireMcLiq: true, FirstBuyOnly: true}
	w, dry, _ := testHarness(t, chain.BSCMainnet, rules)
	ctx := context.Background()

	_ = w.HandleTrade(ctx, buyTrade(dummyWallet1, "w1", 300, chain.BSCMainnet))
	_ = w.HandleTrade(ctx, buyTrade(dummyWallet2, "w2", 300, chain.BSCMainnet))
	if dry.Count() != 0 {
		t.Fatalf("expected no copy when each wallet is below threshold, got %d", dry.Count())
	}
}

func TestScenario_ClusterBatchFiresAcrossWallets(t *testing.T) {
	rules := config.ExecutionRules{MinBuyUsd: 500, RequireMcLiq: true, FirstBuyOnly: true}
	w, dry, _ := testHarness(t, chain.BSCMainnet, rules)
	ctx := context.Background()

	tx := common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	tr1 := buyTrade(dummyWallet1, "w1", 300, chain.BSCMainnet)
	tr1.TxHash = tx
	tr2 := buyTrade(dummyWallet2, "w2", 250, chain.BSCMainnet)
	tr2.TxHash = tx

	clusters := store.BuildTxClusterBuys([]store.TxClusterLeg{
		{Token: tr1.Token, Wallet: tr1.Wallet, Side: "buy", TradeUsd: 300},
		{Token: tr2.Token, Wallet: tr2.Wallet, Side: "buy", TradeUsd: 250},
	})

	if err := w.handleTrade(ctx, tr1, clusters); err != nil {
		t.Fatal(err)
	}
	if err := w.handleTrade(ctx, tr2, clusters); err != nil {
		t.Fatal(err)
	}
	if dry.Count() != 2 {
		t.Fatalf("expected 2 copies after cluster batch >= 500, got %d", dry.Count())
	}
}

func TestScenario_CopyOnEthereum(t *testing.T) {
	rules := config.ExecutionRules{
		MinBuyUsd:           500,
		MaxMarketCapUsd:     5_000_000,
		MinLiquidityUsd:     10_000,
		RequireMcLiq:        true,
		FirstBuyOnly:        true,
		DedupeAcrossWallets: true,
	}
	w, dry, _ := testHarness(t, chain.EthereumMainnet, rules)
	ctx := context.Background()

	if err := w.HandleTrade(ctx, buyTrade(dummyWallet1, "w1", 600, chain.EthereumMainnet)); err != nil {
		t.Fatal(err)
	}
	if dry.Count() != 1 {
		t.Fatalf("expected 1 dry-run copy, got %d", dry.Count())
	}
}

func TestScenario_DedupeSecondWalletSameToken(t *testing.T) {
	rules := config.ExecutionRules{
		MinBuyUsd:           500,
		RequireMcLiq:        true,
		FirstBuyOnly:        true,
		DedupeAcrossWallets: true,
	}
	w, dry, _ := testHarness(t, chain.BSCMainnet, rules)
	ctx := context.Background()

	_ = w.HandleTrade(ctx, buyTrade(dummyWallet1, "w1", 600, chain.BSCMainnet))
	_ = w.HandleTrade(ctx, buyTrade(dummyWallet2, "w2", 600, chain.BSCMainnet))

	if dry.Count() != 1 {
		t.Fatalf("expected 1 copy after dedupe, got %d", dry.Count())
	}
}

func TestScenario_BlockSmallBuyAlert(t *testing.T) {
	rules := config.ExecutionRules{MinBuyUsd: 500, RequireMcLiq: true, FirstBuyOnly: true}
	w, dry, _ := testHarness(t, chain.EthereumMainnet, rules)
	ctx := context.Background()

	tr := buyTrade(dummyWallet1, "w1", 100, chain.EthereumMainnet) // $100 < minBuyUsd
	if err := w.HandleTrade(ctx, tr); err != nil {
		t.Fatal(err)
	}
	if dry.Count() != 0 {
		t.Fatalf("expected no copy on small buy alert skip, got %d", dry.Count())
	}
}

func TestScenario_AllowUnknownLiqForExecution(t *testing.T) {
	dir := t.TempDir()
	tradeLog, _ := store.NewJSONL(filepath.Join(dir, "trades.jsonl"))
	seen, _ := store.NewSeenTokens(filepath.Join(dir, "seen.json"))
	execCfg := config.ExecutionConfig{
		AllowLiveExecution: true,
		MaxExecuteUsd:      500,
		ExecRules: config.ExecutionRules{
			MinBuyUsd:       450,
			MinLiquidityUsd: 20_000,
			RequireMcLiq:    true,
			FirstBuyOnly:    true,
		},
	}
	dry := execute.NewDryRun(execCfg, "ethereum")
	w := New(
		nil,
		chain.EthereumMainnet,
		[]config.WatchedWallet{{Address: common.HexToAddress(dummyWallet1), Label: "w1", Copy: true}},
		config.Rules{AlertOn: []string{"buy"}, MinBuyUsd: 450, MinLiquidityUsd: 20_000},
		execCfg,
		dry,
		safety.Nop{},
		seen,
		nil,
		nil,
		nil,
		enrich.StaticLookup{Info: enrich.TokenInfo{}}, // unlisted: no DexScreener pair
		alerts.Logger{},
		tradeLog,
		nil,
		nil,
	)

	_ = w.HandleTrade(context.Background(), buyTrade(dummyWallet1, "w1", 496, chain.EthereumMainnet))
	if dry.Count() != 1 {
		t.Fatalf("expected copy on unlisted launch, got %d", dry.Count())
	}
}

func TestScenario_BlockThinKnownLiqForExecution(t *testing.T) {
	dir := t.TempDir()
	tradeLog, _ := store.NewJSONL(filepath.Join(dir, "trades.jsonl"))
	seen, _ := store.NewSeenTokens(filepath.Join(dir, "seen.json"))
	execCfg := config.ExecutionConfig{
		AllowLiveExecution: true,
		MaxExecuteUsd:      500,
		ExecRules: config.ExecutionRules{
			MinBuyUsd:       450,
			MinLiquidityUsd: 20_000,
			FirstBuyOnly:    true,
		},
	}
	dry := execute.NewDryRun(execCfg, "ethereum")
	w := New(
		nil,
		chain.EthereumMainnet,
		[]config.WatchedWallet{{Address: common.HexToAddress(dummyWallet1), Label: "w1", Copy: true}},
		config.Rules{AlertOn: []string{"buy"}, MinBuyUsd: 450, MinLiquidityUsd: 20_000},
		execCfg,
		dry,
		safety.Nop{},
		seen,
		nil,
		nil,
		nil,
		enrich.StaticLookup{Info: enrich.TokenInfo{Symbol: "THIN", Liquidity: 8_000, MarketCap: 100_000, PriceUsd: 0.01}},
		alerts.Logger{},
		tradeLog,
		nil,
		nil,
	)

	_ = w.HandleTrade(context.Background(), buyTrade(dummyWallet1, "w1", 600, chain.EthereumMainnet))
	if dry.Count() != 0 {
		t.Fatalf("expected copy blocked on known thin liq, got %d", dry.Count())
	}
}

func TestScenario_CopyDisabledWallet(t *testing.T) {
	dir := t.TempDir()
	tradeLog, _ := store.NewJSONL(filepath.Join(dir, "trades.jsonl"))
	seen, _ := store.NewSeenTokens(filepath.Join(dir, "seen.json"))
	execCfg := config.ExecutionConfig{AllowLiveExecution: true, MaxExecuteUsd: 500, ExecRules: config.ExecutionRules{RequireMcLiq: true}}
	dry := execute.NewDryRun(execCfg, "ethereum")
	w := New(
		nil,
		chain.EthereumMainnet,
		[]config.WatchedWallet{{Address: common.HexToAddress(dummyWallet1), Label: "w1", Copy: false}},
		config.Rules{AlertOn: []string{"buy"}},
		execCfg,
		dry,
		safety.Nop{},
		seen,
		nil,
		nil,
		nil,
		enrich.StaticLookup{Info: goodTokenInfo()},
		alerts.Logger{},
		tradeLog,
		nil,
		nil,
	)

	_ = w.HandleTrade(context.Background(), buyTrade(dummyWallet1, "w1", 600, chain.EthereumMainnet))
	if dry.Count() != 0 {
		t.Fatalf("expected no copy when copy:false, got %d", dry.Count())
	}
}

func TestScenario_MaxOpenPositions(t *testing.T) {
	dir := t.TempDir()
	tradeLog, _ := store.NewJSONL(filepath.Join(dir, "trades.jsonl"))
	seen, _ := store.NewSeenTokens(filepath.Join(dir, "seen.json"))
	positions, err := store.NewPositions(filepath.Join(dir, "positions.json"))
	if err != nil {
		t.Fatal(err)
	}
	execCfg := config.ExecutionConfig{
		AllowLiveExecution: true,
		CopyUsd:            50,
		MaxOpenPositions:   2,
		ExecRules: config.ExecutionRules{
			MinBuyUsd:    500,
			RequireMcLiq: true,
			FirstBuyOnly: true,
		},
	}
	dry := execute.NewDryRun(execCfg, "ethereum")
	w := New(
		nil,
		chain.EthereumMainnet,
		[]config.WatchedWallet{{Address: common.HexToAddress(dummyWallet1), Label: "w1", Copy: true}},
		config.Rules{AlertOn: []string{"buy"}},
		execCfg,
		dry,
		safety.Nop{},
		seen,
		positions,
		nil,
		nil,
		enrich.StaticLookup{Info: goodTokenInfo()},
		alerts.Logger{},
		tradeLog,
		nil,
		nil,
	)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		token := common.HexToAddress(fmt.Sprintf("0x000000000000000000000000000000000000%04x", i+1))
		tr := buyTrade(dummyWallet1, "w1", 600, chain.EthereumMainnet)
		tr.Token = token
		if err := w.HandleTrade(ctx, tr); err != nil {
			t.Fatal(err)
		}
	}
	if dry.Count() != 2 {
		t.Fatalf("expected 2 copies, got %d", dry.Count())
	}

	tr := buyTrade(dummyWallet1, "w1", 600, chain.EthereumMainnet)
	tr.Token = common.HexToAddress("0x0000000000000000000000000000000000009999")
	_ = w.HandleTrade(ctx, tr)
	if dry.Count() != 2 {
		t.Fatalf("expected copy blocked at max open positions, got %d", dry.Count())
	}
}
