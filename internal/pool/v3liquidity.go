package pool

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	methodSlot0     = [4]byte{0x38, 0x50, 0xc7, 0xbd}
	methodLiquidity = [4]byte{0x1a, 0x68, 0x65, 0x02}
	methodBalanceOf = [4]byte{0x70, 0xa0, 0x82, 0x31}
	q96             = new(big.Int).Lsh(big.NewInt(1), 96)
)

// Snapshot is on-chain pool state used for liquidity monitoring.
type Snapshot struct {
	Kind          string
	TVLUsd        float64
	ActiveLiqUsd  float64
	SqrtPriceX96  *big.Int
	PoolLiquidity *big.Int
	CurrentTick   int
	Token0Balance *big.Int
	Token1Balance *big.Int
}

// LiquidityUSD reads pool TVL on-chain. Auto-detects V2 vs V3.
func LiquidityUSD(
	ctx context.Context,
	client *ethclient.Client,
	chainCfg chain.Config,
	pair common.Address,
	dexHint string,
	nativeUsdPrice float64,
) (float64, error) {
	snap, err := ReadSnapshot(ctx, client, chainCfg, pair, dexHint, nativeUsdPrice)
	if err != nil {
		return 0, err
	}
	return snap.TVLUsd, nil
}

func ReadSnapshot(
	ctx context.Context,
	client *ethclient.Client,
	chainCfg chain.Config,
	pair common.Address,
	dexHint string,
	nativeUsdPrice float64,
) (Snapshot, error) {
	if pair == (common.Address{}) {
		return Snapshot{}, fmt.Errorf("pair address is empty")
	}
	if nativeUsdPrice <= 0 {
		return Snapshot{}, fmt.Errorf("nativeUsdPrice must be > 0")
	}
	if isV3Hint(dexHint) {
		return readV3Snapshot(ctx, client, chainCfg, pair, nativeUsdPrice)
	}
	if isV2Hint(dexHint) {
		tvl, err := V2LiquidityUSD(ctx, client, chainCfg, pair, nativeUsdPrice)
		if err != nil {
			return Snapshot{}, err
		}
		return Snapshot{Kind: "v2", TVLUsd: tvl, ActiveLiqUsd: tvl}, nil
	}
	if probeV3(ctx, client, pair) {
		return readV3Snapshot(ctx, client, chainCfg, pair, nativeUsdPrice)
	}
	tvl, err := V2LiquidityUSD(ctx, client, chainCfg, pair, nativeUsdPrice)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Kind: "v2", TVLUsd: tvl, ActiveLiqUsd: tvl}, nil
}

func isV3Hint(dex string) bool {
	d := strings.ToLower(dex)
	return strings.Contains(d, "v3") || strings.Contains(d, "pancake-v3")
}

func isV2Hint(dex string) bool {
	d := strings.ToLower(dex)
	return strings.Contains(d, "v2") && !strings.Contains(d, "v3")
}

func probeV3(ctx context.Context, client *ethclient.Client, pair common.Address) bool {
	_, err := client.CallContract(ctx, ethereumCall(pair, methodSlot0[:]), nil)
	return err == nil
}

func readV3Snapshot(
	ctx context.Context,
	client *ethclient.Client,
	chainCfg chain.Config,
	pool common.Address,
	nativeUsdPrice float64,
) (Snapshot, error) {
	token0, err := callAddress(ctx, client, pool, methodToken0[:])
	if err != nil {
		return Snapshot{}, fmt.Errorf("token0: %w", err)
	}
	token1, err := callAddress(ctx, client, pool, methodToken1[:])
	if err != nil {
		return Snapshot{}, fmt.Errorf("token1: %w", err)
	}

	bal0, err := erc20BalanceOf(ctx, client, token0, pool)
	if err != nil {
		return Snapshot{}, fmt.Errorf("balance0: %w", err)
	}
	bal1, err := erc20BalanceOf(ctx, client, token1, pool)
	if err != nil {
		return Snapshot{}, fmt.Errorf("balance1: %w", err)
	}

	tvl := tokenBalanceUSD(chainCfg, token0, bal0, nativeUsdPrice) +
		tokenBalanceUSD(chainCfg, token1, bal1, nativeUsdPrice)

	sqrtPriceX96, currentTick, err := callSlot0(ctx, client, pool)
	if err != nil {
		return Snapshot{
			Kind: "v3", TVLUsd: tvl, ActiveLiqUsd: tvl,
			Token0Balance: bal0, Token1Balance: bal1,
		}, nil
	}

	poolLiq, err := callUint256(ctx, client, pool, methodLiquidity[:])
	if err != nil {
		return Snapshot{
			Kind: "v3", TVLUsd: tvl, ActiveLiqUsd: tvl,
			SqrtPriceX96: sqrtPriceX96, CurrentTick: currentTick,
			Token0Balance: bal0, Token1Balance: bal1,
		}, nil
	}

	// Active depth at current tick (±1 tick band around slot0 tick).
	sqrtL := getSqrtRatioAtTick(currentTick - 1)
	sqrtU := getSqrtRatioAtTick(currentTick + 1)
	amount0, amount1 := amountsForLiquidity(sqrtPriceX96, sqrtL, sqrtU, poolLiq)
	activeUsd := tokenBalanceUSD(chainCfg, token0, amount0, nativeUsdPrice) +
		tokenBalanceUSD(chainCfg, token1, amount1, nativeUsdPrice)
	if activeUsd <= 0 {
		activeUsd = tvl
	}

	return Snapshot{
		Kind: "v3", TVLUsd: tvl, ActiveLiqUsd: activeUsd,
		SqrtPriceX96: sqrtPriceX96, PoolLiquidity: poolLiq, CurrentTick: currentTick,
		Token0Balance: bal0, Token1Balance: bal1,
	}, nil
}

func tokenBalanceUSD(chainCfg chain.Config, token common.Address, amount *big.Int, nativeUsdPrice float64) float64 {
	if amount == nil || amount.Sign() <= 0 {
		return 0
	}
	if q, ok := chainCfg.QuoteTokens[token]; ok {
		return reserveToUSD(amount, q.Decimals, q.Symbol, nativeUsdPrice)
	}
	return 0
}

func erc20BalanceOf(ctx context.Context, client *ethclient.Client, token, holder common.Address) (*big.Int, error) {
	padded := common.LeftPadBytes(holder.Bytes(), 32)
	data := append(methodBalanceOf[:], padded...)
	out, err := client.CallContract(ctx, ethereumCall(token, data), nil)
	if err != nil {
		return nil, err
	}
	if len(out) < 32 {
		return nil, fmt.Errorf("short balanceOf response")
	}
	return new(big.Int).SetBytes(out[len(out)-32:]), nil
}

func callSlot0(ctx context.Context, client *ethclient.Client, pool common.Address) (*big.Int, int, error) {
	out, err := client.CallContract(ctx, ethereumCall(pool, methodSlot0[:]), nil)
	if err != nil {
		return nil, 0, err
	}
	if len(out) < 64 {
		return nil, 0, fmt.Errorf("short slot0 response")
	}
	sqrtPriceX96 := new(big.Int).SetBytes(out[0:32])
	tick := parseSignedInt24(out[32:64])
	return sqrtPriceX96, tick, nil
}

func callUint256(ctx context.Context, client *ethclient.Client, contract common.Address, data []byte) (*big.Int, error) {
	out, err := client.CallContract(ctx, ethereumCall(contract, data), nil)
	if err != nil {
		return nil, err
	}
	if len(out) < 32 {
		return nil, fmt.Errorf("short uint256 response")
	}
	return new(big.Int).SetBytes(out[len(out)-32:]), nil
}

func parseSignedInt24(word []byte) int {
	if len(word) < 32 {
		return 0
	}
	v := new(big.Int).SetBytes(word)
	if word[len(word)-3]&0x80 != 0 {
		two256 := new(big.Int).Lsh(big.NewInt(1), 256)
		v.Sub(v, two256)
	}
	return int(v.Int64())
}
