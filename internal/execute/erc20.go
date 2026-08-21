package execute

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func erc20Balance(ctx context.Context, client *ethclient.Client, token, owner common.Address) (*big.Int, error) {
	if client == nil {
		return nil, fmt.Errorf("rpc client is nil")
	}
	out, err := client.CallContract(ctx, callMsg(token, encodeBalanceOf(owner)), nil)
	if err != nil {
		return nil, err
	}
	return decodeUint256(out), nil
}

func erc20Allowance(ctx context.Context, client *ethclient.Client, token, owner, spender common.Address) (*big.Int, error) {
	if client == nil {
		return nil, fmt.Errorf("rpc client is nil")
	}
	out, err := client.CallContract(ctx, callMsg(token, encodeAllowance(owner, spender)), nil)
	if err != nil {
		return nil, err
	}
	return decodeUint256(out), nil
}
