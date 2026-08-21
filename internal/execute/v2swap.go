package execute

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type v2SwapPlan struct {
	Router       common.Address
	To           common.Address
	AmountIn     *big.Int
	AmountOutMin *big.Int
	Path         []common.Address
	Deadline     *big.Int
	NativeValue  *big.Int
	Calldata     []byte
}

func quoteV2AmountOut(ctx context.Context, client *ethclient.Client, router common.Address, amountIn *big.Int, path []common.Address) (*big.Int, error) {
	return QuoteV2AmountOut(ctx, client, router, amountIn, path)
}

// QuoteV2AmountOut calls getAmountsOut on a V2 router.
func QuoteV2AmountOut(ctx context.Context, client *ethclient.Client, router common.Address, amountIn *big.Int, path []common.Address) (*big.Int, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, fmt.Errorf("amountIn must be positive")
	}
	data := encodeGetAmountsOut(amountIn, path)
	out, err := client.CallContract(ctx, callMsg(router, data), nil)
	if err != nil {
		return nil, fmt.Errorf("getAmountsOut: %w", err)
	}
	return decodeAmountsOut(out)
}

func buildV2BuyPlan(
	chainCfg chain.Config,
	router common.Address,
	recipient common.Address,
	tokenOut common.Address,
	route SwapRoute,
	amountIn *big.Int,
	slippageBps int,
	quotedOut *big.Int,
) (v2SwapPlan, error) {
	if len(route.Path) != 2 {
		return v2SwapPlan{}, fmt.Errorf("buy route must be single-hop")
	}
	minOut := applySlippage(quotedOut, slippageBps)
	deadline := big.NewInt(time.Now().Add(20 * time.Minute).Unix())
	if route.NativeBuy {
		data := encodeSwapExactETHForTokens(minOut, route.Path, recipient, deadline)
		return v2SwapPlan{
			Router:       router,
			To:           router,
			AmountIn:     amountIn,
			AmountOutMin: minOut,
			Path:         route.Path,
			Deadline:     deadline,
			NativeValue:  amountIn,
			Calldata:     data,
		}, nil
	}
	data := encodeSwapExactTokensForTokensSupportingFee(amountIn, minOut, route.Path, recipient, deadline)
	return v2SwapPlan{
		Router:       router,
		To:           router,
		AmountIn:     amountIn,
		AmountOutMin: minOut,
		Path:         route.Path,
		Deadline:     deadline,
		NativeValue:  big.NewInt(0),
		Calldata:     data,
	}, nil
}

func buildV2SellPlan(
	chainCfg chain.Config,
	router common.Address,
	recipient common.Address,
	tokenIn common.Address,
	route SwapRoute,
	amountIn *big.Int,
	slippageBps int,
	quotedOut *big.Int,
) (v2SwapPlan, error) {
	if len(route.Path) != 2 {
		return v2SwapPlan{}, fmt.Errorf("sell route must be single-hop")
	}
	minOut := applySlippage(quotedOut, slippageBps)
	deadline := big.NewInt(time.Now().Add(20 * time.Minute).Unix())
	if route.NativeSell {
		data := encodeSwapExactTokensForETHSupportingFee(amountIn, minOut, route.Path, recipient, deadline)
		return v2SwapPlan{
			Router:       router,
			To:           router,
			AmountIn:     amountIn,
			AmountOutMin: minOut,
			Path:         route.Path,
			Deadline:     deadline,
			NativeValue:  big.NewInt(0),
			Calldata:     data,
		}, nil
	}
	data := encodeSwapExactTokensForTokensSupportingFee(amountIn, minOut, route.Path, recipient, deadline)
	return v2SwapPlan{
		Router:       router,
		To:           router,
		AmountIn:     amountIn,
		AmountOutMin: minOut,
		Path:         route.Path,
		Deadline:     deadline,
		NativeValue:  big.NewInt(0),
		Calldata:     data,
	}, nil
}
