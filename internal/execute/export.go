package execute

import (
	"context"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Erc20Allowance reads ERC20 allowance.
func Erc20Allowance(ctx context.Context, client *ethclient.Client, token, owner, spender common.Address) (*big.Int, error) {
	return erc20Allowance(ctx, client, token, owner, spender)
}

// EncodeApprove builds ERC20 approve calldata.
func EncodeApprove(spender common.Address, amount *big.Int) []byte {
	return encodeApprove(spender, amount)
}

// MaxUint256 is the max ERC20 approval amount.
func MaxUint256() *big.Int {
	return maxUint256
}

// SignAndSend signs and broadcasts a transaction.
func SignAndSend(ctx context.Context, client *ethclient.Client, w Wallet, chainID int64, to common.Address, value *big.Int, data []byte, gasLimit uint64) (common.Hash, error) {
	return signAndSend(ctx, client, w, chainID, to, value, data, gasLimit)
}

// WaitReceipt polls for a transaction receipt.
func WaitReceipt(ctx context.Context, client *ethclient.Client, hash common.Hash, timeout time.Duration) (*types.Receipt, error) {
	return waitReceipt(ctx, client, hash, timeout)
}

// TokenBalance reads an ERC20 balance for owner.
func TokenBalance(ctx context.Context, client *ethclient.Client, token, owner common.Address) (*big.Int, error) {
	return erc20Balance(ctx, client, token, owner)
}

// IsNoTokenBalanceErr is true when the exec wallet holds none of the token to sell.
func IsNoTokenBalanceErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "balance to sell")
}

// EstimateSwapGas estimates gas for a V2 swap plan.
func EstimateSwapGas(ctx context.Context, client *ethclient.Client, from common.Address, plan v2SwapPlan) (uint64, error) {
	return estimateSwapGas(ctx, client, from, plan)
}
