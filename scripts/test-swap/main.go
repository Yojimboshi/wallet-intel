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
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	chainName := flag.String("chain", "bsc", "chain: bsc or ethereum")
	side := flag.String("side", "buy", "buy or sell")
	tokenStr := flag.String("token", "", "token contract address")
	usd := flag.Float64("usd", 50, "buy size in USD")
	walletLabel := flag.String("wallet", "evm-1", "execution wallet label")
	dry := flag.Bool("dry", true, "simulate only (estimate gas, no broadcast)")
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

	execCfg, err := cfg.ExecutionConfig(string(chainCfg.ID))
	if err != nil {
		log.Fatalf("execution: %v", err)
	}
	execCfg.Provider = "direct"
	execCfg.SimulateSwaps = *dry

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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

	req := execute.Request{
		SourceLabel: "test-swap",
		Trade: parse.Trade{
			Side:  strings.ToLower(*side),
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

	fmt.Printf("chain=%s side=%s token=%s usd=%.0f wallet=%s dry=%v\n", *chainName, *side, token.Hex(), *usd, w.Label, *dry)
	fmt.Printf("v2 router=%s simulate=%v\n", execCfg.V2Router.Hex(), execCfg.SimulateSwaps)

	if _, err := executor.Mirror(ctx, req); err != nil {
		log.Fatalf("mirror: %v", err)
	}
	fmt.Println("ok")
}
