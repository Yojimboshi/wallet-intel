package enrich

// OneWayChart is true when DexScreener shows 24h buys but zero sells on a listed pair.
func OneWayChart(info TokenInfo) bool {
	return info.Liquidity > 0 && info.TxnsBuys24h > 0 && info.TxnsSells24h == 0
}
