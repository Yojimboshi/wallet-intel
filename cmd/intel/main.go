package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Yojimboshi/wallet-intel/internal/alerts"
	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/execute"
	"github.com/Yojimboshi/wallet-intel/internal/exit"
	"github.com/Yojimboshi/wallet-intel/internal/pool"
	"github.com/Yojimboshi/wallet-intel/internal/safety"
	"github.com/Yojimboshi/wallet-intel/internal/store"
	"github.com/Yojimboshi/wallet-intel/internal/watch"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[wallet-intel] ")

	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	wallets, err := config.LoadWatch(config.WatchPath)
	if err != nil {
		log.Fatalf("watch: %v", err)
	}

	tradeLog, err := store.NewJSONL(cfg.TradeLog)
	if err != nil {
		log.Fatalf("trade log: %v", err)
	}

	seen, err := store.NewSeenTokens("data/seen-tokens.json")
	if err != nil {
		log.Fatalf("seen tokens: %v", err)
	}

	positions, err := store.NewPositions("data/positions.json")
	if err != nil {
		log.Fatalf("positions: %v", err)
	}
	hodlBook, err := store.NewHodlBook("data/hodl.json")
	if err != nil {
		log.Fatalf("hodl: %v", err)
	}
	batchBuy := store.NewBatchBuyTracker()
	events, err := store.NewEventLog("data/events.jsonl")
	if err != nil {
		log.Fatalf("event log: %v", err)
	}

	var mysqlDB *store.MySQL
	if cfg.Database.Enabled {
		var err error
		mysqlDB, err = store.OpenMySQL(cfg.Database.DSN(), cfg.Database.Location())
		if err != nil {
			log.Fatalf("mysql: %v", err)
		}
		defer mysqlDB.Close()
		tradeLog.UseMySQL(mysqlDB)
		events.UseMySQL(mysqlDB)
		if err := positions.UseMySQL(mysqlDB); err != nil {
			log.Fatalf("mysql positions: %v", err)
		}
		if err := seen.UseMySQL(mysqlDB); err != nil {
			log.Fatalf("mysql seen tokens: %v", err)
		}
		hodlBook.UseMySQL(mysqlDB)
		if err := mysqlDB.SyncWatchedWallets(context.Background(), wallets); err != nil {
			log.Fatalf("mysql watched wallets: %v", err)
		}
		execPath := cfg.Execution.WalletsFile
		if execPath == "" {
			execPath = config.ExecutionWalletsPath
		}
		execWallets, err := config.LoadExecutionWallets(execPath)
		if err != nil {
			log.Fatalf("execution wallets: %v", err)
		}
		if err := mysqlDB.SyncExecutionWallets(context.Background(), execWallets, cfg.Execution.ActiveWallet); err != nil {
			log.Fatalf("mysql execution wallets: %v", err)
		}
		log.Printf("mysql enabled | db=%s", cfg.Database.Name)
	}

	notify := alerts.MultiNotifier(
		alerts.Logger{},
		alerts.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	started := 0

	for _, chainID := range cfg.Rules.Chains {
		chainCfg, ok := chain.ByID(chainID)
		if !ok {
			log.Printf("skip unsupported chain %q", chainID)
			continue
		}

		execCfg, err := cfg.ExecutionConfig(string(chainCfg.ID))
		if err != nil {
			log.Fatalf("execution (%s): %v", chainCfg.ID, err)
		}
		log.Println(cfg.RPCStatusLine(string(chainCfg.ID)))
		log.Println(execCfg.StatusLine())
		if chainCfg.UniversalRouter != (common.Address{}) {
			log.Printf("execution UR=%s permit2=%s", chainCfg.UniversalRouter.Hex(), chainCfg.Permit2.Hex())
		}

		readClient, err := ethclient.Dial(cfg.ReadDialURLForChain(string(chainCfg.ID)))
		if err != nil {
			log.Fatalf("read rpc (%s): %v", chainCfg.ID, err)
		}
		nativeOracle := pool.NewNativeOracle(readClient, chainCfg, execCfg.NativeUsdPrice)
		if mysqlDB != nil {
			nativeOracle.UseStore(mysqlDB)
		}

		var executor execute.Executor
		var execClient *ethclient.Client
		if execCfg.AllowLiveExecution {
			execURL, err := cfg.ExecutionRPCURL(string(chainCfg.ID))
			if err != nil {
				readClient.Close()
				log.Fatalf("execution rpc (%s): %v", chainCfg.ID, err)
			}
			execClient, err = ethclient.Dial(execURL)
			if err != nil {
				readClient.Close()
				log.Fatalf("execution rpc dial (%s): %v", chainCfg.ID, err)
			}
			executor, err = execute.New(execClient, readClient, execCfg, string(chainCfg.ID), chainCfg, nativeOracle)
			if err != nil {
				readClient.Close()
				execClient.Close()
				log.Fatalf("executor (%s): %v", chainCfg.ID, err)
			}
			log.Printf("copy-trade armed on %s for wallets marked copy:true", chainCfg.ID)
		}

		safetyChecker := safety.New(execCfg.Safety.ToSafetyConfig())
		enricher := enrich.NewClient(chainCfg)

		var exitMon *exit.Monitor
		if execCfg.Exit.Enabled {
			exitMon = exit.NewMonitor(chainCfg, execCfg.Exit, positions, enricher, executor, notify, execCfg, nativeOracle, events, hodlBook)
		}

		w := watch.New(
			readClient,
			chainCfg,
			wallets,
			cfg.Rules,
			execCfg,
			executor,
			safetyChecker,
			seen,
			positions,
			nativeOracle,
			exitMon,
			enricher,
			notify,
			tradeLog,
			events,
			batchBuy,
		)
		if mysqlDB != nil {
			w.UseMySQL(mysqlDB)
		}

		started++
		wg.Add(1)
		if exitMon != nil {
			wg.Add(1)
			go func(m *exit.Monitor, read *ethclient.Client) {
				defer wg.Done()
				m.Run(ctx, read)
			}(exitMon, readClient)
		}
		go func(id chain.ID, read *ethclient.Client, exec *ethclient.Client) {
			defer wg.Done()
			defer read.Close()
			if exec != nil {
				defer exec.Close()
			}
			log.Printf("watching %s", id)
			if err := w.Run(ctx); err != nil && err != context.Canceled {
				log.Printf("watcher %s: %v", id, err)
			}
		}(chainCfg.ID, readClient, execClient)
	}

	if started == 0 {
		log.Fatal("no supported chain in rules.chains")
	}

	wg.Wait()
}
