package execute

import (
	"context"
	"fmt"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/execute/ur"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ResolveCopyQuoteToken finds the spend token (native/USDC/USDT) for a copy buy.
func ResolveCopyQuoteToken(
	ctx context.Context,
	client *ethclient.Client,
	chainCfg chain.Config,
	token, preferred common.Address,
	tradeUsd, nativeUsdPrice float64,
	info enrich.TokenInfo,
) (common.Address, error) {
	if client == nil {
		return common.Address{}, fmt.Errorf("rpc client is nil")
	}
	if chainCfg.UniversalRouter != (common.Address{}) {
		plan, _, err := ur.ResolveBuyRoute(ctx, client, chainCfg, token, preferred, tradeUsd, nativeUsdPrice, info)
		if err != nil {
			return common.Address{}, err
		}
		return plan.TokenIn, nil
	}
	router := chainCfg.V2Router
	plan, _, _, err := ResolveBuyRoute(ctx, client, chainCfg, router, token, preferred, tradeUsd, nativeUsdPrice)
	if err != nil {
		return common.Address{}, err
	}
	return plan.QuoteToken, nil
}
