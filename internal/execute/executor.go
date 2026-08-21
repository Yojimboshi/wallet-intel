package execute

import (
	"context"
	"fmt"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/parse"
	"github.com/Yojimboshi/wallet-intel/internal/pool"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Request struct {
	SourceWallet      common.Address
	SourceLabel       string
	Trade             parse.Trade
	Token             enrich.TokenInfo
	Chain             string
	SizeUsd           float64
	QuoteToken        common.Address // entry/exit spend/receive token (native/USDC/USDT)
	HubToken          common.Address // optional 2-hop hub (e.g. NVDAB)
	ExecWallet        Wallet
	TokenRecipient    common.Address // buy: swap output wallet (default exec wallet)
	TransferRecipient common.Address // transfer: ERC20 recipient (default collector)
	SellFractionBps   int            // sell: 0/10000=full, 5000=half of balance
}

type Executor interface {
	Mirror(ctx context.Context, req Request) (common.Hash, error)
}

func New(execClient, readClient *ethclient.Client, cfg config.ExecutionConfig, chainID string, chainCfg chain.Config, nativeOracle *pool.NativeOracle) (Executor, error) {
	var inner Executor
	switch cfg.Provider {
	case "", "direct", "ur", "universal":
		inner = &UniversalSwap{execClient: execClient, readClient: readClient, cfg: cfg, chainCfg: chainCfg, chainID: chainID, nativeOracle: nativeOracle}
	case "v2direct":
		inner = &DirectSwap{execClient: execClient, readClient: readClient, cfg: cfg, chainCfg: chainCfg, chainID: chainID, nativeOracle: nativeOracle}
	default:
		return nil, fmt.Errorf("unknown execution provider %q (use direct, ur, or v2direct)", cfg.Provider)
	}
	if cfg.RotateWallets && len(cfg.Wallets) > 0 {
		return NewPool(execClient, readClient, cfg, chainCfg, inner, nativeOracle), nil
	}
	return inner, nil
}

func balanceClient(read, exec *ethclient.Client) *ethclient.Client {
	if read != nil {
		return read
	}
	return exec
}
