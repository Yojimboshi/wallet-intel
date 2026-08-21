// Buy a token for long-term hold — tokens land in collector wallet, not exec wallet.
//
//	go run ./scripts/hodl-buy -chain bsc -token 0x... -usd 50 -notes "perma hold"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/execute"
	"github.com/Yojimboshi/wallet-intel/internal/parse"
	"github.com/Yojimboshi/wallet-intel/internal/pool"
	"github.com/Yojimboshi/wallet-intel/internal/store"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	chainName := flag.String("chain", "bsc", "chain: bsc or ethereum")
	tokenStr := flag.String("token", "", "token contract address")
	usd := flag.Float64("usd", 50, "buy size in USD")
	walletLabel := flag.String("wallet", "evm-1", "execution wallet (pays BNB + gas)")
	collector := flag.String("collector", "", "token recipient (default: config/collectors.json evm)")
	notes := flag.String("notes", "hodl", "hodl notes")
	dry := flag.Bool("dry", false, "simulate only")
	flag.Parse()

	if *tokenStr == "" {
		log.Fatal("-token is required")
	}

	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	chainCfg, ok := chain.ByID(*chainName)
	if !ok {
		log.Fatalf("unsupported chain %q", *chainName)
	}

	collectors, err := config.LoadCollectors(config.CollectorsPath)
	if err != nil {
		log.Fatalf("collectors: %v", err)
	}
	collectorAddr := strings.TrimSpace(*collector)
	if collectorAddr == "" {
		collectorAddr = collectors.EVM
	}
	if collectorAddr == "" {
		log.Fatal("collector address required")
	}
	collectorHex := common.HexToAddress(collectorAddr)

	execCfg, err := cfg.ExecutionConfig(string(chainCfg.ID))
	if err != nil {
		log.Fatalf("execution: %v", err)
	}
	execCfg.SimulateSwaps = *dry
	execCfg.Provider = "direct"

	walletsFile, err := config.LoadExecutionWallets(config.ExecutionWalletsPath)
	if err != nil {
		log.Fatalf("wallets: %v", err)
	}
	w, err := walletsFile.Pick(*walletLabel, *chainName)
	if err != nil {
		log.Fatalf("wallet: %v", err)
	}

	readURL := cfg.ReadDialURLForChain(*chainName)
	execURL, err := cfg.ExecutionRPCURL(*chainName)
	if err != nil {
		log.Fatalf("exec rpc: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	readClient, err := ethclient.DialContext(ctx, readURL)
	if err != nil {
		log.Fatalf("read dial: %v", err)
	}
	defer readClient.Close()

	var execClient *ethclient.Client
	if !*dry {
		execClient, err = ethclient.DialContext(ctx, execURL)
		if err != nil {
			log.Fatalf("exec dial: %v", err)
		}
		defer execClient.Close()
	} else {
		execClient = readClient
	}

	oracle := pool.NewNativeOracle(readClient, chainCfg, execCfg.NativeUsdPrice)
	executor, err := execute.New(execClient, readClient, execCfg, string(chainCfg.ID), chainCfg, oracle)
	if err != nil {
		log.Fatalf("executor: %v", err)
	}

	token := common.HexToAddress(*tokenStr)
	enricher := enrich.NewClient(chainCfg)
	info, _ := enricher.LookupToken(ctx, token)

	fmt.Printf("hodl buy chain=%s token=%s usd=%.0f pay=%s receive=%s dry=%v\n",
		*chainName, token.Hex(), *usd, w.Label, collectorHex.Hex(), *dry)

	req := execute.Request{
		SourceLabel:    "hodl-buy",
		TokenRecipient: collectorHex,
		Trade: parse.Trade{
			Side:  "buy",
			Token: token,
		},
		Token:   info,
		Chain:   string(chainCfg.ID),
		SizeUsd: *usd,
		ExecWallet: execute.Wallet{
			Label:      w.Label,
			Address:    common.HexToAddress(w.Address),
			PrivateKey: w.PrivateKey,
		},
	}
	txHash, err := executor.Mirror(ctx, req)
	if err != nil {
		log.Fatalf("buy: %v", err)
	}
	if *dry {
		fmt.Println("simulate ok")
		return
	}

	var mysqlDB *store.MySQL
	if cfg.Database.Enabled {
		mysqlDB, err = store.OpenMySQL(cfg.Database.DSN(), cfg.Database.Location())
		if err != nil {
			log.Fatalf("mysql: %v", err)
		}
		defer mysqlDB.Close()
	}
	book, err := store.NewHodlBook("data/hodl.json")
	if err != nil {
		log.Fatalf("hodl book: %v", err)
	}
	book.UseMySQL(mysqlDB)

	pos := store.HodlPosition{
		Chain:         *chainName,
		Token:         token.Hex(),
		TokenSymbol:   info.Symbol,
		TokenName:     info.Name,
		EntryTx:       txHash.Hex(),
		EntryPriceUsd: info.PriceUsd,
		EntrySizeUsd:  *usd,
		ExecWallet:    collectorHex.Hex(),
		Notes:         *notes,
		OpenedAt:      time.Now().UTC(),
	}
	if err := book.Add(pos); err != nil {
		log.Fatalf("record hodl: %v", err)
	}

	events, err := store.NewEventLog("data/events.jsonl")
	if err != nil {
		log.Fatalf("events: %v", err)
	}
	events.UseMySQL(mysqlDB)
	sym := info.Symbol
	if sym == "" {
		sym = token.Hex()
	}
	_ = events.Append(store.Event{
		Type:    "hodl_open",
		Chain:   *chainName,
		Token:   token.Hex(),
		Symbol:  sym,
		SizeUsd: *usd,
		TxHash:  txHash.Hex(),
		Detail:  *notes + " → collector",
	})

	fmt.Printf("hodl tx=%s tokens in collector %s (exit monitor ignores this wallet)\n", txHash.Hex(), collectorHex.Hex())
}
