package parse

import (
	"math/big"
	"testing"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestAggregatorStableFillsBuyQuote(t *testing.T) {
	wallet := common.HexToAddress(testWallet)
	token := common.HexToAddress(testToken)
	usdt := common.HexToAddress("0x55d398326f99059fF775485246999027B3197955")
	usdc := common.HexToAddress("0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d")
	helper := common.HexToAddress("0xccc88a9d1b4ed6b0eaba998850414b24f1c315be")
	router := common.HexToAddress("0x000000000000000000000000000000000000d001")
	watched := map[common.Address]string{wallet: "fm-test"}

	usdtAmt := mustBig("7195480807722828591")
	usdcAmt := mustBig("7189858177552146207")
	maxUint := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	logs := []*types.Log{
		quoteTransfer(usdt, helper, helper, maxUint, 0),
		quoteTransfer(usdt, helper, router, usdtAmt, 1),
		quoteTransfer(usdc, router, router, usdcAmt, 2),
		{
			Address: token,
			Topics: []common.Hash{
				topicTransfer,
				common.BytesToHash(common.LeftPadBytes(router.Bytes(), 32)),
				common.BytesToHash(common.LeftPadBytes(wallet.Bytes(), 32)),
			},
			Data:  common.FromHex("0x0000000000000000000000000000000000000000000000000de0b6b3a7640000"),
			Index: 3,
		},
	}

	trades := ParseLogs(logs, watched, chain.BSCMainnet, common.HexToHash(testTxHash), testBlock)
	if len(trades) != 1 {
		t.Fatalf("got %d trades", len(trades))
	}
	if trades[0].QuoteAmount == nil || trades[0].QuoteAmount.Cmp(usdtAmt) != 0 {
		t.Fatalf("quote=%v want %s", trades[0].QuoteAmount, usdtAmt)
	}
	if trades[0].QuoteToken != usdt {
		t.Fatalf("quote token %s", trades[0].QuoteToken.Hex())
	}
	if trades[0].QuoteSymbol != "USDT" {
		t.Fatalf("symbol %s", trades[0].QuoteSymbol)
	}
}

func TestAggregatorSplitsAmongWatchedBuys(t *testing.T) {
	w1 := common.HexToAddress("0x000000000000000000000000000000000000a001")
	w2 := common.HexToAddress("0x000000000000000000000000000000000000a002")
	token := common.HexToAddress(testToken)
	usdt := common.HexToAddress("0x55d398326f99059fF775485246999027B3197955")
	helper := common.HexToAddress("0x000000000000000000000000000000000000d001")
	watched := map[common.Address]string{w1: "a", w2: "b"}
	spend := big.NewInt(10_000_000)

	logs := []*types.Log{
		quoteTransfer(usdt, helper, helper, spend, 0),
		tokenTransfer(token, helper, w1, big.NewInt(1e18), 1),
		tokenTransfer(token, helper, w2, big.NewInt(3e18), 2),
	}
	trades := ParseLogs(logs, watched, chain.BSCMainnet, common.HexToHash(testTxHash), testBlock)
	if len(trades) != 2 {
		t.Fatalf("got %d trades", len(trades))
	}

	got := map[common.Address]*big.Int{}
	for _, tr := range trades {
		got[tr.Wallet] = tr.QuoteAmount
	}
	if got[w1] == nil || got[w1].Cmp(big.NewInt(2_500_000)) != 0 {
		t.Fatalf("w1 quote %v", got[w1])
	}
	if got[w2] == nil || got[w2].Cmp(big.NewInt(7_500_000)) != 0 {
		t.Fatalf("w2 quote %v", got[w2])
	}
}

func quoteTransfer(token, from, to common.Address, amount *big.Int, index uint) *types.Log {
	return &types.Log{
		Address: token,
		Topics: []common.Hash{
			topicTransfer,
			common.BytesToHash(common.LeftPadBytes(from.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(to.Bytes(), 32)),
		},
		Data:  common.LeftPadBytes(amount.Bytes(), 32),
		Index: index,
	}
}

func tokenTransfer(token, from, to common.Address, amount *big.Int, index uint) *types.Log {
	return quoteTransfer(token, from, to, amount, index)
}

func mustBig(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(s)
	}
	return n
}
