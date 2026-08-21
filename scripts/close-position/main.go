// Close an open position in positions.json (+ MySQL when enabled).
//
//	go run ./scripts/close-position -chain bsc -token 0x... -reason "manual sell"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/store"
)

func main() {
	chainName := flag.String("chain", "bsc", "chain id")
	tokenStr := flag.String("token", "", "token address")
	reason := flag.String("reason", "manual close", "exit reason")
	flag.Parse()
	if *tokenStr == "" {
		log.Fatal("-token required")
	}

	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Fatalf("config: %v", err)
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

	if err := positions.Close(*chainName, *tokenStr, *reason); err != nil {
		log.Fatalf("close: %v", err)
	}
	_ = positions.ResolveManualIntervention(*chainName, *tokenStr)

	events, err := store.NewEventLog("data/events.jsonl")
	if err != nil {
		log.Fatalf("events: %v", err)
	}
	events.UseMySQL(mysqlDB)
	_ = events.Append(store.Event{
		Type:   "position_close",
		Chain:  *chainName,
		Token:  *tokenStr,
		Symbol: "APARTMENT",
		Reason: *reason,
	})

	fmt.Println("ok")
	_ = context.Background()
}
