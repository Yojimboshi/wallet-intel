package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/parse"
)

type TradeRecord struct {
	Timestamp   time.Time `json:"timestamp"`
	Chain       string    `json:"chain"`
	Wallet      string    `json:"wallet"`
	WalletLabel string    `json:"walletLabel"`
	Side        string    `json:"side"`
	Token       string    `json:"token"`
	TokenSymbol string    `json:"tokenSymbol"`
	TokenAmount string    `json:"tokenAmount"`
	QuoteSymbol string    `json:"quoteSymbol"`
	QuoteAmount string    `json:"quoteAmount"`
	MarketCap   float64   `json:"marketCapUsd"`
	Liquidity   float64   `json:"liquidityUsd"`
	TxHash      string    `json:"txHash"`
	BlockNumber uint64    `json:"blockNumber"`
	DexURL      string    `json:"dexUrl"`
}

type JSONL struct {
	path  string
	mysql *MySQL
}

func NewJSONL(path string) (*JSONL, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	return &JSONL{path: path}, nil
}

func (j *JSONL) UseMySQL(db *MySQL) {
	j.mysql = db
}

func (j *JSONL) Append(chainName string, tr parse.Trade, info enrich.TokenInfo) error {
	rec := TradeRecord{
		Timestamp:   time.Now().UTC(),
		Chain:       chainName,
		Wallet:      tr.Wallet.Hex(),
		WalletLabel: tr.WalletLabel,
		Side:        tr.Side,
		Token:       tr.Token.Hex(),
		TokenSymbol: info.Symbol,
		TxHash:      tr.TxHash.Hex(),
		BlockNumber: tr.BlockNumber,
		MarketCap:   info.MarketCap,
		Liquidity:   info.Liquidity,
		DexURL:      info.DexURL,
		QuoteSymbol: tr.QuoteSymbol,
	}
	if tr.TokenAmount != nil {
		rec.TokenAmount = tr.TokenAmount.String()
	}
	if tr.QuoteAmount != nil {
		rec.QuoteAmount = tr.QuoteAmount.String()
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(append(line, '\n'))
	if err != nil {
		return err
	}
	if j.mysql != nil {
		if err := j.mysql.InsertTrade(context.Background(), rec); err != nil {
			return fmt.Errorf("mysql trade: %w", err)
		}
	}
	return nil
}
