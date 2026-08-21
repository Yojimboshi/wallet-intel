// Record a discretionary hodl position (no exit monitor).
//
//	go run ./scripts/record-hodl -chain bsc -token 0x... -entry-tx 0x... -size-usd 50 -notes "perma hold"
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
	"github.com/Yojimboshi/wallet-intel/internal/store"
	"github.com/ethereum/go-ethereum/common"
)

func main() {
	chainName := flag.String("chain", "bsc", "chain id")
	tokenStr := flag.String("token", "", "token address")
	entryTx := flag.String("entry-tx", "", "buy tx hash")
	sizeUsd := flag.Float64("size-usd", 50, "buy size USD")
	execWallet := flag.String("exec-wallet", "", "execution wallet address")
	notes := flag.String("notes", "hodl", "optional note")
	flag.Parse()

	if *tokenStr == "" || *entryTx == "" {
		log.Fatal("-token and -entry-tx required")
	}

	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	chainCfg, ok := chain.ByID(*chainName)
	if !ok {
		log.Fatalf("unsupported chain %q", *chainName)
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
		log.Fatalf("hodl: %v", err)
	}
	book.UseMySQL(mysqlDB)

	ctx := context.Background()
	token := common.HexToAddress(*tokenStr)
	info, _ := enrich.NewClient(chainCfg).LookupToken(ctx, token)

	collectors, _ := config.LoadCollectors(config.CollectorsPath)
	execAddr := strings.TrimSpace(*execWallet)
	if execAddr == "" {
		execAddr = collectors.EVM
	}

	pos := store.HodlPosition{
		Chain:         *chainName,
		Token:         token.Hex(),
		TokenSymbol:   info.Symbol,
		TokenName:     info.Name,
		EntryTx:       *entryTx,
		EntryPriceUsd: info.PriceUsd,
		EntrySizeUsd:  *sizeUsd,
		ExecWallet:    execAddr,
		Notes:         *notes,
		OpenedAt:      time.Now().UTC(),
	}
	if err := book.Add(pos); err != nil {
		log.Fatalf("add: %v", err)
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
		SizeUsd: *sizeUsd,
		TxHash:  *entryTx,
		Detail:  *notes,
	})

	fmt.Printf("hodl recorded %s %s $%.0f\n", *chainName, sym, *sizeUsd)
}
