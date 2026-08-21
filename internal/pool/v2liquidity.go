package pool

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	methodToken0      = [4]byte{0x0d, 0xfe, 0x16, 0x81}
	methodToken1      = [4]byte{0xd2, 0x12, 0x20, 0xa7}
	methodGetReserves = [4]byte{0x09, 0x02, 0xf1, 0xac}
)

// V2LiquidityUSD reads Uniswap/Pancake V2 pair reserves and estimates pool liquidity in USD.
func V2LiquidityUSD(
	ctx context.Context,
	client *ethclient.Client,
	chainCfg chain.Config,
	pair common.Address,
	nativeUsdPrice float64,
) (float64, error) {
	if pair == (common.Address{}) {
		return 0, fmt.Errorf("pair address is empty")
	}
	if nativeUsdPrice <= 0 {
		return 0, fmt.Errorf("nativeUsdPrice must be > 0")
	}

	token0, err := callAddress(ctx, client, pair, methodToken0[:])
	if err != nil {
		return 0, fmt.Errorf("token0: %w", err)
	}
	token1, err := callAddress(ctx, client, pair, methodToken1[:])
	if err != nil {
		return 0, fmt.Errorf("token1: %w", err)
	}

	reserve0, reserve1, err := callReserves(ctx, client, pair)
	if err != nil {
		return 0, err
	}

	quoteUSD, ok := quoteReserveUSD(chainCfg, token0, token1, reserve0, reserve1, nativeUsdPrice)
	if !ok {
		return 0, fmt.Errorf("pair has no known quote token")
	}
	return quoteUSD * 2, nil
}

func quoteReserveUSD(
	chainCfg chain.Config,
	token0, token1 common.Address,
	reserve0, reserve1 *big.Int,
	nativeUsdPrice float64,
) (float64, bool) {
	if q, ok := chainCfg.QuoteTokens[token0]; ok {
		return reserveToUSD(reserve0, q.Decimals, q.Symbol, nativeUsdPrice), true
	}
	if q, ok := chainCfg.QuoteTokens[token1]; ok {
		return reserveToUSD(reserve1, q.Decimals, q.Symbol, nativeUsdPrice), true
	}
	return 0, false
}

func reserveToUSD(amount *big.Int, decimals int, symbol string, nativeUsdPrice float64) float64 {
	if amount == nil || amount.Sign() <= 0 {
		return 0
	}
	f := new(big.Float).SetInt(amount)
	div := new(big.Float).SetFloat64(pow10(decimals))
	amt, _ := new(big.Float).Quo(f, div).Float64()
	switch symbol {
	case "WETH", "WBNB":
		return amt * nativeUsdPrice
	default:
		// USDC, USDT, DAI, BUSD ≈ $1
		return amt
	}
}

func callAddress(ctx context.Context, client *ethclient.Client, contract common.Address, data []byte) (common.Address, error) {
	out, err := client.CallContract(ctx, ethereumCall(contract, data), nil)
	if err != nil {
		return common.Address{}, err
	}
	if len(out) < 32 {
		return common.Address{}, fmt.Errorf("short token address response")
	}
	return common.BytesToAddress(out[12:32]), nil
}

func callReserves(ctx context.Context, client *ethclient.Client, pair common.Address) (*big.Int, *big.Int, error) {
	out, err := client.CallContract(ctx, ethereumCall(pair, methodGetReserves[:]), nil)
	if err != nil {
		return nil, nil, err
	}
	if len(out) < 64 {
		return nil, nil, fmt.Errorf("short getReserves response")
	}
	return new(big.Int).SetBytes(out[0:32]), new(big.Int).SetBytes(out[32:64]), nil
}

func pow10(n int) float64 {
	out := 1.0
	for i := 0; i < n; i++ {
		out *= 10
	}
	return out
}
