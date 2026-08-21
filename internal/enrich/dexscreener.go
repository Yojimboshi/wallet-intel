package enrich

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
)

type TokenInfo struct {
	Symbol            string
	Name              string
	Decimals          int
	MarketCap         float64
	Liquidity         float64
	PriceUsd          float64
	DexURL            string
	PairAddress       string
	QuoteTokenAddress string
	QuoteSymbol       string
	TxnsBuys24h       int
	TxnsSells24h      int
}

type dexPair struct {
	ChainID     string `json:"chainId"`
	PairAddress string `json:"pairAddress"`
	URL         string `json:"url"`
	PriceUsd    string `json:"priceUsd"`
	Liquidity   struct {
		Usd float64 `json:"usd"`
	} `json:"liquidity"`
	MarketCap float64 `json:"marketCap"`
	Txns      struct {
		H24 struct {
			Buys  int `json:"buys"`
			Sells int `json:"sells"`
		} `json:"h24"`
	} `json:"txns"`
	BaseToken struct {
		Address  string `json:"address"`
		Symbol   string `json:"symbol"`
		Name     string `json:"name"`
	} `json:"baseToken"`
	QuoteToken struct {
		Address string `json:"address"`
		Symbol  string `json:"symbol"`
	} `json:"quoteToken"`
}

type dexScreenerResponse struct {
	Pairs []dexPair `json:"pairs"`
}

type Client struct {
	http     *http.Client
	chainID  string
	chainCfg chain.Config
	dexBase  string
}

func NewClient(chainCfg chain.Config) *Client {
	return &Client{
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
		chainID:  string(chainCfg.ID),
		chainCfg: chainCfg,
		dexBase:  chainCfg.DexScreener,
	}
}

func (c *Client) LookupToken(ctx context.Context, token common.Address) (TokenInfo, error) {
	url := fmt.Sprintf("https://api.dexscreener.com/latest/dex/tokens/%s", token.Hex())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return TokenInfo{}, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return TokenInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return TokenInfo{}, fmt.Errorf("dexscreener status %d", resp.StatusCode)
	}

	var payload dexScreenerResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return TokenInfo{}, err
	}

	best, ok := pickBestExecutionPair(c.chainCfg, c.chainID, token, payload.Pairs)
	if !ok {
		return TokenInfo{}, fmt.Errorf("no %s pair found for %s", c.chainID, token.Hex())
	}

	price := 0.0
	fmt.Sscanf(best.PriceUsd, "%f", &price)
	return TokenInfo{
		Symbol:            best.BaseToken.Symbol,
		Name:              best.BaseToken.Name,
		MarketCap:         best.MarketCap,
		Liquidity:         best.Liquidity.Usd,
		PriceUsd:          price,
		DexURL:            best.URL,
		PairAddress:       best.PairAddress,
		QuoteTokenAddress: best.QuoteToken.Address,
		QuoteSymbol:       best.QuoteToken.Symbol,
		TxnsBuys24h:       best.Txns.H24.Buys,
		TxnsSells24h:      best.Txns.H24.Sells,
	}, nil
}

func pickBestExecutionPair(chainCfg chain.Config, chainID string, token common.Address, pairs []dexPair) (dexPair, bool) {
	var best *dexPair
	var bestAny *dexPair
	for i := range pairs {
		p := pairs[i]
		if !strings.EqualFold(p.ChainID, chainID) {
			continue
		}
		if !strings.EqualFold(p.BaseToken.Address, token.Hex()) {
			continue
		}
		cp := p
		if bestAny == nil || cp.Liquidity.Usd > bestAny.Liquidity.Usd {
			tmp := cp
			bestAny = &tmp
		}
		quoteAddr := common.HexToAddress(p.QuoteToken.Address)
		if !chainCfg.IsExecutionQuote(quoteAddr) {
			continue
		}
		if best == nil || cp.Liquidity.Usd > best.Liquidity.Usd {
			tmp := cp
			best = &tmp
		}
	}
	if best != nil {
		return *best, true
	}
	if bestAny != nil {
		return *bestAny, true
	}
	return dexPair{}, false
}

func FormatUsd(v float64) string {
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("~$%.1fM", v/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("~$%.0fK", v/1_000)
	case v > 0:
		return fmt.Sprintf("~$%.0f", v)
	default:
		return "n/a"
	}
}
