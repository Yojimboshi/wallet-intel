package execute

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// SwapRoute is a single-hop V2 path through an execution quote token.
type SwapRoute struct {
	Path       []common.Address
	QuoteToken common.Address
	NativeBuy  bool
	NativeSell bool
}

func PreferredQuote(chainCfg chain.Config, info enrich.TokenInfo) common.Address {
	if info.QuoteTokenAddress == "" {
		return common.Address{}
	}
	addr := common.HexToAddress(info.QuoteTokenAddress)
	if chainCfg.IsExecutionQuote(addr) {
		return addr
	}
	return common.Address{}
}

func buyAmountIn(chainCfg chain.Config, quote common.Address, tradeUsd, nativeUsdPrice float64) (*big.Int, error) {
	wrapped, ok := chainCfg.WrappedNative()
	if !ok {
		return nil, fmt.Errorf("wrapped native not configured")
	}
	if quote == wrapped {
		amt := usdToWei(tradeUsd, nativeUsdPrice)
		if amt.Sign() <= 0 {
			return nil, fmt.Errorf("buy size too small")
		}
		return amt, nil
	}
	dec, ok := chainCfg.QuoteTokenDecimals(quote)
	if !ok {
		return nil, fmt.Errorf("unknown quote token %s", quote.Hex())
	}
	amt := usdToTokenUnits(tradeUsd, dec)
	if amt.Sign() <= 0 {
		return nil, fmt.Errorf("buy size too small")
	}
	return amt, nil
}

func ResolveBuyRoute(
	ctx context.Context,
	client *ethclient.Client,
	chainCfg chain.Config,
	router, token, preferred common.Address,
	tradeUsd, nativeUsdPrice float64,
) (SwapRoute, *big.Int, *big.Int, error) {
	if client == nil || router == (common.Address{}) || token == (common.Address{}) {
		return SwapRoute{}, nil, nil, fmt.Errorf("invalid buy route params")
	}
	wrapped, _ := chainCfg.WrappedNative()
	for _, quote := range chainCfg.ExecutionQuoteCandidates(preferred) {
		amountIn, err := buyAmountIn(chainCfg, quote, tradeUsd, nativeUsdPrice)
		if err != nil {
			continue
		}
		path := []common.Address{quote, token}
		quoted, err := quoteV2AmountOut(ctx, client, router, amountIn, path)
		if err != nil || quoted == nil || quoted.Sign() <= 0 {
			continue
		}
		return SwapRoute{
			Path:       path,
			QuoteToken: quote,
			NativeBuy:  quote == wrapped,
		}, amountIn, quoted, nil
	}
	return SwapRoute{}, nil, nil, fmt.Errorf("no v2 buy route for %s", token.Hex())
}

func ResolveSellRoute(
	ctx context.Context,
	client *ethclient.Client,
	chainCfg chain.Config,
	router, token, preferred common.Address,
	amountIn *big.Int,
) (SwapRoute, *big.Int, error) {
	if client == nil || router == (common.Address{}) || token == (common.Address{}) {
		return SwapRoute{}, nil, fmt.Errorf("invalid sell route params")
	}
	if amountIn == nil || amountIn.Sign() <= 0 {
		return SwapRoute{}, nil, fmt.Errorf("amountIn must be positive")
	}
	wrapped, _ := chainCfg.WrappedNative()
	for _, quote := range chainCfg.ExecutionQuoteCandidates(preferred) {
		path := []common.Address{token, quote}
		quoted, err := quoteV2AmountOut(ctx, client, router, amountIn, path)
		if err != nil || quoted == nil || quoted.Sign() <= 0 {
			continue
		}
		return SwapRoute{
			Path:       path,
			QuoteToken: quote,
			NativeSell: quote == wrapped,
		}, quoted, nil
	}
	return SwapRoute{}, nil, fmt.Errorf("no v2 sell route for %s", token.Hex())
}
