package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	chainID := flag.String("chain", "bsc", "chain: bsc or ethereum")
	fromLabel := flag.String("from", "evm-1", "source wallet label")
	toLabel := flag.String("to", "evm-2", "destination wallet label")
	amountWei := flag.Uint64("wei", 1, "amount to send in wei")
	dry := flag.Bool("dry", false, "build and estimate only, do not broadcast")
	flag.Parse()

	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	walletsFile, err := config.LoadExecutionWallets(config.ExecutionWalletsPath)
	if err != nil {
		log.Fatalf("wallets: %v", err)
	}

	from, err := walletsFile.Pick(*fromLabel, *chainID)
	if err != nil {
		log.Fatalf("from wallet: %v", err)
	}
	to, err := walletsFile.Pick(*toLabel, *chainID)
	if err != nil {
		log.Fatalf("to wallet: %v", err)
	}

	execURL, err := cfg.ExecutionRPCURL(*chainID)
	if err != nil {
		log.Fatalf("exec rpc: %v", err)
	}
	readURL := cfg.ReadDialURLForChain(*chainID)

	var chainIDNum int64
	switch strings.ToLower(*chainID) {
	case "bsc", "bnb":
		chainIDNum = 56
	case "ethereum", "eth":
		chainIDNum = 1
	default:
		log.Fatalf("unsupported chain %q", *chainID)
	}

	fromAddr := common.HexToAddress(from.Address)
	toAddr := common.HexToAddress(to.Address)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	fmt.Printf("chain=%s exec=%s read=%s\n", *chainID, hostOnly(execURL), hostOnly(readURL))
	fmt.Printf("from=%s (%s) to=%s (%s) amount=%d wei\n", *fromLabel, fromAddr.Hex(), *toLabel, toAddr.Hex(), *amountWei)

	execClient, err := ethclient.DialContext(ctx, execURL)
	if err != nil {
		log.Fatalf("exec dial: %v", err)
	}
	defer execClient.Close()

	readClient, err := ethclient.DialContext(ctx, readURL)
	if err != nil {
		log.Fatalf("read dial: %v", err)
	}
	defer readClient.Close()

	checkBalance(ctx, "exec", execClient, fromAddr)
	checkBalance(ctx, "read", readClient, fromAddr)

	key, err := crypto.HexToECDSA(strings.TrimPrefix(from.PrivateKey, "0x"))
	if err != nil {
		log.Fatalf("private key: %v", err)
	}
	if crypto.PubkeyToAddress(key.PublicKey) != fromAddr {
		log.Fatalf("private key does not match from address")
	}

	nonce, err := execClient.PendingNonceAt(ctx, fromAddr)
	if err != nil {
		log.Fatalf("exec nonce: %v", err)
	}
	gasPrice, err := execClient.SuggestGasPrice(ctx)
	if err != nil {
		log.Fatalf("exec gas price: %v", err)
	}

	value := new(big.Int).SetUint64(*amountWei)
	tx := types.NewTransaction(nonce, toAddr, value, 21_000, gasPrice, nil)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainIDNum)), key)
	if err != nil {
		log.Fatalf("sign: %v", err)
	}

	fmt.Printf("signed tx hash=%s nonce=%d gasPrice=%s\n", signed.Hash().Hex(), nonce, gasPrice.String())

	if *dry {
		fmt.Println("dry-run only — not broadcasting")
		return
	}

	err = execClient.SendTransaction(ctx, signed)
	if err != nil {
		log.Fatalf("exec SendTransaction: %v", err)
	}
	fmt.Printf("broadcast ok via exec rpc: %s\n", signed.Hash().Hex())

	receipt, err := waitReceipt(ctx, execClient, signed.Hash())
	if err != nil {
		log.Fatalf("wait receipt: %v", err)
	}
	fmt.Printf("mined block=%d status=%d gasUsed=%d\n", receipt.BlockNumber.Uint64(), receipt.Status, receipt.GasUsed)
}

func checkBalance(ctx context.Context, label string, client *ethclient.Client, addr common.Address) {
	bal, err := client.BalanceAt(ctx, addr, nil)
	if err != nil {
		fmt.Printf("balance[%s]: ERROR %v\n", label, err)
		return
	}
	fmt.Printf("balance[%s]: %s wei\n", label, bal.String())
}

func waitReceipt(ctx context.Context, client *ethclient.Client, hash common.Hash) (*types.Receipt, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		receipt, err := client.TransactionReceipt(ctx, hash)
		if err == nil {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func hostOnly(raw string) string {
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	}
	if j := strings.Index(raw, "/"); j >= 0 {
		raw = raw[:j]
	}
	return raw
}
