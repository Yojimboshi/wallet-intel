package execute

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/pool"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// DirectSwap sends V2 router swaps (Uniswap on ETH, Pancake on BSC).
// Supports single-hop routes via wrapped-native, USDC, or USDT.
type DirectSwap struct {
	execClient   *ethclient.Client
	readClient   *ethclient.Client
	cfg          config.ExecutionConfig
	chainCfg     chain.Config
	chainID      string
	nativeOracle *pool.NativeOracle
}

func (d *DirectSwap) Mirror(ctx context.Context, req Request) (common.Hash, error) {
	w := req.ExecWallet
	if w.Address == (common.Address{}) {
		w = Wallet{Address: d.cfg.WalletAddress, PrivateKey: d.cfg.PrivateKey, Label: d.cfg.ActiveWallet}
	}
	if ok, reason := d.cfg.CanExecuteFor(config.FundedWallet{
		Label: w.Label, Address: w.Address, PrivateKey: w.PrivateKey,
	}, req.SizeUsd); !ok {
		return common.Hash{}, fmt.Errorf("%s", reason)
	}

	router := d.cfg.V2Router
	if router == (common.Address{}) {
		router = d.chainCfg.V2Router
	}
	if router == (common.Address{}) {
		return common.Hash{}, fmt.Errorf("no v2 router configured for chain %s", d.chainID)
	}

	nativePrice, err := d.nativeUSD(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("native usd price: %w", err)
	}
	read := balanceClient(d.readClient, d.execClient)

	token := req.Trade.Token
	if token == (common.Address{}) {
		return common.Hash{}, fmt.Errorf("token address is empty")
	}

	preferred := req.QuoteToken
	if preferred == (common.Address{}) {
		preferred = PreferredQuote(d.chainCfg, req.Token)
	}

	side := strings.ToLower(req.Trade.Side)
	if side == "transfer" {
		sym := req.Token.Symbol
		if sym == "" {
			sym = token.Hex()
		}
		recipient := req.TransferRecipient
		if recipient == (common.Address{}) {
			recipient = d.cfg.CollectorEVM
		}
		return d.executeTransfer(ctx, w, token, sym, recipient, read)
	}

	var quoteForFunds common.Address
	if side == "buy" {
		route, _, _, err := ResolveBuyRoute(ctx, read, d.chainCfg, router, token, preferred, req.SizeUsd, nativePrice)
		if err != nil {
			return common.Hash{}, fmt.Errorf("quote buy: %w", err)
		}
		quoteForFunds = route.QuoteToken
	}

	if read != nil && d.cfg.GasReserveUsd > 0 {
		ok, err := HasFundsForTrade(ctx, read, d.chainCfg, w.Address, req.Trade.Side, req.SizeUsd, d.cfg.GasReserveUsd, nativePrice, quoteForFunds)
		if err != nil {
			return common.Hash{}, fmt.Errorf("funds check: %w", err)
		}
		if !ok {
			if side == "buy" && quoteForFunds != (common.Address{}) {
				sym := d.chainCfg.QuoteSymbol(quoteForFunds)
				if sym == "" {
					sym = quoteForFunds.Hex()
				}
				return common.Hash{}, fmt.Errorf("wallet %s: insufficient %s or native gas reserve", w.Label, sym)
			}
			return common.Hash{}, fmt.Errorf("wallet %s: insufficient native for trade + gas reserve", w.Label)
		}
	}

	switch side {
	case "buy":
		sym := req.Token.Symbol
		if sym == "" {
			sym = token.Hex()
		}
		recipient := w.Address
		if req.TokenRecipient != (common.Address{}) {
			recipient = req.TokenRecipient
		}
		route, amountIn, _, err := ResolveBuyRoute(ctx, read, d.chainCfg, router, token, preferred, req.SizeUsd, nativePrice)
		if err != nil {
			return common.Hash{}, fmt.Errorf("quote buy: %w", err)
		}
		if !route.NativeBuy {
			if err := d.ensureAllowance(ctx, w, route.QuoteToken, router, amountIn); err != nil {
				return common.Hash{}, err
			}
		}
		tiers := buySlippageTiers(d.cfg)
		var lastErr error
		for i, slippageBps := range tiers {
			route, amountIn, quoted, err := ResolveBuyRoute(ctx, read, d.chainCfg, router, token, preferred, req.SizeUsd, nativePrice)
			if err != nil {
				return common.Hash{}, fmt.Errorf("quote buy: %w", err)
			}
			plan, err := buildV2BuyPlan(d.chainCfg, router, recipient, token, route, amountIn, slippageBps, quoted)
			if err != nil {
				return common.Hash{}, err
			}
			quoteSym := d.chainCfg.QuoteSymbol(plan.Path[0])
			if quoteSym == "" {
				quoteSym = plan.Path[0].Hex()
			}
			log.Printf(
				"COPY [%s] buy %s via v2 router %s quote=%s slippage=%dbps | in=%s minOut=%s | token %s | exec %s",
				d.chainID, sym, router.Hex(), quoteSym, slippageBps,
				plan.AmountIn.String(), plan.AmountOutMin.String(),
				token.Hex(), w.Label,
			)
			if req.TokenRecipient != (common.Address{}) && req.TokenRecipient != w.Address {
				log.Printf("COPY [%s] buy recipient %s (exec wallet %s)", d.chainID, req.TokenRecipient.Hex(), w.Address.Hex())
			}
			if d.cfg.SimulateSwaps {
				gas, err := estimateSwapGas(ctx, read, w.Address, plan)
				if err != nil {
					lastErr = err
					log.Printf("buy simulate %dbps failed: %v", slippageBps, err)
					if isSlippageBuyErr(err) && i+1 < len(tiers) {
						sleepBuyRetry(ctx)
						continue
					}
					return common.Hash{}, fmt.Errorf("simulate swap: %w", err)
				}
				log.Printf("SIMULATE [%s] %s gas≈%d slippage=%dbps — not broadcasting", d.chainID, sym, gas, slippageBps)
				return common.Hash{}, nil
			}
			if d.execClient == nil {
				return common.Hash{}, fmt.Errorf("execution rpc not configured")
			}
			if _, err := estimateSwapGas(ctx, read, w.Address, plan); err != nil {
				lastErr = err
				log.Printf("buy preflight %dbps failed: %v", slippageBps, err)
				if isSlippageBuyErr(err) && i+1 < len(tiers) {
					sleepBuyRetry(ctx)
					continue
				}
				return common.Hash{}, err
			}
			hash, err := signAndSend(ctx, d.execClient, w, d.chainCfg.ChainID, plan.To, plan.NativeValue, plan.Calldata, 0)
			if err != nil {
				lastErr = err
				log.Printf("buy broadcast %dbps failed: %v", slippageBps, err)
				if isSlippageBuyErr(err) && i+1 < len(tiers) {
					sleepBuyRetry(ctx)
					continue
				}
				return common.Hash{}, err
			}
			log.Printf("COPY TX [%s] %s hash=%s slippage=%dbps", d.chainID, sym, hash.Hex(), slippageBps)
			return hash, nil
		}
		return common.Hash{}, fmt.Errorf("buy failed after slippage retries: %w", lastErr)
	case "sell":
		sym := req.Token.Symbol
		if sym == "" {
			sym = token.Hex()
		}
		return common.Hash{}, d.executeSell(ctx, w, token, sym, router, read, preferred, req.SellFractionBps)
	default:
		return common.Hash{}, fmt.Errorf("unsupported trade side %q", req.Trade.Side)
	}
}

func (d *DirectSwap) ensureAllowance(ctx context.Context, w Wallet, token, router common.Address, need *big.Int) error {
	read := balanceClient(d.readClient, d.execClient)
	allow, err := erc20Allowance(ctx, read, token, w.Address, router)
	if err != nil {
		return fmt.Errorf("allowance: %w", err)
	}
	if allow.Cmp(need) >= 0 {
		return nil
	}
	if d.cfg.SimulateSwaps {
		log.Printf("SIMULATE approve %s for router %s", token.Hex(), router.Hex())
		return nil
	}
	data := encodeApprove(router, maxUint256)
	hash, err := signAndSend(ctx, d.execClient, w, d.chainCfg.ChainID, token, big.NewInt(0), data, 80_000)
	if err != nil {
		return fmt.Errorf("approve: %w", err)
	}
	log.Printf("APPROVE TX hash=%s", hash.Hex())
	receipt, err := waitReceipt(ctx, d.execClient, hash, 90*time.Second)
	if err != nil {
		return err
	}
	if receipt.Status == 0 {
		return fmt.Errorf("approve tx reverted")
	}
	return nil
}

func (d *DirectSwap) nativeUSD(ctx context.Context) (float64, error) {
	return nativeUSD(ctx, d.nativeOracle, d.cfg.NativeUsdPrice)
}
