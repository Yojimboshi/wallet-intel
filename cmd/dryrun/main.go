// Dry-run harness: ping RPC endpoints and simulate copy scenarios without broadcasting txs.
//
// Usage:
//
//	go run ./cmd/dryrun
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/alerts"
	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/execute"
	"github.com/Yojimboshi/wallet-intel/internal/parse"
	"github.com/Yojimboshi/wallet-intel/internal/safety"
	"github.com/Yojimboshi/wallet-intel/internal/store"
	"github.com/Yojimboshi/wallet-intel/internal/watch"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	dummyWallet1 = "0x000000000000000000000000000000000000a001"
	dummyWallet2 = "0x000000000000000000000000000000000000a002"
	dummyToken   = "0x000000000000000000000000000000000000c0de"
)

func main() {
	log.SetFlags(log.LstdFlags)
	log.Println("=== wallet-intel dry-run ===")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Printf("config: %v (using defaults for RPC-only checks)", err)
	}

	chains := []chain.Config{chain.EthereumMainnet, chain.BSCMainnet}
	if err == nil {
		chains = nil
		for _, id := range cfg.Rules.Chains {
			if c, ok := chain.ByID(id); ok {
				chains = append(chains, c)
			}
		}
	}

	fmt.Println("\n--- RPC connectivity ---")
	if err != nil {
		log.Println("skip RPC pings (config/local.json not loaded — no embedded keys)")
	}
	for _, c := range chains {
		readURL := defaultReadURL(cfg, string(c.ID), err == nil)
		execURL := defaultExecURL(cfg, string(c.ID), err == nil)
		if readURL == "" {
			log.Printf("skip read %s (no rpc configured)", c.ID)
			continue
		}
		if err := pingRPC(ctx, readURL); err != nil {
			log.Printf("FAIL read %s (%s): %v", c.ID, redact(readURL), err)
		} else {
			log.Printf("OK   read %s (%s)", c.ID, redact(readURL))
		}
		if execURL != "" {
			if err := pingRPC(ctx, execURL); err != nil {
				log.Printf("FAIL exec %s (%s): %v", c.ID, redact(execURL), err)
			} else {
				log.Printf("OK   exec %s (%s)", c.ID, redact(execURL))
			}
		}
	}

	fmt.Println("\n--- Scenario simulations (no broadcast) ---")
	dir, err := os.MkdirTemp("", "wallet-intel-dryrun-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	rules := config.ExecutionRules{
		MinBuyUsd:           500,
		MaxMarketCapUsd:     5_000_000,
		MinLiquidityUsd:     10_000,
		RequireMcLiq:        true,
		FirstBuyOnly:        true,
		DedupeAcrossWallets: true,
	}
	if err == nil {
		execCfg, e := cfg.ExecutionConfig("ethereum")
		if e == nil {
			rules = execCfg.ExecRules
		}
	}

	scenarios := []struct {
		name   string
		chain  chain.Config
		run    func(context.Context, chain.Config, config.ExecutionRules, string) error
	}{
		{"eth_copy_first_wallet", chain.EthereumMainnet, runCopyScenario},
		{"bsc_copy_first_wallet", chain.BSCMainnet, runCopyScenario},
		{"eth_dedupe_second_wallet", chain.EthereumMainnet, runDedupeScenario},
		{"bsc_dedupe_second_wallet", chain.BSCMainnet, runDedupeScenario},
		{"eth_block_small_buy", chain.EthereumMainnet, runSmallBuyBlocked},
	}

	for _, sc := range scenarios {
		seenPath := filepath.Join(dir, sc.name+"-seen.json")
		logPath := filepath.Join(dir, sc.name+"-trades.jsonl")
		if err := sc.run(ctx, sc.chain, rules, seenPath); err != nil {
			log.Printf("FAIL %s: %v", sc.name, err)
		} else {
			log.Printf("PASS %s (logs: %s)", sc.name, logPath)
		}
	}

	fmt.Println("\n--- GoPlus safety (WBNB on BSC, should pass) ---")
	if err := liveSafetyCheck(ctx, chain.BSCMainnet, "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c"); err != nil {
		log.Printf("FAIL safety wbnb: %v", err)
	} else {
		log.Printf("PASS safety wbnb")
	}

	fmt.Println("\n=== dry-run complete ===")
}

func runCopyScenario(ctx context.Context, chainCfg chain.Config, rules config.ExecutionRules, seenPath string) error {
	w, dry, err := newDryWatcher(chainCfg, rules, seenPath)
	if err != nil {
		return err
	}
	if err := w.HandleTrade(ctx, buyTrade(dummyWallet1, "w1", 600, chainCfg)); err != nil {
		return err
	}
	if dry.Count() != 1 {
		return fmt.Errorf("expected 1 copy, got %d", dry.Count())
	}
	return nil
}

func runDedupeScenario(ctx context.Context, chainCfg chain.Config, rules config.ExecutionRules, seenPath string) error {
	w, dry, err := newDryWatcher(chainCfg, rules, seenPath)
	if err != nil {
		return err
	}
	_ = w.HandleTrade(ctx, buyTrade(dummyWallet1, "w1", 600, chainCfg))
	_ = w.HandleTrade(ctx, buyTrade(dummyWallet2, "w2", 600, chainCfg))
	if dry.Count() != 1 {
		return fmt.Errorf("expected 1 copy after dedupe, got %d", dry.Count())
	}
	return nil
}

func runSmallBuyBlocked(ctx context.Context, chainCfg chain.Config, rules config.ExecutionRules, seenPath string) error {
	w, dry, err := newDryWatcher(chainCfg, rules, seenPath)
	if err != nil {
		return err
	}
	if err := w.HandleTrade(ctx, buyTrade(dummyWallet1, "w1", 50, chainCfg)); err != nil {
		return err
	}
	if dry.Count() != 0 {
		return fmt.Errorf("expected 0 copies, got %d", dry.Count())
	}
	return nil
}

func newDryWatcher(chainCfg chain.Config, rules config.ExecutionRules, seenPath string) (*watch.Watcher, *execute.DryRun, error) {
	dir := filepath.Dir(seenPath)
	tradeLog, err := store.NewJSONL(filepath.Join(dir, "trades.jsonl"))
	if err != nil {
		return nil, nil, err
	}
	seen, err := store.NewSeenTokens(seenPath)
	if err != nil {
		return nil, nil, err
	}
	execCfg := config.ExecutionConfig{
		AllowLiveExecution: true,
		MaxExecuteUsd:      500,
		ExecRules:          rules,
	}
	dry := execute.NewDryRun(execCfg, string(chainCfg.ID))
	wallets := []config.WatchedWallet{
		{Address: common.HexToAddress(dummyWallet1), Label: "w1", Copy: true},
		{Address: common.HexToAddress(dummyWallet2), Label: "w2", Copy: true},
	}
	w := watch.New(
		nil,
		chainCfg,
		wallets,
		config.Rules{AlertOn: []string{"buy"}, MinBuyUsd: 500, MaxMarketCapUsd: 5_000_000, MinLiquidityUsd: 10_000},
		execCfg,
		dry,
		safety.Nop{},
		seen,
		nil,
		nil,
		nil,
		enrich.StaticLookup{Info: enrich.TokenInfo{Symbol: "TEST", MarketCap: 1e6, Liquidity: 5e4, PriceUsd: 0.01, Decimals: 18}},
		alerts.Logger{},
		tradeLog,
		nil,
		store.NewBatchBuyTracker(),
	)
	return w, dry, nil
}

func buyTrade(wallet, label string, usd float64, chainCfg chain.Config) parse.Trade {
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
		Wallet:      common.HexToAddress(wallet),
		WalletLabel: label,
		Side:        "buy",
		Token:       common.HexToAddress(dummyToken),
		TokenAmount: amount,
		QuoteToken:  quoteToken,
		QuoteAmount: quote,
		QuoteSymbol: quoteSym,
	}
}

func pingRPC(ctx context.Context, url string) error {
	if url == "" {
		return fmt.Errorf("empty url")
	}
	client, err := ethclient.DialContext(ctx, url)
	if err != nil {
		return err
	}
	defer client.Close()
	n, err := client.BlockNumber(ctx)
	if err != nil {
		return err
	}
	log.Printf("       block=%d", n)
	return nil
}

func liveSafetyCheck(ctx context.Context, chainCfg chain.Config, tokenHex string) error {
	checker := safety.New(safety.Config{
		Enabled:       true,
		MaxBuyTaxPct:  10,
		MaxSellTaxPct: 10,
		BlockHoneypot: true,
		BlockMintable: true,
		FailClosed:    true,
	})
	result, err := checker.Check(ctx, chainCfg, common.HexToAddress(tokenHex))
	if err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("safety not ok: %s", result.Reason)
	}
	b, _ := json.Marshal(result)
	log.Printf("       %s", b)
	return nil
}

func defaultReadURL(cfg config.Local, chainID string, loaded bool) string {
	if !loaded {
		return ""
	}
	return cfg.ReadDialURLForChain(chainID)
}

func defaultExecURL(cfg config.Local, chainID string, loaded bool) string {
	if !loaded {
		return ""
	}
	u, err := cfg.ExecutionRPCURL(chainID)
	if err != nil {
		return ""
	}
	return u
}

func redact(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		host := u[i+3:]
		if j := strings.Index(host, "/"); j >= 0 {
			host = host[:j]
		}
		return host
	}
	return u
}
