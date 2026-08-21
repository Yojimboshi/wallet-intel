// One-off: scan last 2 days of ERC-20 transfers for watched wallets.
//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sort"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/parse"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const lookbackHours = 48

var topicTransfer = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

func main() {
	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Fatal(err)
	}
	wallets, err := config.LoadWatch(config.WatchPath)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	since := time.Now().Add(-lookbackHours * time.Hour)
	fmt.Printf("Scanning last %dh (since ~%s UTC)\n\n", lookbackHours, since.UTC().Format(time.RFC3339))
	fmt.Printf("Alert rules: minBuyUsd=$%.0f maxMC=$%.0f minLiq=$%.0f chains=%v\n\n",
		cfg.Rules.MinBuyUsd, cfg.Rules.MaxMarketCapUsd, cfg.Rules.MinLiquidityUsd, cfg.Rules.Chains)

	var alerts, blocked int
	for _, chainID := range cfg.Rules.Chains {
		chainCfg, ok := chain.ByID(chainID)
		if !ok {
			continue
		}
		url := cfg.ReadDialURLForChain(chainID)
		client, err := ethclient.Dial(url)
		if err != nil {
			log.Printf("dial %s: %v", chainID, err)
			continue
		}
		head, err := client.HeaderByNumber(ctx, nil)
		if err != nil {
			log.Printf("head %s: %v", chainID, err)
			client.Close()
			continue
		}
		fromBlock := blockFromTime(head, chainID, since)
		fmt.Printf("=== %s blocks %d → %d ===\n", chainCfg.Name, fromBlock, head.Number.Uint64())

		watched := map[common.Address]string{}
		var topics []common.Hash
		for _, w := range wallets {
			if len(w.Chains) > 0 {
				if _, ok := w.Chains[chainID]; !ok {
					continue
				}
			}
			watched[w.Address] = w.Label
			topics = append(topics, common.BytesToHash(common.LeftPadBytes(w.Address.Bytes(), 32)))
		}
		if len(watched) == 0 {
			client.Close()
			continue
		}

		enricher := enrich.NewClient(chainCfg)
		logs := fetchLogs(ctx, client, fromBlock, head.Number.Uint64(), topics)
		client.Close()

		byTx := map[common.Hash]uint64{}
		for _, lg := range logs {
			byTx[lg.TxHash] = lg.BlockNumber
		}
		txs := make([]common.Hash, 0, len(byTx))
		for h := range byTx {
			txs = append(txs, h)
		}
		sort.Slice(txs, func(i, j int) bool { return byTx[txs[i]] < byTx[txs[j]] })

		fmt.Printf("  %d transfer logs, %d unique txs\n", len(logs), len(txs))

		readClient, _ := ethclient.Dial(url)
		for _, txHash := range txs {
			blockNum := byTx[txHash]
			receipt, err := readClient.TransactionReceipt(ctx, txHash)
			if err != nil {
				continue
			}
			trades := parse.ParseLogs(receipt.Logs, watched, chainCfg, txHash, blockNum)
			for _, tr := range trades {
				info, _ := enricher.LookupToken(ctx, tr.Token)
				usd := parse.TradeUsd(tr, info, chainCfg, 0)
				ok, reason := cfg.Rules.PassesAlert(tr.Side, usd, info)
				if ok {
					alerts++
					fmt.Printf("  ALERT %s %s %s token=%s ~$%.0f MC=$%.0f liq=$%.0f tx=%s\n",
						tr.WalletLabel, tr.Side, info.Symbol, tr.Token.Hex(), usd, info.MarketCap, info.Liquidity, txHash.Hex())
				} else if tr.Side == "buy" || tr.Side == "sell" {
					blocked++
					fmt.Printf("  skip  %s %s %s ~$%.0f — %s\n",
						tr.WalletLabel, tr.Side, info.Symbol, usd, reason)
				}
			}
		}
		readClient.Close()
		fmt.Println()
	}

	fmt.Printf("Summary: %d would-alert, %d blocked/skipped by rules\n", alerts, blocked)
}

func blockFromTime(head *types.Header, chainID string, since time.Time) uint64 {
	blockSec := 12.0
	if chainID == "bsc" {
		blockSec = 3.0
	}
	secs := time.Unix(int64(head.Time), 0).Sub(since).Seconds()
	if secs <= 0 {
		return head.Number.Uint64()
	}
	blocksBack := uint64(secs / blockSec)
	if blocksBack >= head.Number.Uint64() {
		return 0
	}
	return head.Number.Uint64() - blocksBack
}

func fetchLogs(ctx context.Context, client *ethclient.Client, from, to uint64, walletTopics []common.Hash) []types.Log {
	var out []types.Log
	for _, q := range [][][]common.Hash{
		{{topicTransfer}, nil, walletTopics},
		{{topicTransfer}, walletTopics},
	} {
		out = append(out, fetchLogsChunked(ctx, client, from, to, q)...)
	}
	return dedupe(out)
}

func fetchLogsChunked(ctx context.Context, client *ethclient.Client, from, to uint64, topics [][]common.Hash) []types.Log {
const step = 5000
	var out []types.Log
	for start := from; start <= to; start += step {
		end := start + step - 1
		if end > to {
			end = to
		}
		q := ethereum.FilterQuery{FromBlock: new(big.Int).SetUint64(start), ToBlock: new(big.Int).SetUint64(end), Topics: topics}
		logs, err := client.FilterLogs(ctx, q)
		if err != nil {
			log.Printf("  chunk %d-%d: %v", start, end, err)
			continue
		}
		out = append(out, logs...)
	}
	return out
}

func dedupe(logs []types.Log) []types.Log {
	seen := map[string]struct{}{}
	var out []types.Log
	for _, l := range logs {
		k := fmt.Sprintf("%s:%d", l.TxHash.Hex(), l.Index)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, l)
	}
	return out
}
