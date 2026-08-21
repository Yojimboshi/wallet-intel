package execute

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/execute/ur"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	permit2ApprovalSec = 86400 * 365
	permit2LeewaySec   = 300
)

var maxUint160 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 160), big.NewInt(1))

func ensureURPermit2Ready(
	ctx context.Context,
	execClient, readClient *ethclient.Client,
	chainCfg chain.Config,
	w Wallet,
	token common.Address,
	amountIn *big.Int,
	simulate bool,
) error {
	if chainCfg.Permit2 == (common.Address{}) || chainCfg.UniversalRouter == (common.Address{}) {
		return fmt.Errorf("permit2 or universal router not configured")
	}
	read := readClient
	if read == nil {
		read = execClient
	}
	if err := ensureErc20ApprovedForPermit2(ctx, execClient, read, chainCfg, w, token, amountIn, simulate); err != nil {
		return err
	}
	return ensurePermit2RouterAllowance(ctx, execClient, read, chainCfg, w, token, amountIn, simulate)
}

func ensureErc20ApprovedForPermit2(ctx context.Context, execClient, read *ethclient.Client, chainCfg chain.Config, w Wallet, token common.Address, need *big.Int, simulate bool) error {
	allow, err := erc20Allowance(ctx, read, token, w.Address, chainCfg.Permit2)
	if err != nil {
		return fmt.Errorf("erc20 allowance: %w", err)
	}
	if allow.Cmp(need) >= 0 {
		return nil
	}
	if simulate {
		log.Printf("SIMULATE approve %s for Permit2", token.Hex())
		return nil
	}
	data := encodeApprove(chainCfg.Permit2, maxUint256)
	hash, err := signAndSend(ctx, execClient, w, chainCfg.ChainID, token, big.NewInt(0), data, 80_000)
	if err != nil {
		return fmt.Errorf("approve permit2: %w", err)
	}
	log.Printf("APPROVE TX hash=%s", hash.Hex())
	receipt, err := waitReceipt(ctx, execClient, hash, 90*time.Second)
	if err != nil {
		return err
	}
	if receipt.Status == 0 {
		return fmt.Errorf("approve tx reverted")
	}
	return nil
}

func ensurePermit2RouterAllowance(ctx context.Context, execClient, read *ethclient.Client, chainCfg chain.Config, w Wallet, token common.Address, need *big.Int, simulate bool) error {
	data, err := ur.EncodePermit2Allowance(w.Address, token, chainCfg.UniversalRouter)
	if err != nil {
		return err
	}
	out, err := read.CallContract(ctx, CallMsg(chainCfg.Permit2, data), nil)
	if err != nil {
		return fmt.Errorf("permit2 allowance read: %w", err)
	}
	amount, expiration, err := ur.DecodePermit2Allowance(out)
	if err != nil {
		return err
	}
	now := uint64(time.Now().Unix())
	if amount.Cmp(need) >= 0 && expiration > now+permit2LeewaySec {
		return nil
	}
	if simulate {
		log.Printf("SIMULATE Permit2 approve for UR token=%s", token.Hex())
		return nil
	}
	exp := now + permit2ApprovalSec
	approveData, err := ur.EncodePermit2Approve(token, chainCfg.UniversalRouter, maxUint160, exp)
	if err != nil {
		return err
	}
	hash, err := signAndSend(ctx, execClient, w, chainCfg.ChainID, chainCfg.Permit2, big.NewInt(0), approveData, 100_000)
	if err != nil {
		return fmt.Errorf("permit2 approve: %w", err)
	}
	log.Printf("PERMIT2 TX hash=%s", hash.Hex())
	receipt, err := waitReceipt(ctx, execClient, hash, 90*time.Second)
	if err != nil {
		return err
	}
	if receipt.Status == 0 {
		return fmt.Errorf("permit2 approve reverted")
	}
	return nil
}
