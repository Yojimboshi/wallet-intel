package parse

import (
	"math/big"
	"testing"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
)

func TestConsolidateTradesMergeSwapAndTransfer(t *testing.T) {
	wallet := common.HexToAddress(testWallet)
	token := common.HexToAddress(testToken)
	tx := common.HexToHash(testTxHash)
	quote := big.NewInt(45500000000000000)

	trades := []Trade{
		{
			Wallet: wallet, WalletLabel: "w1", Side: sideBuy, Token: token,
			TokenAmount: big.NewInt(1e18), TxHash: tx, BlockNumber: testBlock, LogIndex: 1,
		},
		{
			Wallet: wallet, WalletLabel: "w1", Side: sideBuy,
			QuoteAmount: quote, Pair: common.HexToAddress("0x1111111111111111111111111111111111111111"),
			DEX: "uniswap-v2", TxHash: tx, BlockNumber: testBlock, LogIndex: 2,
		},
	}

	out := consolidateTrades(trades, chain.BSCMainnet)
	if len(out) != 1 {
		t.Fatalf("expected 1 consolidated trade, got %d", len(out))
	}
	if out[0].Token != token {
		t.Fatalf("token not preserved")
	}
	if out[0].QuoteAmount == nil || out[0].QuoteAmount.Cmp(quote) != 0 {
		t.Fatalf("quote not merged")
	}
	if out[0].QuoteToken == (common.Address{}) {
		t.Fatalf("expected wrapped native quote token")
	}
}

func TestConsolidateTradesDropsSwapOnly(t *testing.T) {
	wallet := common.HexToAddress(testWallet)
	tx := common.HexToHash(testTxHash)
	trades := []Trade{{
		Wallet: wallet, Side: sideBuy, QuoteAmount: big.NewInt(1e16),
		TxHash: tx, LogIndex: 1,
	}}
	out := consolidateTrades(trades, chain.BSCMainnet)
	if len(out) != 0 {
		t.Fatalf("expected swap-only trade dropped, got %d", len(out))
	}
}
