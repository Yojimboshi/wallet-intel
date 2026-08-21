package execute

import (
	"context"
	"fmt"
	"log"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func (d *DirectSwap) executeTransfer(
	ctx context.Context,
	w Wallet,
	token common.Address,
	sym string,
	recipient common.Address,
	read *ethclient.Client,
) (common.Hash, error) {
	if recipient == (common.Address{}) {
		return common.Hash{}, fmt.Errorf("transfer recipient is empty")
	}
	balance, err := erc20Balance(ctx, read, token, w.Address)
	if err != nil {
		return common.Hash{}, fmt.Errorf("token balance: %w", err)
	}
	if balance.Sign() <= 0 {
		return common.Hash{}, fmt.Errorf("wallet %s: no %s balance to transfer", w.Label, token.Hex())
	}

	log.Printf(
		"COPY [%s] transfer %s → collector %s | amount=%s | exec %s",
		d.chainID, sym, recipient.Hex(), balance.String(), w.Label,
	)

	if d.cfg.SimulateSwaps {
		msg := callMsg(token, encodeTransfer(recipient, balance))
		msg.From = w.Address
		gas, err := read.EstimateGas(ctx, msg)
		if err != nil {
			return common.Hash{}, fmt.Errorf("simulate transfer: %w", err)
		}
		log.Printf("SIMULATE [%s] transfer %s gas≈%d — not broadcasting", d.chainID, sym, gas)
		return common.Hash{}, nil
	}
	if d.execClient == nil {
		return common.Hash{}, fmt.Errorf("execution rpc not configured")
	}

	hash, err := signAndSend(ctx, d.execClient, w, d.chainCfg.ChainID, token, big.NewInt(0), encodeTransfer(recipient, balance), 0)
	if err != nil {
		return common.Hash{}, err
	}
	log.Printf("COPY TX [%s] transfer %s hash=%s → %s", d.chainID, sym, hash.Hex(), recipient.Hex())
	return hash, nil
}
