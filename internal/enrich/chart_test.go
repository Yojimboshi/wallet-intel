package enrich

import "testing"

func TestOneWayChart(t *testing.T) {
	if !OneWayChart(TokenInfo{Liquidity: 1000, TxnsBuys24h: 10, TxnsSells24h: 0}) {
		t.Fatal("expected one-way chart")
	}
	if OneWayChart(TokenInfo{Liquidity: 1000, TxnsBuys24h: 10, TxnsSells24h: 1}) {
		t.Fatal("expected not one-way when sells exist")
	}
	if OneWayChart(TokenInfo{Liquidity: 0, TxnsBuys24h: 10, TxnsSells24h: 0}) {
		t.Fatal("unlisted should not trigger chart gate")
	}
}
