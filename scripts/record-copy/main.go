// Record a manual copy in positions, seen_tokens, trades, events, and trade_decisions.
//
//	go run ./scripts/record-copy -chain bsc -token 0x... -entry-tx 0x... -source-wallet 0x... -source-label fm-0121
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/parse"
	"github.com/Yojimboshi/wallet-intel/internal/store"
	"github.com/ethereum/go-ethereum/common"
)

func main() {
	chainName := flag.String("chain", "bsc", "chain id")
	tokenStr := flag.String("token", "", "token address")
	entryTx := flag.String("entry-tx", "", "our copy tx hash")
	sourceWallet := flag.String("source-wallet", "", "watched wallet that triggered copy")
	sourceLabel := flag.String("source-label", "", "watched wallet label")
	execWallet := flag.String("exec-wallet", "", "execution wallet address")
	sizeUsd := flag.Float64("size-usd", 50, "copy size USD")
	sourceTx := flag.String("source-tx", "", "optional watched-wallet signal tx")
	block := flag.Uint64("block", 0, "optional block of source tx")
	dedupeGlobal := flag.Bool("dedupe-global", true, "mark seen with chain-wide token dedupe")
	flag.Parse()

	if *tokenStr == "" || *entryTx == "" || *sourceWallet == "" || *sourceLabel == "" {
		log.Fatal("-token, -entry-tx, -source-wallet, -source-label required")
	}

	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	chainCfg, ok := chain.ByID(*chainName)
	if !ok {
		log.Fatalf("unsupported chain %q", *chainName)
	}
	execCfg, err := cfg.ExecutionConfig(string(chainCfg.ID))
	if err != nil {
		log.Fatalf("execution: %v", err)
	}

	ctx := context.Background()
	enricher := enrich.NewClient(chainCfg)
	token := common.HexToAddress(*tokenStr)
	info, err := enricher.LookupToken(ctx, token)
	if err != nil {
		log.Printf("enrich: %v (continuing with partial info)", err)
	}

	var mysqlDB *store.MySQL
	if cfg.Database.Enabled {
		mysqlDB, err = store.OpenMySQL(cfg.Database.DSN(), cfg.Database.Location())
		if err != nil {
			log.Fatalf("mysql: %v", err)
		}
		defer mysqlDB.Close()
	}

	positions, err := store.NewPositions("data/positions.json")
	if err != nil {
		log.Fatalf("positions: %v", err)
	}
	if err := positions.UseMySQL(mysqlDB); err != nil {
		log.Fatalf("positions mysql: %v", err)
	}

	seen, err := store.NewSeenTokens("data/seen-tokens.json")
	if err != nil {
		log.Fatalf("seen: %v", err)
	}
	if err := seen.UseMySQL(mysqlDB); err != nil {
		log.Fatalf("seen mysql: %v", err)
	}

	tradeLog, err := store.NewJSONL("data/trades.jsonl")
	if err != nil {
		log.Fatalf("trades: %v", err)
	}
	tradeLog.UseMySQL(mysqlDB)

	events, err := store.NewEventLog("data/events.jsonl")
	if err != nil {
		log.Fatalf("events: %v", err)
	}
	events.UseMySQL(mysqlDB)

	chainKey := string(chainCfg.ID)
	signalTx := *sourceTx
	if signalTx == "" {
		signalTx = *entryTx
	}

	tr := parse.Trade{
		Wallet:      common.HexToAddress(*sourceWallet),
		WalletLabel: *sourceLabel,
		Side:        "buy",
		Token:       token,
		TxHash:      common.HexToHash(signalTx),
		BlockNumber: *block,
		DEX:         "uniswap-v2",
	}
	tradeUsd := parse.TradeUsd(tr, info, chainCfg, 0)
	if tradeUsd <= 0 {
		tradeUsd = *sizeUsd
	}

	pair := info.PairAddress
	pos := store.Position{
		Chain:             chainKey,
		Token:             token.Hex(),
		TokenSymbol:       info.Symbol,
		TokenName:         info.Name,
		Pair:              pair,
		DEX:               "uniswap-v2",
		SourceWallet:      *sourceWallet,
		SourceLabel:       *sourceLabel,
		ExecWallet:        *execWallet,
		EntryTx:           *entryTx,
		EntryPriceUsd:     info.PriceUsd,
		EntrySizeUsd:      *sizeUsd,
		EntryLiquidityUsd: info.Liquidity,
		LastLiquidityUsd:  info.Liquidity,
		OpenedAt:          time.Now().UTC(),
		Status:            store.PositionOpen,
	}
	if err := positions.Open(pos); err != nil {
		log.Fatalf("open position: %v", err)
	}
	fmt.Printf("position open %s %s $%.0f\n", chainKey, info.Symbol, *sizeUsd)

	global := *dedupeGlobal && execCfg.ExecRules.DedupeAcrossWallets
	if err := seen.MarkCopy(chainKey, *sourceWallet, token.Hex(), global); err != nil {
		log.Fatalf("seen: %v", err)
	}
	fmt.Printf("seen_tokens marked (global=%v)\n", global)

	if err := tradeLog.Append(chainKey, tr, info); err != nil {
		log.Fatalf("trade log: %v", err)
	}
	fmt.Println("trades recorded")

	d := store.TradeDecisionFrom(tr, info, chainKey, tradeUsd, tradeUsd, 0)
	d.AlertAction = "follow"
	d.AlertReason = "manual record-copy"
	d.CopyAction = "follow"
	d.CopyReason = "manual test-swap"
	d.CopySizeUsd = *sizeUsd
	if mysqlDB != nil {
		if err := mysqlDB.InsertTradeDecision(ctx, d); err != nil {
			log.Fatalf("trade_decision: %v", err)
		}
	}
	fmt.Println("trade_decisions recorded")

	sym := info.Symbol
	if sym == "" {
		sym = token.Hex()
	}
	if err := events.Append(store.Event{
		Type:        "position_open",
		Chain:       chainKey,
		Token:       token.Hex(),
		Symbol:      sym,
		Side:        "buy",
		Wallet:      *sourceWallet,
		WalletLabel: *sourceLabel,
		SizeUsd:     *sizeUsd,
		TxHash:      *entryTx,
		Detail:      "manual record-copy",
	}); err != nil {
		log.Fatalf("event: %v", err)
	}
	fmt.Println("events recorded")
	fmt.Println("ok")
}
