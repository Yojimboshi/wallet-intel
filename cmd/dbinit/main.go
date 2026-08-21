package main

import (
	"context"
	"log"

	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[dbinit] ")

	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if !cfg.Database.Enabled {
		log.Fatal("database.enabled is false in config/local.json")
	}

	db, err := store.OpenMySQL(cfg.Database.DSN(), cfg.Database.Location())
	if err != nil {
		log.Fatalf("mysql: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	wallets, err := config.LoadWatch(config.WatchPath)
	if err != nil {
		log.Fatalf("watch: %v", err)
	}
	if err := db.SyncWatchedWallets(ctx, wallets); err != nil {
		log.Fatalf("sync watched wallets: %v", err)
	}

	execPath := cfg.Execution.WalletsFile
	if execPath == "" {
		execPath = config.ExecutionWalletsPath
	}
	execWallets, err := config.LoadExecutionWallets(execPath)
	if err != nil {
		log.Fatalf("execution wallets: %v", err)
	}
	if err := db.SyncExecutionWallets(ctx, execWallets, cfg.Execution.ActiveWallet); err != nil {
		log.Fatalf("sync execution wallets: %v", err)
	}

	counts, err := db.TableCounts(ctx)
	if err != nil {
		log.Fatalf("table counts: %v", err)
	}

	log.Printf("wallet_intel ready on %s", cfg.Database.Name)
	for _, table := range []string{
		"watched_wallets", "execution_wallets", "trade_decisions",
		"trades", "events", "positions", "seen_tokens", "native_prices",
	} {
		log.Printf("  %s: %d rows", table, counts[table])
	}
}
