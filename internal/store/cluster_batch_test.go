package store

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestBuildTxClusterBuysSumsAcrossWallets(t *testing.T) {
	token := common.HexToAddress("0x000000000000000000000000000000000000c0de")
	w1 := common.HexToAddress("0x000000000000000000000000000000000000a001")
	w2 := common.HexToAddress("0x000000000000000000000000000000000000a002")

	out := BuildTxClusterBuys([]TxClusterLeg{
		{Token: token, Wallet: w1, Side: "buy", TradeUsd: 300},
		{Token: token, Wallet: w2, Side: "buy", TradeUsd: 250},
	})
	c, ok := out[token]
	if !ok || c.TotalUsd != 550 || c.Legs != 2 {
		t.Fatalf("cluster: %+v ok=%v", c, ok)
	}
}

func TestBuildTxClusterBuysIgnoresSingleWallet(t *testing.T) {
	token := common.HexToAddress("0x000000000000000000000000000000000000c0de")
	w1 := common.HexToAddress("0x000000000000000000000000000000000000a001")

	out := BuildTxClusterBuys([]TxClusterLeg{
		{Token: token, Wallet: w1, Side: "buy", TradeUsd: 600},
		{Token: token, Wallet: w1, Side: "buy", TradeUsd: 100},
	})
	if len(out) != 0 {
		t.Fatalf("expected no cluster for one wallet, got %+v", out)
	}
}
