package pool

import (
	"context"
	"testing"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestNativeUSDFromV2Pair_live(t *testing.T) {
	ctx := context.Background()

	bscClient, err := ethclient.Dial("https://bsc-dataseed1.binance.org")
	if err != nil {
		t.Skip(err)
	}
	bnbPrice, err := NativeUSDFromV2Pair(ctx, bscClient, chain.BSCMainnet, chain.BSCMainnet.NativeUSDPool)
	if err != nil {
		t.Fatalf("bsc: %v", err)
	}
	if bnbPrice < 50 || bnbPrice > 5_000 {
		t.Fatalf("unexpected BNB price: %.2f", bnbPrice)
	}

	ethClient, err := ethclient.Dial("https://1rpc.io/eth")
	if err != nil {
		t.Skip(err)
	}
	ethPrice, err := NativeUSDFromV2Pair(ctx, ethClient, chain.EthereumMainnet, chain.EthereumMainnet.NativeUSDPool)
	if err != nil {
		t.Skipf("ethereum rpc: %v", err)
	}
	if ethPrice < 500 || ethPrice > 50_000 {
		t.Fatalf("unexpected ETH price: %.2f", ethPrice)
	}
}

func TestNativeOracle_cache(t *testing.T) {
	ctx := context.Background()
	client, err := ethclient.Dial("https://bsc-dataseed1.binance.org")
	if err != nil {
		t.Skip(err)
	}
	o := NewNativeOracle(client, chain.BSCMainnet, 0)
	p1, err := o.USD(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := o.USD(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Fatalf("expected cached price, got %.2f vs %.2f", p1, p2)
	}
}
