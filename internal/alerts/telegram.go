package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/parse"
)

type Notifier interface {
	Send(ctx context.Context, msg string) error
}

type Telegram struct {
	token  string
	chatID string
	client *http.Client
}

func NewTelegram(token, chatID string) *Telegram {
	if token == "" || chatID == "" {
		return nil
	}
	return &Telegram{
		token:  token,
		chatID: chatID,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *Telegram) Send(ctx context.Context, msg string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	body, _ := json.Marshal(map[string]string{
		"chat_id":    t.chatID,
		"text":       msg,
		"parse_mode": "HTML",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

type Logger struct{}

func (Logger) Send(_ context.Context, msg string) error {
	log.Println(msg)
	return nil
}

type Multi struct {
	backends []Notifier
}

func MultiNotifier(backends ...Notifier) Notifier {
	var valid []Notifier
	for _, b := range backends {
		if b != nil {
			valid = append(valid, b)
		}
	}
	if len(valid) == 0 {
		return Logger{}
	}
	return Multi{backends: valid}
}

func (m Multi) Send(ctx context.Context, msg string) error {
	var first error
	for _, b := range m.backends {
		if err := b.Send(ctx, msg); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func FormatTrade(tr parse.Trade, info enrich.TokenInfo, chainCfg chain.Config) string {
	icon := "🟢"
	action := "BUY"
	if tr.Side == "sell" {
		icon = "🔴"
		action = "SELL"
	}

	symbol := info.Symbol
	if symbol == "" {
		symbol = shortHex(tr.Token.Hex())
	}

	spent := "?"
	if tr.QuoteAmount != nil {
		qSym := tr.QuoteSymbol
		if qSym == "" {
			qSym = chainCfg.NativeSymbol
		}
		decimals := 18
		if q, ok := chainCfg.QuoteTokens[tr.QuoteToken]; ok {
			decimals = q.Decimals
		}
		spent = fmt.Sprintf("%s %s", parse.FormatAmount(tr.QuoteAmount, decimals), qSym)
	}

	tokenAmt := "?"
	if tr.TokenAmount != nil {
		decimals := info.Decimals
		if decimals == 0 {
			decimals = 18
		}
		tokenAmt = parse.FormatAmount(tr.TokenAmount, decimals)
	}

	lines := []string{
		fmt.Sprintf("%s <b>%s</b> %s bought <b>%s</b>", icon, action, tr.WalletLabel, symbol),
		fmt.Sprintf("Amount: %s | Spent: %s", tokenAmt, spent),
		fmt.Sprintf("MC: %s | Liq: %s", enrich.FormatUsd(info.MarketCap), enrich.FormatUsd(info.Liquidity)),
	}

	if info.DexURL != "" {
		lines = append(lines, info.DexURL)
	} else {
		lines = append(lines, chainCfg.ExplorerTxURL+tr.TxHash.Hex())
	}

	return strings.Join(lines, "\n")
}

func shortHex(s string) string {
	if len(s) < 10 {
		return s
	}
	return s[:6] + "…" + s[len(s)-4:]
}
