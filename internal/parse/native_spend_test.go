package parse

import (
	"math/big"
	"testing"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestFourMemeTokenPurchaseFillsNativeQuote(t *testing.T) {
	wallet := common.HexToAddress(testWallet)
	token := common.HexToAddress(testToken)
	watched := map[common.Address]string{wallet: "w1"}
	funds := big.NewInt(2e16) // 0.02 BNB

	data := make([]byte, 256)
	copy(data[12:32], token.Bytes())
	copy(data[44:64], wallet.Bytes())
	funds.FillBytes(data[224:256])

	transfer := types.Log{
		Address: token,
		Topics: []common.Hash{
			topicTransfer,
			common.BytesToHash(common.LeftPadBytes(common.HexToAddress(testSender).Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(wallet.Bytes(), 32)),
		},
		Data:  common.FromHex("0x0000000000000000000000000000000000000000000000000de0b6b3a7640000"),
		Index: 1,
	}
	purchase := types.Log{
		Address: common.HexToAddress("0x5c952063c7fc8610FFDB798152D69F0B9550762b"),
		Topics:  []common.Hash{topicTokenPurchase},
		Data:    data,
		Index:   2,
	}

	trades := ParseLogs(
		[]*types.Log{&transfer, &purchase},
		watched,
		chain.BSCMainnet,
		common.HexToHash(testTxHash),
		testBlock,
	)
	if len(trades) != 1 {
		t.Fatalf("got %d trades", len(trades))
	}
	if trades[0].QuoteAmount == nil || trades[0].QuoteAmount.Cmp(funds) != 0 {
		t.Fatalf("quote=%v want %s", trades[0].QuoteAmount, funds)
	}
	wbnb, _ := chain.BSCMainnet.WrappedNative()
	if trades[0].QuoteToken != wbnb {
		t.Fatalf("quote token %s", trades[0].QuoteToken.Hex())
	}
}

func TestTxValueFillsSingleBuy(t *testing.T) {
	wallet := common.HexToAddress(testWallet)
	token := common.HexToAddress(testToken)
	value := big.NewInt(3e16)
	trades := []Trade{{
		Wallet: wallet, Side: sideBuy, Token: token,
		TokenAmount: big.NewInt(1e18),
	}}
	ApplyTxNativeValue(trades, nil, chain.BSCMainnet, value)
	if trades[0].QuoteAmount == nil || trades[0].QuoteAmount.Cmp(value) != 0 {
		t.Fatalf("quote=%v", trades[0].QuoteAmount)
	}
}
