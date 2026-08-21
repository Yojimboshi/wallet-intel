package execute

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/execute/ur"
	"github.com/Yojimboshi/wallet-intel/internal/pool"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// PoolExecutor rotates through execution wallets when balance is insufficient.
type PoolExecutor struct {
	execClient   *ethclient.Client
	readClient   *ethclient.Client
	chainCfg     chain.Config
	cfg          config.ExecutionConfig
	inner        Executor
	nativeOracle *pool.NativeOracle
}

func NewPool(execClient, readClient *ethclient.Client, cfg config.ExecutionConfig, chainCfg chain.Config, inner Executor, nativeOracle *pool.NativeOracle) *PoolExecutor {
	return &PoolExecutor{execClient: execClient, readClient: readClient, cfg: cfg, chainCfg: chainCfg, inner: inner, nativeOracle: nativeOracle}
}

func (p *PoolExecutor) Mirror(ctx context.Context, req Request) (common.Hash, error) {
	wallets := p.cfg.Wallets
	if len(wallets) == 0 {
		return p.inner.Mirror(ctx, req)
	}

	start := 0
	for i, w := range wallets {
		if w.Label == p.cfg.ActiveWallet {
			start = i
			break
		}
	}

	nativePrice, err := nativeUSD(ctx, p.nativeOracle, p.cfg.NativeUsdPrice)
	if err != nil {
		return common.Hash{}, err
	}

	read := balanceClient(p.readClient, p.execClient)
	router := p.cfg.V2Router
	if router == (common.Address{}) {
		router = p.chainCfg.V2Router
	}

	quoteForFunds := req.QuoteToken
	if strings.EqualFold(req.Trade.Side, "buy") && quoteForFunds == (common.Address{}) {
		preferred := PreferredQuote(p.chainCfg, req.Token)
		if p.chainCfg.UniversalRouter != (common.Address{}) {
			if plan, _, err := ur.ResolveBuyRoute(ctx, read, p.chainCfg, req.Trade.Token, preferred, req.SizeUsd, nativePrice, req.Token); err == nil {
				quoteForFunds = plan.TokenIn
				req.QuoteToken = plan.TokenIn
			}
		} else {
			route, _, _, err := ResolveBuyRoute(ctx, read, p.chainCfg, router, req.Trade.Token, preferred, req.SizeUsd, nativePrice)
			if err == nil {
				quoteForFunds = route.QuoteToken
				req.QuoteToken = route.QuoteToken
			}
		}
	}

	var lastReason string
	for i := 0; i < len(wallets); i++ {
		w := wallets[(start+i)%len(wallets)]
		ok, err := HasFundsForTrade(ctx, read, p.chainCfg, w.Address, req.Trade.Side, req.SizeUsd, p.cfg.GasReserveUsd, nativePrice, quoteForFunds)
		if err != nil {
			lastReason = fmt.Sprintf("%s balance check: %v", w.Label, err)
			log.Printf("copy rotate: %s", lastReason)
			continue
		}
		if !ok {
			if strings.EqualFold(req.Trade.Side, "sell") {
				lastReason = fmt.Sprintf("%s needs ≥$%.0f %s left for gas", w.Label, p.cfg.GasReserveUsd, p.chainCfg.NativeSymbol)
			} else if quoteForFunds != (common.Address{}) {
				sym := p.chainCfg.QuoteSymbol(quoteForFunds)
				if sym == "" {
					sym = quoteForFunds.Hex()
				}
				lastReason = fmt.Sprintf("%s insufficient %s or native gas reserve for ~$%.0f buy", w.Label, sym, req.SizeUsd)
			} else {
				lastReason = fmt.Sprintf("%s insufficient for ~$%.0f buy + $%.0f gas reserve", w.Label, req.SizeUsd, p.cfg.GasReserveUsd)
			}
			log.Printf("copy rotate: %s — trying next wallet", lastReason)
			continue
		}

		req.ExecWallet = Wallet{Label: w.Label, Address: w.Address, PrivateKey: w.PrivateKey}
		log.Printf("copy using wallet %s (%s)", w.Label, w.Address.Hex())
		hash, err := p.inner.Mirror(ctx, req)
		if err != nil {
			return common.Hash{}, err
		}
		return hash, nil
	}
	if lastReason == "" {
		lastReason = "no wallets configured"
	}
	return common.Hash{}, fmt.Errorf("no funded execution wallet: %s", lastReason)
}
