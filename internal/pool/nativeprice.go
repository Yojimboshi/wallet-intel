package pool

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// NativePriceStore persists last-known wrapped-native USD prices (optional MySQL).
type NativePriceStore interface {
	GetNativePrice(ctx context.Context, chain string) (float64, error)
	SaveNativePrice(ctx context.Context, chain string, priceUsd float64) error
}

// NativeOracle reads wrapped-native/stable V2 pool reserves for live ETH/BNB USD price.
type NativeOracle struct {
	client   *ethclient.Client
	chainCfg chain.Config
	fallback float64
	store    NativePriceStore
	ttl      time.Duration

	mu       sync.Mutex
	cached   float64
	cachedAt time.Time
}

func NewNativeOracle(client *ethclient.Client, chainCfg chain.Config, fallback float64) *NativeOracle {
	return &NativeOracle{
		client:   client,
		chainCfg: chainCfg,
		fallback: fallback,
		ttl:      3 * time.Hour,
	}
}

func (o *NativeOracle) UseStore(store NativePriceStore) {
	o.store = store
}

func (o *NativeOracle) USD(ctx context.Context) (float64, error) {
	if o == nil {
		return 0, fmt.Errorf("native oracle is nil")
	}

	o.mu.Lock()
	if o.cached > 0 && time.Since(o.cachedAt) < o.ttl {
		price := o.cached
		o.mu.Unlock()
		return price, nil
	}
	o.mu.Unlock()

	price, err := o.fetch(ctx)
	if err != nil {
		if p, dbErr := o.dbPrice(ctx); dbErr == nil {
			o.mu.Lock()
			o.cached = p
			o.cachedAt = time.Now().UTC()
			o.mu.Unlock()
			return p, nil
		}
		if o.fallback > 0 {
			return o.fallback, nil
		}
		return 0, err
	}

	if o.store != nil {
		_ = o.store.SaveNativePrice(ctx, string(o.chainCfg.ID), price)
	}

	o.mu.Lock()
	o.cached = price
	o.cachedAt = time.Now().UTC()
	o.mu.Unlock()
	return price, nil
}

func (o *NativeOracle) fetch(ctx context.Context) (float64, error) {
	if o.client == nil {
		return 0, fmt.Errorf("rpc client is nil")
	}
	if o.chainCfg.NativeUSDPool == (common.Address{}) {
		return 0, fmt.Errorf("no native USD pool configured for %s", o.chainCfg.ID)
	}
	return NativeUSDFromV2Pair(ctx, o.client, o.chainCfg, o.chainCfg.NativeUSDPool)
}

func (o *NativeOracle) dbPrice(ctx context.Context) (float64, error) {
	if o.store == nil {
		return 0, fmt.Errorf("no native price store")
	}
	return o.store.GetNativePrice(ctx, string(o.chainCfg.ID))
}

// NativeUSDFromV2Pair prices wrapped native (WETH/WBNB) from a V2 pair vs USDC/USDT/DAI/BUSD.
func NativeUSDFromV2Pair(
	ctx context.Context,
	client *ethclient.Client,
	chainCfg chain.Config,
	pair common.Address,
) (float64, error) {
	if pair == (common.Address{}) {
		return 0, fmt.Errorf("pair address is empty")
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

	q0, ok0 := chainCfg.QuoteTokens[token0]
	q1, ok1 := chainCfg.QuoteTokens[token1]
	if !ok0 || !ok1 {
		return 0, fmt.Errorf("pair tokens not in chain quote list")
	}

	if isWrappedNative(q0.Symbol) && isStableQuote(q1.Symbol) {
		return nativePriceFromReserves(reserve0, q0.Decimals, reserve1, q1.Decimals)
	}
	if isWrappedNative(q1.Symbol) && isStableQuote(q0.Symbol) {
		return nativePriceFromReserves(reserve1, q1.Decimals, reserve0, q0.Decimals)
	}
	return 0, fmt.Errorf("pair %s is not wrapped-native/stable", pair.Hex())
}

func nativePriceFromReserves(nativeReserve *big.Int, nativeDecimals int, stableReserve *big.Int, stableDecimals int) (float64, error) {
	nativeAmt := amountFloat(nativeReserve, nativeDecimals)
	stableAmt := amountFloat(stableReserve, stableDecimals)
	if nativeAmt <= 0 || stableAmt <= 0 {
		return 0, fmt.Errorf("zero pool reserves")
	}
	return stableAmt / nativeAmt, nil
}

func isWrappedNative(symbol string) bool {
	switch strings.ToUpper(symbol) {
	case "WETH", "WBNB":
		return true
	default:
		return false
	}
}

func isStableQuote(symbol string) bool {
	switch strings.ToUpper(symbol) {
	case "USDC", "USDT", "DAI", "BUSD":
		return true
	default:
		return false
	}
}

func amountFloat(amount *big.Int, decimals int) float64 {
	if amount == nil || amount.Sign() <= 0 {
		return 0
	}
	if decimals <= 0 {
		decimals = 18
	}
	f := new(big.Float).SetInt(amount)
	div := new(big.Float).SetFloat64(pow10(decimals))
	out, _ := new(big.Float).Quo(f, div).Float64()
	return out
}
