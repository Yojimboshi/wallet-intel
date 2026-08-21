package execute

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/execute/ur"
	"github.com/Yojimboshi/wallet-intel/internal/pool"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// UniversalSwap executes copies via Uniswap Universal Router / Pancake Infinity UR.
type UniversalSwap struct {
	execClient   *ethclient.Client
	readClient   *ethclient.Client
	cfg          config.ExecutionConfig
	chainCfg     chain.Config
	chainID      string
	nativeOracle *pool.NativeOracle
}

func (u *UniversalSwap) Mirror(ctx context.Context, req Request) (common.Hash, error) {
	w := req.ExecWallet
	if w.Address == (common.Address{}) {
		w = Wallet{Address: u.cfg.WalletAddress, PrivateKey: u.cfg.PrivateKey, Label: u.cfg.ActiveWallet}
	}
	if ok, reason := u.cfg.CanExecuteFor(config.FundedWallet{
		Label: w.Label, Address: w.Address, PrivateKey: w.PrivateKey,
	}, req.SizeUsd); !ok {
		return common.Hash{}, fmt.Errorf("%s", reason)
	}
	if u.chainCfg.UniversalRouter == (common.Address{}) {
		return common.Hash{}, fmt.Errorf("universal router not configured for chain %s", u.chainID)
	}

	nativePrice, err := u.nativeUSD(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("native usd price: %w", err)
	}
	read := balanceClient(u.readClient, u.execClient)
	token := req.Trade.Token
	if token == (common.Address{}) {
		return common.Hash{}, fmt.Errorf("token address is empty")
	}

	side := strings.ToLower(req.Trade.Side)
	if side == "transfer" {
		d := &DirectSwap{execClient: u.execClient, readClient: u.readClient, cfg: u.cfg, chainCfg: u.chainCfg, chainID: u.chainID, nativeOracle: u.nativeOracle}
		return d.executeTransfer(ctx, w, token, req.Token.Symbol, req.TransferRecipient, read)
	}

	preferred := req.QuoteToken
	if preferred == (common.Address{}) {
		preferred = PreferredQuote(u.chainCfg, req.Token)
	}
	hub := req.HubToken
	if hub == (common.Address{}) {
		hub = common.HexToAddress(req.Token.QuoteTokenAddress)
	}
	if u.chainCfg.IsExecutionQuote(hub) || hub == token {
		hub = common.Address{}
	}

	switch side {
	case "buy":
		return u.mirrorBuy(ctx, w, req, token, preferred, read, nativePrice)
	case "sell":
		return u.mirrorSell(ctx, w, req, token, preferred, hub, read)
	default:
		return common.Hash{}, fmt.Errorf("unsupported trade side %q", req.Trade.Side)
	}
}

func (u *UniversalSwap) mirrorBuy(ctx context.Context, w Wallet, req Request, token, preferred common.Address, read *ethclient.Client, nativePrice float64) (common.Hash, error) {
	plan, amountIn, err := ur.ResolveBuyRoute(ctx, read, u.chainCfg, token, preferred, req.SizeUsd, nativePrice, req.Token)
	if err != nil {
		return common.Hash{}, fmt.Errorf("quote buy: %w", err)
	}
	quoteForFunds := plan.TokenIn
	if read != nil && u.cfg.GasReserveUsd > 0 {
		ok, err := HasFundsForTrade(ctx, read, u.chainCfg, w.Address, "buy", req.SizeUsd, u.cfg.GasReserveUsd, nativePrice, quoteForFunds)
		if err != nil {
			return common.Hash{}, fmt.Errorf("funds check: %w", err)
		}
		if !ok {
			sym := u.chainCfg.QuoteSymbol(quoteForFunds)
			return common.Hash{}, fmt.Errorf("wallet %s: insufficient %s or native gas reserve", w.Label, sym)
		}
	}
	recipient := w.Address
	if req.TokenRecipient != (common.Address{}) {
		recipient = req.TokenRecipient
	}
	if !plan.NativeIn {
		if err := ensureURPermit2Ready(ctx, u.execClient, read, u.chainCfg, w, plan.TokenIn, amountIn, u.cfg.SimulateSwaps); err != nil {
			return common.Hash{}, err
		}
	}

	sym := req.Token.Symbol
	if sym == "" {
		sym = token.Hex()
	}
	tiers := buySlippageTiers(u.cfg)
	var lastErr error
	for i, slippageBps := range tiers {
		plan, amountIn, err = ur.ResolveBuyRoute(ctx, read, u.chainCfg, token, preferred, req.SizeUsd, nativePrice, req.Token)
		if err != nil {
			return common.Hash{}, fmt.Errorf("quote buy: %w", err)
		}
		payload, err := ur.BuildBuyPayload(u.chainCfg, plan, amountIn, recipient, slippageBps)
		if err != nil {
			return common.Hash{}, err
		}
		log.Printf("COPY [%s] buy %s via UR %s slippage=%dbps", u.chainID, sym, u.chainCfg.UniversalRouter.Hex(), slippageBps)
		if u.cfg.SimulateSwaps {
			if err := u.simulateUR(ctx, read, w.Address, payload); err != nil {
				lastErr = err
				log.Printf("buy simulate %dbps failed: %v", slippageBps, err)
				if isSlippageBuyErr(err) && i+1 < len(tiers) {
					sleepBuyRetry(ctx)
					continue
				}
				log.Printf("SIMULATE [%s] %s payload ok (gas estimate: %v)", u.chainID, sym, err)
			}
			log.Printf("SIMULATE [%s] %s — not broadcasting", u.chainID, sym)
			return common.Hash{}, nil
		}
		if u.execClient == nil {
			return common.Hash{}, fmt.Errorf("execution rpc not configured")
		}
		hash, err := u.sendUR(ctx, w, payload)
		if err != nil {
			lastErr = err
			log.Printf("buy %dbps failed: %v", slippageBps, err)
			if isSlippageBuyErr(err) && i+1 < len(tiers) {
				sleepBuyRetry(ctx)
				continue
			}
			return common.Hash{}, err
		}
		log.Printf("COPY TX [%s] %s hash=%s slippage=%dbps", u.chainID, sym, hash.Hex(), slippageBps)
		return hash, nil
	}
	return common.Hash{}, fmt.Errorf("buy failed after slippage retries: %w", lastErr)
}

func (u *UniversalSwap) mirrorSell(ctx context.Context, w Wallet, req Request, token, preferred, hub common.Address, read *ethclient.Client) (common.Hash, error) {
	balance, err := erc20Balance(ctx, read, token, w.Address)
	if err != nil {
		return common.Hash{}, fmt.Errorf("token balance: %w", err)
	}
	if balance.Sign() <= 0 {
		return common.Hash{}, fmt.Errorf("wallet %s: no %s balance to sell", w.Label, token.Hex())
	}
	amountIn := applySellFraction(balance, req.SellFractionBps)
	if amountIn.Sign() <= 0 {
		return common.Hash{}, fmt.Errorf("sell fraction is zero")
	}
	if !u.chainCfg.IsExecutionQuote(hub) && hub != token {
		// hub from enrich / position for 2-hop exit
	} else {
		hub = common.Address{}
	}
	plan, _, err := ur.ResolveSellRoute(ctx, read, u.chainCfg, token, preferred, hub, amountIn)
	if err != nil {
		return common.Hash{}, fmt.Errorf("quote sell: %w", err)
	}
	if err := ensureURPermit2Ready(ctx, u.execClient, read, u.chainCfg, w, token, amountIn, u.cfg.SimulateSwaps); err != nil {
		return common.Hash{}, err
	}
	var lastErr error
	for _, slippageBps := range sellSlippageTiers(u.cfg) {
		payload, err := ur.BuildSellPayload(u.chainCfg, plan, amountIn, w.Address, slippageBps)
		if err != nil {
			return common.Hash{}, err
		}
		sym := req.Token.Symbol
		if sym == "" {
			sym = token.Hex()
		}
		log.Printf("COPY [%s] sell %s via UR %s slippage=%dbps", u.chainID, sym, u.chainCfg.UniversalRouter.Hex(), slippageBps)
		if u.cfg.SimulateSwaps {
			if err := u.simulateUR(ctx, read, w.Address, payload); err != nil {
				lastErr = err
				continue
			}
			log.Printf("SIMULATE [%s] %s — not broadcasting", u.chainID, sym)
			return common.Hash{}, nil
		}
		hash, err := u.sendUR(ctx, w, payload)
		if err != nil {
			lastErr = err
			if isSlippageSellErr(err) {
				continue
			}
			return common.Hash{}, err
		}
		log.Printf("COPY TX [%s] %s hash=%s", u.chainID, sym, hash.Hex())
		return hash, nil
	}
	return common.Hash{}, fmt.Errorf("sell failed after slippage retries: %w", lastErr)
}

func (u *UniversalSwap) broadcast(ctx context.Context, w Wallet, req Request, token common.Address, side string, payload ur.ExecutePayload) (common.Hash, error) {
	read := balanceClient(u.readClient, u.execClient)
	sym := req.Token.Symbol
	if sym == "" {
		sym = token.Hex()
	}
	log.Printf("COPY [%s] %s %s via UR %s", u.chainID, side, sym, u.chainCfg.UniversalRouter.Hex())
	if u.cfg.SimulateSwaps {
		if err := u.simulateUR(ctx, read, w.Address, payload); err != nil {
			log.Printf("SIMULATE [%s] %s payload ok (gas estimate: %v)", u.chainID, sym, err)
		}
		log.Printf("SIMULATE [%s] %s — not broadcasting", u.chainID, sym)
		return common.Hash{}, nil
	}
	if u.execClient == nil {
		return common.Hash{}, fmt.Errorf("execution rpc not configured")
	}
	hash, err := u.sendUR(ctx, w, payload)
	if err != nil {
		return common.Hash{}, err
	}
	log.Printf("COPY TX [%s] %s hash=%s", u.chainID, sym, hash.Hex())
	return hash, nil
}

func (u *UniversalSwap) sendUR(ctx context.Context, w Wallet, payload ur.ExecutePayload) (common.Hash, error) {
	data, err := ur.EncodeExecute(payload.Commands, payload.Inputs, payload.Deadline)
	if err != nil {
		return common.Hash{}, err
	}
	val := payload.Value
	if val == nil {
		val = big.NewInt(0)
	}
	return signAndSend(ctx, u.execClient, w, u.chainCfg.ChainID, u.chainCfg.UniversalRouter, val, data, 0)
}

func (u *UniversalSwap) simulateUR(ctx context.Context, read *ethclient.Client, from common.Address, payload ur.ExecutePayload) error {
	data, err := ur.EncodeExecute(payload.Commands, payload.Inputs, payload.Deadline)
	if err != nil {
		return err
	}
	val := payload.Value
	if val == nil {
		val = big.NewInt(0)
	}
	msg := CallMsg(u.chainCfg.UniversalRouter, data)
	msg.From = from
	msg.Value = val
	_, err = read.EstimateGas(ctx, msg)
	return err
}

func (u *UniversalSwap) nativeUSD(ctx context.Context) (float64, error) {
	return nativeUSD(ctx, u.nativeOracle, u.cfg.NativeUsdPrice)
}
