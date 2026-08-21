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
	fromLabel := flag.String("from", "", "optional source wallet label (e.g. evm-2)")
	toLabel := flag.String("to", "evm-1", "destination wallet label")
	dry := flag.Bool("dry", false, "estimate only")
	flag.Parse()

	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	walletsFile, err := config.LoadExecutionWallets(config.ExecutionWalletsPath)
	if err != nil {
		log.Fatalf("wallets: %v", err)
	}

	dest, err := walletsFile.Pick(*toLabel, *chainID)
	if err != nil {
		log.Fatalf("dest wallet: %v", err)
	}
	destAddr := common.HexToAddress(dest.Address)

	execURL, err := cfg.ExecutionRPCURL(*chainID)
	if err != nil {
		log.Fatalf("exec rpc: %v", err)
	}
	readURL := cfg.ReadDialURLForChain(*chainID)

	var chainNum int64
	switch strings.ToLower(*chainID) {
	case "bsc", "bnb":
		chainNum = 56
	case "ethereum", "eth":
		chainNum = 1
	default:
		log.Fatalf("unsupported chain %q", *chainID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	rpcURL := execURL
	if strings.EqualFold(*chainID, "ethereum") || strings.EqualFold(*chainID, "eth") {
		rpcURL = readURL
	}

	client, err := dialRPC(ctx, rpcURL)
	if err != nil {
		log.Fatalf("rpc dial: %v", err)
	}
	fmt.Printf("using rpc: %s\n", hostOnly(rpcURL))
	defer client.Close()

	fmt.Printf("sweep %s native → %s (%s)\n", *chainID, *toLabel, destAddr.Hex())

	for _, w := range walletsFile.EVM {
		if *fromLabel != "" && !strings.EqualFold(w.Label, *fromLabel) {
			continue
		}
		if strings.EqualFold(w.Label, *toLabel) {
			continue
		}
		if err := sweepWallet(ctx, client, w, destAddr, chainNum, *dry); err != nil {
			fmt.Printf("%s: %v\n", w.Label, err)
		}
	}
}

func dialRPC(ctx context.Context, url string) (*ethclient.Client, error) {
	return ethclient.DialContext(ctx, url)
}

func sweepWallet(ctx context.Context, client *ethclient.Client, w config.ExecutionWallet, dest common.Address, chainNum int64, dry bool) error {
	addr := common.HexToAddress(w.Address)
	bal, err := client.BalanceAt(ctx, addr, nil)
	if err != nil {
		return fmt.Errorf("balance: %w", err)
	}
	if bal.Sign() <= 0 {
		fmt.Printf("%s: empty, skip\n", w.Label)
		return nil
	}

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("gas price: %w", err)
	}
	gasLimit := uint64(21_000)
	fee := new(big.Int).Mul(gasPrice, big.NewInt(int64(gasLimit)))
	if bal.Cmp(fee) <= 0 {
		fmt.Printf("%s: balance %s wei ≤ fee %s wei, skip\n", w.Label, bal, fee)
		return nil
	}

	value := new(big.Int).Sub(bal, fee)
	fmt.Printf("%s: sending %s wei (keep %s wei for gas)\n", w.Label, value, fee)

	if dry {
		return nil
	}

	key, err := crypto.HexToECDSA(strings.TrimPrefix(w.PrivateKey, "0x"))
	if err != nil {
		return fmt.Errorf("key: %w", err)
	}
	if crypto.PubkeyToAddress(key.PublicKey) != addr {
		return fmt.Errorf("key mismatch")
	}

	nonce, err := client.PendingNonceAt(ctx, addr)
	if err != nil {
		return fmt.Errorf("nonce: %w", err)
	}

	tx := types.NewTransaction(nonce, dest, value, gasLimit, gasPrice, nil)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainNum)), key)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	fmt.Printf("%s: broadcast %s\n", w.Label, signed.Hash().Hex())

	receipt, err := waitReceipt(ctx, client, signed.Hash())
	if err != nil {
		return fmt.Errorf("receipt: %w", err)
	}
	fmt.Printf("%s: mined block=%d status=%d\n", w.Label, receipt.BlockNumber.Uint64(), receipt.Status)
	return nil
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
