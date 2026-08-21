package execute

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func sellSlippageTiers(cfg config.ExecutionConfig) []int {
	// Slippage ladder used for selling tokens with unstable price impact.
	// Interpreted as bps: [5%,15%,25%,35%] => [500,1500,2500,3500].
	_ = cfg // keep signature stable; ladder is intentionally fixed.
	return []int{500, 1500, 2500, 3500}
}

func buySlippageTiers(cfg config.ExecutionConfig) []int {
	first := cfg.SlippageBps
	if first <= 0 {
		first = 500
	}
	return []int{first, 1000}
}

func IsSlippageBuyErr(err error) bool {
	return isSlippageBuyErr(err)
}

func isSlippageBuyErr(err error) bool {
	if isSlippageSellErr(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "execution reverted") ||
		strings.Contains(msg, "estimate gas")
}

func sleepBuyRetry(ctx context.Context) {
	t := time.NewTimer(1500 * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func IsSlippageSellErr(err error) bool {
	return isSlippageSellErr(err)
}

func isSlippageSellErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "INSUFFICIENT_OUTPUT_AMOUNT") ||
		strings.Contains(msg, "INSUFFICIENT_OUTPUT") ||
		strings.Contains(msg, "EXCESSIVE_INPUT_AMOUNT") ||
		strings.Contains(msg, "PANCAKE: K") ||
		strings.Contains(msg, "UNISWAPV2: K")
}

func applySellFraction(balance *big.Int, fractionBps int) *big.Int {
	if balance == nil || balance.Sign() <= 0 {
		return big.NewInt(0)
	}
	if fractionBps <= 0 || fractionBps >= 10000 {
		return new(big.Int).Set(balance)
	}
	out := new(big.Int).Mul(balance, big.NewInt(int64(fractionBps)))
	return out.Div(out, big.NewInt(10000))
}

func (d *DirectSwap) executeSell(
	ctx context.Context,
	w Wallet,
	token common.Address,
	sym string,
	router common.Address,
	read *ethclient.Client,
	preferred common.Address,
	fractionBps int,
) error {
	balance, err := erc20Balance(ctx, read, token, w.Address)
	if err != nil {
		return fmt.Errorf("token balance: %w", err)
	}
	if balance.Sign() <= 0 {
		return fmt.Errorf("wallet %s: no %s balance to sell", w.Label, token.Hex())
	}
	amountIn := applySellFraction(balance, fractionBps)
	if amountIn.Sign() <= 0 {
		return fmt.Errorf("wallet %s: sell fraction %dbps of %s is zero", w.Label, fractionBps, token.Hex())
	}
	if err := d.ensureAllowance(ctx, w, token, router, amountIn); err != nil {
		return err
	}

	fracNote := "full"
	if fractionBps > 0 && fractionBps < 10000 {
		fracNote = fmt.Sprintf("%d%%", fractionBps/100)
	}

	var lastErr error
	for _, slippageBps := range sellSlippageTiers(d.cfg) {
		route, quoted, err := ResolveSellRoute(ctx, read, d.chainCfg, router, token, preferred, amountIn)
		if err != nil {
			return fmt.Errorf("quote sell: %w", err)
		}
		plan, err := buildV2SellPlan(d.chainCfg, router, w.Address, token, route, amountIn, slippageBps, quoted)
		if err != nil {
			return err
		}
		quoteSym := d.chainCfg.QuoteSymbol(route.QuoteToken)
		if quoteSym == "" {
			quoteSym = route.QuoteToken.Hex()
		}

		log.Printf(
			"COPY [%s] sell %s (%s) via v2 router %s quote=%s | slippage=%dbps in=%s minOut=%s | token %s | exec %s",
			d.chainID, sym, fracNote, router.Hex(), quoteSym, slippageBps,
			plan.AmountIn.String(), plan.AmountOutMin.String(),
			token.Hex(), w.Label,
		)

		if d.cfg.SimulateSwaps {
			gas, err := estimateSwapGas(ctx, read, w.Address, plan)
			if err != nil {
				lastErr = err
				log.Printf("sell simulate %dbps failed: %v", slippageBps, err)
				continue
			}
			log.Printf("SIMULATE [%s] %s gas≈%d slippage=%dbps — not broadcasting", d.chainID, sym, gas, slippageBps)
			return nil
		}
		if d.execClient == nil {
			return fmt.Errorf("execution rpc not configured")
		}

		if _, err := estimateSwapGas(ctx, read, w.Address, plan); err != nil {
			lastErr = err
			log.Printf("sell preflight %dbps failed: %v", slippageBps, err)
			continue
		}

		hash, err := signAndSend(ctx, d.execClient, w, d.chainCfg.ChainID, plan.To, plan.NativeValue, plan.Calldata, 0)
		if err != nil {
			lastErr = err
			log.Printf("sell broadcast %dbps failed: %v", slippageBps, err)
			continue
		}
		log.Printf("COPY TX [%s] %s hash=%s slippage=%dbps", d.chainID, sym, hash.Hex(), slippageBps)
		return nil
	}
	return fmt.Errorf("sell failed after slippage retries: %w", lastErr)
}
