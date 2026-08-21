package ur

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func ResolveBuyRoute(
	ctx context.Context,
	client *ethclient.Client,
	chainCfg chain.Config,
	tokenOut, preferredQuote common.Address,
	tradeUsd, nativeUsdPrice float64,
	info enrich.TokenInfo,
) (RoutePlan, *big.Int, error) {
	if client == nil {
		return RoutePlan{}, nil, fmt.Errorf("rpc client is nil")
	}
	for _, quote := range chainCfg.ExecutionQuoteCandidates(preferredQuote) {
		amountIn, nativeIn, err := buyAmountIn(chainCfg, quote, tradeUsd, nativeUsdPrice)
		if err != nil || amountIn.Sign() <= 0 {
			continue
		}
		if plan, err := trySingleHopBuy(ctx, client, chainCfg, quote, tokenOut, amountIn, nativeIn); err == nil {
			return plan, amountIn, nil
		}
	}
	hub := common.HexToAddress(info.QuoteTokenAddress)
	if hub != (common.Address{}) && !chainCfg.IsExecutionQuote(hub) && hub != tokenOut {
		for _, quote := range chainCfg.ExecutionQuoteCandidates(preferredQuote) {
			amountIn, nativeIn, err := buyAmountIn(chainCfg, quote, tradeUsd, nativeUsdPrice)
			if err != nil || amountIn.Sign() <= 0 || nativeIn {
				continue // hub 2-hop expects ERC20 quote (USDT/USDC)
			}
			if plan, err := tryTwoHopBuy(ctx, client, chainCfg, quote, hub, tokenOut, amountIn); err == nil {
				return plan, amountIn, nil
			}
		}
	}
	return RoutePlan{}, nil, fmt.Errorf("no universal router buy route for %s", tokenOut.Hex())
}

func ResolveSellRoute(
	ctx context.Context,
	client *ethclient.Client,
	chainCfg chain.Config,
	tokenIn, preferredQuote, hubToken common.Address,
	amountIn *big.Int,
) (RoutePlan, *big.Int, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return RoutePlan{}, nil, fmt.Errorf("amountIn must be positive")
	}
	wrapped, _ := chainCfg.WrappedNative()
	for _, quote := range chainCfg.ExecutionQuoteCandidates(preferredQuote) {
		nativeOut := quote == wrapped
		if plan, err := trySingleHopSell(ctx, client, chainCfg, tokenIn, quote, nativeOut); err == nil {
			plan.QuotedOut, err = quoteOut(ctx, client, chainCfg, plan, amountIn)
			if err != nil {
				continue
			}
			return plan, amountIn, nil
		}
	}
	if hubToken != (common.Address{}) && !chainCfg.IsExecutionQuote(hubToken) {
		for _, quote := range chainCfg.ExecutionQuoteCandidates(preferredQuote) {
			if quote == wrapped {
				continue
			}
			if plan, err := tryTwoHopSell(ctx, client, chainCfg, tokenIn, hubToken, quote); err == nil {
				hop1Out, err := quoteHop(ctx, client, chainCfg, tokenIn, hubToken, plan.Hop1Venue, plan.Hop1V3Fee, amountIn)
				if err != nil {
					continue
				}
				plan.Hop1Quoted = hop1Out
				out, err := quoteHop(ctx, client, chainCfg, hubToken, quote, plan.Hop2Venue, 0, hop1Out)
				if err != nil {
					continue
				}
				plan.QuotedOut = out
				plan.TokenOut = quote
				return plan, amountIn, nil
			}
		}
	}
	return RoutePlan{}, nil, fmt.Errorf("no universal router sell route for %s", tokenIn.Hex())
}

func trySingleHopBuy(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, tokenIn, tokenOut common.Address, amountIn *big.Int, nativeIn bool) (RoutePlan, error) {
	if out, fee, err := QuoteV3SingleHop(ctx, client, chainCfg, tokenIn, tokenOut, amountIn, 0); err == nil {
		return RoutePlan{TokenIn: tokenIn, TokenOut: tokenOut, Venue: VenueV3, V3Fee: fee, NativeIn: nativeIn, QuotedOut: out}, nil
	}
	if out, err := QuoteV2SingleHop(ctx, client, chainCfg, tokenIn, tokenOut, amountIn); err == nil {
		return RoutePlan{TokenIn: tokenIn, TokenOut: tokenOut, Venue: VenueV2, NativeIn: nativeIn, QuotedOut: out}, nil
	}
	return RoutePlan{}, fmt.Errorf("no single-hop route")
}

func tryTwoHopBuy(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, tokenIn, hub, tokenOut common.Address, amountIn *big.Int) (RoutePlan, error) {
	var hop1Out *big.Int
	var hop1Fee uint32
	var hop1Venue Venue
	if out, fee, err := QuoteV3SingleHop(ctx, client, chainCfg, tokenIn, hub, amountIn, 0); err == nil {
		hop1Out, hop1Fee, hop1Venue = out, fee, VenueV3
	} else if out, err := QuoteV2SingleHop(ctx, client, chainCfg, tokenIn, hub, amountIn); err == nil {
		hop1Out, hop1Venue = out, VenueV2
	} else {
		return RoutePlan{}, fmt.Errorf("hop1 failed")
	}
	var hop2Out *big.Int
	var hop2Venue Venue
	var hop2Fee uint32
	if out, err := QuoteV2SingleHop(ctx, client, chainCfg, hub, tokenOut, hop1Out); err == nil {
		hop2Out, hop2Venue = out, VenueV2
	} else if out, fee, err := QuoteV3SingleHop(ctx, client, chainCfg, hub, tokenOut, hop1Out, 0); err == nil {
		hop2Out, hop2Venue, hop2Fee = out, VenueV3, fee
	} else {
		return RoutePlan{}, fmt.Errorf("hop2 failed")
	}
	return RoutePlan{
		TokenIn:    tokenIn,
		TokenOut:   tokenOut,
		HubToken:   hub,
		TwoHop:     true,
		Hop1Venue:  hop1Venue,
		Hop1V3Fee:  hop1Fee,
		Hop2Venue:  hop2Venue,
		Hop2V3Fee:  hop2Fee,
		Hop1Quoted: hop1Out,
		QuotedOut:  hop2Out,
	}, nil
}

func trySingleHopSell(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, tokenIn, tokenOut common.Address, nativeOut bool) (RoutePlan, error) {
	probe := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	if _, fee, err := QuoteV3SingleHop(ctx, client, chainCfg, tokenIn, tokenOut, probe, 0); err == nil {
		return RoutePlan{TokenIn: tokenIn, TokenOut: tokenOut, Venue: VenueV3, V3Fee: fee, NativeOut: nativeOut}, nil
	}
	if _, err := QuoteV2SingleHop(ctx, client, chainCfg, tokenIn, tokenOut, probe); err == nil {
		return RoutePlan{TokenIn: tokenIn, TokenOut: tokenOut, Venue: VenueV2, NativeOut: nativeOut}, nil
	}
	return RoutePlan{}, fmt.Errorf("no single-hop sell route")
}

func tryTwoHopSell(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, tokenIn, hub, tokenOut common.Address) (RoutePlan, error) {
	probe := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	var hop1Venue Venue
	var hop1Fee uint32
	if _, fee, err := QuoteV3SingleHop(ctx, client, chainCfg, tokenIn, hub, probe, 0); err == nil {
		hop1Venue, hop1Fee = VenueV3, fee
	} else if _, err := QuoteV2SingleHop(ctx, client, chainCfg, tokenIn, hub, probe); err == nil {
		hop1Venue = VenueV2
	} else {
		return RoutePlan{}, fmt.Errorf("sell hop1 probe failed")
	}
	var hop2Venue Venue
	if _, err := QuoteV2SingleHop(ctx, client, chainCfg, hub, tokenOut, probe); err == nil {
		hop2Venue = VenueV2
	} else if _, _, err := QuoteV3SingleHop(ctx, client, chainCfg, hub, tokenOut, probe, 0); err == nil {
		hop2Venue = VenueV3
	} else {
		return RoutePlan{}, fmt.Errorf("sell hop2 probe failed")
	}
	return RoutePlan{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		HubToken:  hub,
		TwoHop:    true,
		Hop1Venue: hop1Venue,
		Hop1V3Fee: hop1Fee,
		Hop2Venue: hop2Venue,
	}, nil
}

func quoteOut(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, plan RoutePlan, amountIn *big.Int) (*big.Int, error) {
	if plan.Venue == VenueV2 {
		return QuoteV2SingleHop(ctx, client, chainCfg, plan.TokenIn, plan.TokenOut, amountIn)
	}
	out, _, err := QuoteV3SingleHop(ctx, client, chainCfg, plan.TokenIn, plan.TokenOut, amountIn, plan.V3Fee)
	return out, err
}

func quoteHop(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, tokenIn, tokenOut common.Address, venue Venue, fee uint32, amountIn *big.Int) (*big.Int, error) {
	if venue == VenueV2 {
		return QuoteV2SingleHop(ctx, client, chainCfg, tokenIn, tokenOut, amountIn)
	}
	out, _, err := QuoteV3SingleHop(ctx, client, chainCfg, tokenIn, tokenOut, amountIn, fee)
	return out, err
}

func buyAmountIn(chainCfg chain.Config, quote common.Address, tradeUsd, nativeUsdPrice float64) (*big.Int, bool, error) {
	wrapped, ok := chainCfg.WrappedNative()
	if !ok {
		return nil, false, fmt.Errorf("wrapped native not configured")
	}
	if quote == wrapped {
		amt := usdToWei(tradeUsd, nativeUsdPrice)
		if amt.Sign() <= 0 {
			return nil, true, fmt.Errorf("buy size too small")
		}
		return amt, true, nil
	}
	dec, ok := chainCfg.QuoteTokenDecimals(quote)
	if !ok {
		return nil, false, fmt.Errorf("unknown quote")
	}
	amt := usdToTokenUnits(tradeUsd, dec)
	if amt.Sign() <= 0 {
		return nil, false, fmt.Errorf("buy size too small")
	}
	return amt, false, nil
}

func usdToWei(usd, nativeUsdPrice float64) *big.Int {
	if usd <= 0 || nativeUsdPrice <= 0 {
		return big.NewInt(0)
	}
	ether := usd / nativeUsdPrice
	f := new(big.Float).SetFloat64(ether)
	weiFloat := new(big.Float).Mul(f, new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)))
	wei, _ := weiFloat.Int(nil)
	return wei
}

func usdToTokenUnits(usd float64, decimals int) *big.Int {
	if usd <= 0 {
		return big.NewInt(0)
	}
	if decimals <= 0 {
		decimals = 18
	}
	scale := new(big.Float).SetFloat64(usd)
	mul := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	out, _ := new(big.Float).Mul(scale, mul).Int(nil)
	return out
}
