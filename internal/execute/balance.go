package execute

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Wallet is a funded hot wallet used for copy execution.
type Wallet struct {
	Label      string
	Address    common.Address
	PrivateKey string
}

// HasGasForTrade ensures the wallet keeps at least gasReserveUsd worth of native
// (ETH/BNB) after the trade. Buys spend native; sells only need gas headroom.
func HasGasForTrade(
	ctx context.Context,
	client *ethclient.Client,
	addr common.Address,
	side string,
	tradeUsd float64,
	gasReserveUsd float64,
	nativeUsdPrice float64,
) (bool, error) {
	if client == nil {
		return false, fmt.Errorf("rpc client is nil")
	}
	if nativeUsdPrice <= 0 {
		return false, fmt.Errorf("nativeUsdPrice not configured")
	}
	bal, err := client.BalanceAt(ctx, addr, nil)
	if err != nil {
		return false, err
	}

	spendUsd := gasReserveUsd
	if strings.EqualFold(side, "buy") {
		spendUsd += tradeUsd
	}
	need := usdToWei(spendUsd, nativeUsdPrice)
	return bal.Cmp(need) >= 0, nil
}

// HasSufficientNative is an alias for buy-side checks.
func HasSufficientNative(
	ctx context.Context,
	client *ethclient.Client,
	addr common.Address,
	tradeUsd float64,
	gasReserveUsd float64,
	nativeUsdPrice float64,
) (bool, error) {
	return HasGasForTrade(ctx, client, addr, "buy", tradeUsd, gasReserveUsd, nativeUsdPrice)
}

func usdToWei(usd, nativeUsdPrice float64) *big.Int {
	if usd <= 0 {
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

// HasFundsForTrade checks native gas reserve and buy-side spend (native or stable quote).
func HasFundsForTrade(
	ctx context.Context,
	client *ethclient.Client,
	chainCfg chain.Config,
	addr common.Address,
	side string,
	tradeUsd float64,
	gasReserveUsd float64,
	nativeUsdPrice float64,
	quoteToken common.Address,
) (bool, error) {
	gasOK, err := HasGasForTrade(ctx, client, addr, "sell", 0, gasReserveUsd, nativeUsdPrice)
	if err != nil {
		return false, err
	}
	if !gasOK {
		return false, nil
	}
	if !strings.EqualFold(side, "buy") {
		return true, nil
	}
	wrapped, ok := chainCfg.WrappedNative()
	if !ok {
		return false, fmt.Errorf("wrapped native not configured")
	}
	if quoteToken == (common.Address{}) || quoteToken == wrapped {
		return HasGasForTrade(ctx, client, addr, "buy", tradeUsd, gasReserveUsd, nativeUsdPrice)
	}
	dec, ok := chainCfg.QuoteTokenDecimals(quoteToken)
	if !ok {
		return false, fmt.Errorf("unknown quote token %s", quoteToken.Hex())
	}
	need := usdToTokenUnits(tradeUsd, dec)
	if need.Sign() <= 0 {
		return false, nil
	}
	bal, err := erc20Balance(ctx, client, quoteToken, addr)
	if err != nil {
		return false, err
	}
	return bal.Cmp(need) >= 0, nil
}

func NativeBalanceUSD(ctx context.Context, client *ethclient.Client, addr common.Address, nativeUsdPrice float64) (float64, error) {
	bal, err := client.BalanceAt(ctx, addr, nil)
	if err != nil {
		return 0, err
	}
	f := new(big.Float).SetInt(bal)
	eth, _ := new(big.Float).Quo(f, new(big.Float).SetInt64(1e18)).Float64()
	return eth * nativeUsdPrice, nil
}

func NativeSymbol(chainCfg chain.Config) string {
	return chainCfg.NativeSymbol
}

// GasReserveNative returns human-readable min native to keep (e.g. 0.0025 ETH).
func GasReserveNative(gasReserveUsd, nativeUsdPrice float64) float64 {
	if gasReserveUsd <= 0 || nativeUsdPrice <= 0 {
		return 0
	}
	return gasReserveUsd / nativeUsdPrice
}
