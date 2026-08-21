package parse

import (
	"math/big"
	"testing"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/ethereum/go-ethereum/common"
)

func TestTradeUsd_wbnbQuote(t *testing.T) {
	wbnb := common.HexToAddress("0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c")
	amount := new(big.Int).Mul(big.NewInt(25), big.NewInt(1e16)) // 0.25 WBNB
	tr := Trade{
		Side:        sideBuy,
		QuoteToken:  wbnb,
		QuoteAmount: amount,
	}
	usd := TradeUsd(tr, enrich.TokenInfo{}, chain.BSCMainnet, 600)
	if usd != 150 {
		t.Fatalf("expected $150, got %.2f", usd)
	}
}

func TestTradeUsd_usdcQuote(t *testing.T) {
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	tr := Trade{
		Side:        sideBuy,
		QuoteToken:  usdc,
		QuoteAmount: big.NewInt(600_000_000), // $600
	}
	usd := TradeUsd(tr, enrich.TokenInfo{}, chain.EthereumMainnet, 0)
	if usd != 600 {
		t.Fatalf("expected $600, got %.2f", usd)
	}
}
