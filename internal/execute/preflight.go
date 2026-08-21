package execute

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/execute/ur"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ProbeSellQuote checks that a Universal Router sell route exists for one token unit.
func ProbeSellQuote(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, router, token, preferred common.Address, decimals int) error {
	if client == nil || token == (common.Address{}) {
		return nil
	}
	if chainCfg.UniversalRouter == (common.Address{}) {
		return probeSellQuoteV2(ctx, client, chainCfg, router, token, preferred, decimals)
	}
	if decimals <= 0 {
		decimals = 18
	}
	amt := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	hub := common.Address{}
	_, _, err := ur.ResolveSellRoute(ctx, client, chainCfg, token, preferred, hub, amt)
	if err != nil {
		return err
	}
	return nil
}

// ProbeSellQuoteWithHub is like ProbeSellQuote but allows a hub token for 2-hop probes.
func ProbeSellQuoteWithHub(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, router, token, preferred, hub common.Address, decimals int) error {
	if client == nil || token == (common.Address{}) {
		return nil
	}
	if chainCfg.UniversalRouter == (common.Address{}) {
		return probeSellQuoteV2(ctx, client, chainCfg, router, token, preferred, decimals)
	}
	if decimals <= 0 {
		decimals = 18
	}
	amt := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	_, _, err := ur.ResolveSellRoute(ctx, client, chainCfg, token, preferred, hub, amt)
	return err
}

// ProbeSellGas simulates a full-balance sell when the wallet already holds the token.
func ProbeSellGas(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, execCfg config.ExecutionConfig, wallet, token, preferred common.Address, decimals int) error {
	if client == nil || wallet == (common.Address{}) || token == (common.Address{}) {
		return nil
	}
	balance, err := erc20Balance(ctx, client, token, wallet)
	if err != nil || balance.Sign() <= 0 {
		return nil
	}
	if chainCfg.UniversalRouter != (common.Address{}) {
		hub := common.Address{}
		plan, _, err := ur.ResolveSellRoute(ctx, client, chainCfg, token, preferred, hub, balance)
		if err != nil {
			return fmt.Errorf("sell quote: %w", err)
		}
		payload, err := ur.BuildSellPayload(chainCfg, plan, balance, wallet, execCfg.SlippageBps)
		if err != nil {
			return err
		}
		data, err := ur.EncodeExecute(payload.Commands, payload.Inputs, payload.Deadline)
		if err != nil {
			return err
		}
		val := payload.Value
		if val == nil {
			val = big.NewInt(0)
		}
		msg := CallMsg(chainCfg.UniversalRouter, data)
		msg.From = wallet
		msg.Value = val
		_, err = client.EstimateGas(ctx, msg)
		if isTransferFromFailed(err) {
			return fmt.Errorf("sell blocked: %w", err)
		}
		return nil
	}
	return probeSellGasV2(ctx, client, chainCfg, execCfg, wallet, token, preferred, decimals)
}

func probeSellQuoteV2(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, router, token, preferred common.Address, decimals int) error {
	if router == (common.Address{}) {
		router = chainCfg.V2Router
	}
	if decimals <= 0 {
		decimals = 18
	}
	amt := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	_, _, err := ResolveSellRoute(ctx, client, chainCfg, router, token, preferred, amt)
	return err
}

func probeSellGasV2(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, execCfg config.ExecutionConfig, wallet, token, preferred common.Address, decimals int) error {
	balance, err := erc20Balance(ctx, client, token, wallet)
	if err != nil || balance.Sign() <= 0 {
		return nil
	}
	router := execCfg.V2Router
	if router == (common.Address{}) {
		router = chainCfg.V2Router
	}
	route, quoted, err := ResolveSellRoute(ctx, client, chainCfg, router, token, preferred, balance)
	if err != nil {
		return fmt.Errorf("sell quote: %w", err)
	}
	plan, err := buildV2SellPlan(chainCfg, router, wallet, token, route, balance, execCfg.SlippageBps, quoted)
	if err != nil {
		return err
	}
	_, err = estimateSwapGas(ctx, client, wallet, plan)
	if isTransferFromFailed(err) {
		return fmt.Errorf("sell blocked: %w", err)
	}
	return nil
}

func isTransferFromFailed(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "TRANSFER_FROM_FAILED") || strings.Contains(msg, "TRANSFERHELPER")
}

// ProbeBuyRoute resolves a UR buy route for preflight (optional helper).
func ProbeBuyRoute(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, token, preferred common.Address, tradeUsd, nativePrice float64, info enrich.TokenInfo) error {
	_, _, err := ur.ResolveBuyRoute(ctx, client, chainCfg, token, preferred, tradeUsd, nativePrice, info)
	return err
}
