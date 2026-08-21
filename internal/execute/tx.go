package execute

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func signAndSend(
	ctx context.Context,
	client *ethclient.Client,
	w Wallet,
	chainID int64,
	to common.Address,
	value *big.Int,
	data []byte,
	gasLimit uint64,
) (common.Hash, error) {
	if client == nil {
		return common.Hash{}, fmt.Errorf("rpc client is nil")
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(w.PrivateKey, "0x"))
	if err != nil {
		return common.Hash{}, fmt.Errorf("private key: %w", err)
	}
	if crypto.PubkeyToAddress(key.PublicKey) != w.Address {
		return common.Hash{}, fmt.Errorf("private key does not match wallet %s", w.Label)
	}

	nonce, err := client.PendingNonceAt(ctx, w.Address)
	if err != nil {
		return common.Hash{}, fmt.Errorf("nonce: %w", err)
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("gas price: %w", err)
	}
	if gasLimit == 0 {
		msg := callMsg(to, data)
		msg.From = w.Address
		msg.Value = value
		gasLimit, err = client.EstimateGas(ctx, msg)
		if err != nil {
			return common.Hash{}, fmt.Errorf("estimate gas: %w", err)
		}
		gasLimit = gasLimit + gasLimit/5 // +20% buffer
	}

	tx := types.NewTransaction(nonce, to, value, gasLimit, gasPrice, data)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), key)
	if err != nil {
		return common.Hash{}, fmt.Errorf("sign: %w", err)
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		return common.Hash{}, fmt.Errorf("send: %w", err)
	}
	return signed.Hash(), nil
}

func estimateSwapGas(ctx context.Context, client *ethclient.Client, from common.Address, plan v2SwapPlan) (uint64, error) {
	msg := callMsg(plan.To, plan.Calldata)
	msg.From = from
	msg.Value = plan.NativeValue
	return client.EstimateGas(ctx, msg)
}

func waitReceipt(ctx context.Context, client *ethclient.Client, hash common.Hash, timeout time.Duration) (*types.Receipt, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		receipt, err := client.TransactionReceipt(ctx, hash)
		if err == nil {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("timeout waiting for receipt %s", hash.Hex())
}
